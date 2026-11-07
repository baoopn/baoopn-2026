package contact

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jhillyerd/enmime/v2"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// smtpSink is a local SMTP catcher on 127.0.0.1: it keeps every DATA body.
type smtpSink struct {
	ln   net.Listener
	mu   sync.Mutex
	msgs []string
}

func newSink(t *testing.T) *smtpSink {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &smtpSink{ln: ln}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

func (s *smtpSink) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *smtpSink) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	say := func(l string) { _, _ = fmt.Fprint(c, l+"\r\n") }
	say("220 sink ESMTP")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(cmd, "EHLO"):
			say("250-sink")
			say("250 8BITMIME") // no STARTTLS: allowed only for a local host
		case cmd == "DATA":
			say("354 go")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			s.mu.Lock()
			s.msgs = append(s.msgs, b.String())
			s.mu.Unlock()
			say("250 queued")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("250 ok")
		}
	}
}

func (s *smtpSink) raw() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.msgs...)
}

// newSMTPEnv is newEnv with the real SMTP mailer pointed at sink.
func newSMTPEnv(t *testing.T, sink *smtpSink) *env {
	t.Helper()
	db := testdb.New(t)
	mailer := bmail.NewSMTP(bmail.Config{Enabled: true, Host: "127.0.0.1", Port: sink.port(), From: "Bao Nguyen <bao@mail.baoopn.com>"})
	site := sitesettings.New(sitesettings.Deps{DB: db, DefaultNotify: ownerAddr, Mailer: mailer})
	m := New(Deps{DB: db, Mailer: mailer, Settings: site, PublicURL: "https://baoopn.com",
		InboundDomain: testDomain, InboundSecret: testSecret})
	r := chi.NewRouter()
	m.Public(r)
	m.Admin(r)
	m.Hooks(r)
	site.Admin(r)
	return &env{t: t, db: db, site: site, m: m, h: r}
}

const (
	hostileName    = "Eve\r\nBcc: bcc@evil.test <b>Bold</b>"
	hostileSubject = "Hi\r\nBcc: bcc@evil.test ‮gnp.exe"
	marker         = "QZXMARKERQZX"
)

func hostileMessage() string {
	return marker + " <script>alert(1)</script></td></table> visit https://evil.test/login " +
		"zero​width ‮RTL‬ שלום עולם مرحبا\n" + strings.Repeat("x", 3000) + "\n" + strings.Repeat("word ", 300)
}

// TestFourEmailsAsMIME sends the four emails through the real SMTP client to
// a local catcher and checks the raw messages.
func TestFourEmailsAsMIME(t *testing.T) {
	sink := newSink(t)
	e := newSMTPEnv(t, sink)
	e.withTurnstile()
	s := sitesettings.Defaults()
	s.AutoReplyEnabled = true
	s.Signature = "Bao Nguyen\nhttps://baoopn.com"
	putSettings(t, e, s)

	if code, raw := e.form(hostileName, "visitor@example.test", hostileSubject, hostileMessage()); code != 201 {
		t.Fatalf("form: %d %s", code, raw)
	}
	e.wait()
	var id int64
	var tok string
	if err := e.db.QueryRow(t.Context(), `SELECT id, reply_token FROM threads`).Scan(&id, &tok); err != nil {
		t.Fatal(err)
	}
	inID := "<in-1@mail.example>"
	if code := e.hook(email("Visitor <visitor@example.test>", replyAddr(tok), inID,
		"Thanks <b>Bao</b>.\n\nSee https://evil.test/x"), replyAddr(tok)); code != 204 {
		t.Fatalf("hook: %d", code)
	}
	e.wait()
	body := `{"body":"Hi,\n\n> Thanks Bao.\n\nSure.","request_id":"req-mime-1"}`
	if code, raw := e.do("POST", "/threads/"+strconv.FormatInt(id, 10)+"/messages", body, nil); code != 201 {
		t.Fatalf("reply: %d %s", code, raw)
	}
	e.wait()

	raws := sink.raw()
	if len(raws) != 4 {
		t.Fatalf("want 4 messages (alert, ack, reply alert, reply), got %d", len(raws))
	}
	byKind := map[string]*enmime.Envelope{}
	for i, raw := range raws {
		if dir := os.Getenv("EMAIL_EML_DIR"); dir != "" {
			_ = os.WriteFile(filepath.Join(dir, "msg-"+strconv.Itoa(i+1)+".eml"), []byte(raw), 0o644)
		}
		checkMIME(t, raw)
		env, err := enmime.ReadEnvelope(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		kind := ""
		switch subj := env.GetHeader("Subject"); {
		case strings.HasPrefix(subj, "New message:"):
			kind = "alert"
		case strings.HasPrefix(subj, "New reply:"):
			kind = "reply-alert"
		case env.GetHeader("Auto-Submitted") == "auto-replied":
			kind = "ack"
		default:
			kind = "reply"
		}
		byKind[kind] = env
	}
	for _, k := range []string{"alert", "ack", "reply-alert", "reply"} {
		if byKind[k] == nil {
			t.Fatalf("no %s message", k)
		}
	}

	ack := byKind["ack"]
	if ack.GetHeader("To") != "<visitor@example.test>" || ack.GetHeader("Subject") != "Re: your message" ||
		ack.GetHeader("Reply-To") != "<"+replyAddr(tok)+">" {
		t.Fatalf("ack headers: to %q subject %q reply-to %q", ack.GetHeader("To"), ack.GetHeader("Subject"), ack.GetHeader("Reply-To"))
	}
	for _, part := range []string{ack.Text, ack.HTML} {
		if strings.Contains(part, marker) || strings.Contains(part, "evil.test") || strings.Contains(part, "Eve") {
			t.Fatal("the ack shows visitor content")
		}
	}
	if !strings.Contains(ack.Text, "Hi there,") {
		t.Fatalf("ack greeting: %q", ack.Text)
	}

	alert := byKind["alert"]
	if alert.GetHeader("To") != "<"+ownerAddr+">" || !strings.Contains(alert.Text, marker) ||
		!strings.Contains(alert.HTML, "&lt;script&gt;alert(1)&lt;/script&gt;") || !strings.Contains(alert.Text, "<script>alert(1)</script>") {
		t.Fatal("the owner alert does not show the message, escaped in HTML and raw in text")
	}
	if !strings.Contains(alert.Text, strings.Repeat("x", 3000)) || !strings.Contains(alert.HTML, strings.Repeat("x", 3000)) {
		t.Fatal("a 3000-character word was cut")
	}

	ra := byKind["reply-alert"]
	if !strings.Contains(ra.HTML, "Thanks &lt;b&gt;Bao&lt;/b&gt;.") || strings.Contains(ra.HTML, "<b>Bao") {
		t.Fatal("the reply alert does not escape the visitor's reply")
	}

	reply := byKind["reply"]
	if reply.GetHeader("In-Reply-To") != inID || strings.TrimSpace(reply.GetHeader("References")) != inID ||
		reply.GetHeader("Auto-Submitted") != "" || reply.GetHeader("Reply-To") != "<"+replyAddr(tok)+">" ||
		!strings.HasPrefix(reply.GetHeader("Subject"), "Re: ") {
		t.Fatalf("reply threading: in-reply-to %q refs %q subject %q", reply.GetHeader("In-Reply-To"),
			reply.GetHeader("References"), reply.GetHeader("Subject"))
	}
	if got := strings.ReplaceAll(reply.Text, "\r\n", "\n"); got != "Hi,\n\n> Thanks Bao.\n\nSure.\n\n-- \nBao Nguyen\nhttps://baoopn.com" {
		t.Fatalf("reply text changed: %q", got)
	}
}

// checkMIME checks one raw message: no injected header, multipart/alternative
// with a utf-8 text and HTML part, and no line over the SMTP limit.
func checkMIME(t *testing.T, raw string) {
	t.Helper()
	head, _, _ := strings.Cut(raw, "\r\n\r\n")
	for _, l := range strings.Split(head, "\r\n") {
		if k, _, _ := strings.Cut(l, ":"); strings.EqualFold(strings.TrimSpace(k), "bcc") {
			t.Fatalf("injected header %q", l)
		}
	}
	for _, l := range strings.Split(raw, "\r\n") {
		if len(l) > 998 {
			t.Fatalf("line of %d octets", len(l))
		}
	}
	env, err := enmime.ReadEnvelope(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ct := env.Root.ContentType; ct != "multipart/alternative" {
		t.Fatalf("content type %q", ct)
	}
	var kinds []string
	for p := env.Root.FirstChild; p != nil; p = p.NextSibling {
		kinds = append(kinds, p.ContentType+";"+strings.ToLower(p.Charset))
	}
	if strings.Join(kinds, ",") != "text/plain;utf-8,text/html;utf-8" {
		t.Fatalf("parts %v", kinds)
	}
	if env.Text == "" || env.HTML == "" || strings.Contains(strings.ToLower(env.HTML), "<script") {
		t.Fatal("a part is empty or the HTML carries a script")
	}
}

// TestAckPerAddressCapConcurrent sends many forms from one address at once:
// no more than ackPerAddress acknowledgements go out.
func TestAckPerAddressCapConcurrent(t *testing.T) {
	e := newEnv(t, true, testDomain)
	e.withTurnstile()
	s := sitesettings.Defaults()
	s.AutoReplyEnabled = true
	putSettings(t, e, s)
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			addr := "same@x.com"
			if i%2 == 1 {
				addr = "Same+t" + strconv.Itoa(i) + "@X.com"
			}
			e.form("Sam", addr, "Hi", "a long enough message")
		})
	}
	wg.Wait()
	e.wait()
	n := 0
	for _, m := range e.mailer.messages() {
		if m.AutoSubmitted == "auto-replied" {
			n++
		}
	}
	if n > ackPerAddress {
		t.Fatalf("%d acks to one address, cap %d", n, ackPerAddress)
	}
	if got := e.count(`SELECT count(*) FROM threads`); got != 10 {
		t.Fatalf("threads stored: %d", got)
	}
}

// TestHostileDocsParityAndText checks hostile, long and right-to-left input:
// every value in the HTML is in the text part, and the text part is never
// HTML-escaped.
func TestHostileDocsParityAndText(t *testing.T) {
	sub := submission{name: "<b>אבי</b> ‮", email: "v@example.test", subject: "שלום ​ <i>x</i>",
		message: hostileMessage(), at: goldenAt}
	th := thread{ThreadSummary: ThreadSummary{ID: 9, Name: sub.name, Email: sub.email, Subject: sub.subject}}
	s := goldenSettings()
	ack, _ := ackDoc(goldenBrand, 9, sub, s.AutoReplyText, s.Signature)
	docs := map[string]bmail.Doc{
		"new-thread": newThreadDoc(goldenBrand, goldenLink, 9, sub),
		"inbound":    inboundDoc(goldenBrand, goldenLink, th, sub.message),
		"ack":        ack,
		"reply":      replyDoc(goldenBrand, "Re: x", sub.message+"\n> <q>quoted</q>", s.Signature),
	}
	for name, d := range docs {
		text, html := renderDoc(t, d)
		for _, b := range d.Blocks {
			for _, f := range b.Fields {
				if !strings.Contains(text, f.Label+": "+f.Value) {
					t.Errorf("%s: field %q missing from text", name, f.Label)
				}
			}
			for _, l := range strings.Split(b.Text, "\n") {
				if !strings.Contains(text, l) {
					t.Errorf("%s: line %q missing from text", name, clipRunes(l, 40))
				}
			}
		}
		if strings.Contains(text, "&lt;") || strings.Contains(text, "&amp;") || strings.Contains(text, "&#") {
			t.Errorf("%s: the text part is HTML-escaped", name)
		}
		if strings.Contains(html, "<b>") || strings.Contains(html, "<i>") || strings.Contains(html, "<q>") || strings.Contains(html, "<script") {
			t.Errorf("%s: raw markup in HTML", name)
		}
	}
	if !strings.HasPrefix(mustText(t, ack), "Hi there,") {
		t.Error("an RTL-override name reached the ack")
	}
}
