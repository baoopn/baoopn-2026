package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jhillyerd/enmime/v2"
)

// catcher is an in-process SMTP server that takes one message per
// connection.
type catcher struct {
	addr string
	// onData answers the end of DATA ("250 OK" when empty); onQuit answers QUIT
	// ("" hangs up without a word).
	onData, onQuit string
	conns          atomic.Int32
	mu             sync.Mutex
	messages       []string
	rcpts          []string
}

func newCatcher(t *testing.T, onData, onQuit string) *catcher {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	c := &catcher{addr: ln.Addr().String(), onData: onData, onQuit: onQuit}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			c.conns.Add(1)
			go c.serve(conn)
		}
	}()
	return c
}

func (c *catcher) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	r := bufio.NewReader(conn)
	say := func(line string) { _, _ = conn.Write([]byte(line + "\r\n")) }
	say("220 catcher ready")
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch verb := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(verb, "EHLO"), strings.HasPrefix(verb, "HELO"):
			say("250-catcher")
			say("250 AUTH PLAIN")
		case strings.HasPrefix(verb, "AUTH"):
			say("235 ok")
		case strings.HasPrefix(verb, "RCPT"):
			c.mu.Lock()
			c.rcpts = append(c.rcpts, strings.TrimSpace(line))
			c.mu.Unlock()
			say("250 OK")
		case strings.HasPrefix(verb, "DATA"):
			say("354 go on")
			var body strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				body.WriteString(l)
			}
			answer := c.onData
			if answer == "" {
				answer = "250 OK: queued"
				c.mu.Lock()
				c.messages = append(c.messages, body.String())
				c.mu.Unlock()
			}
			say(answer)
		case strings.HasPrefix(verb, "QUIT"):
			if c.onQuit != "" {
				say(c.onQuit)
			}
			return
		default:
			say("250 OK")
		}
	}
}

func (c *catcher) taken() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.messages...)
}

func (c *catcher) smtp(t *testing.T) *SMTP {
	t.Helper()
	host, port, err := net.SplitHostPort(c.addr)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := strconv.Atoi(port)
	return NewSMTP(Config{Enabled: true, Host: host, Port: p, User: "baoopn", Password: "key", From: "Bao Nguyen <bao@mail.baoopn.com>"})
}

var reply = Message{
	To: "priya@example.com", ReplyTo: "reply+0123456789abcdef0123456789abcdef@reply.baoopn.com",
	Subject: "Re: Hello", Text: "We have August 14 open.",
	InReplyTo: "<in-2@mail.example>", References: []string{"<in-1@mail.example>", "bad id", "<in-2@mail.example>"},
}

func headersOf(t *testing.T, raw string) mail.Header {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("not a valid message: %v", err)
	}
	return msg.Header
}

func TestSendDeliversAndReturnsTheMessageID(t *testing.T) {
	for name, onQuit := range map[string]string{
		"an ordinary goodbye":         "221 bye",
		"quit answered with an error": "421 4.3.0 shutting down",
		"the server hangs up first":   "",
	} {
		t.Run(name, func(t *testing.T) {
			c := newCatcher(t, "", onQuit)
			id, err := c.smtp(t).Send(context.Background(), reply)
			if err != nil || !strings.HasSuffix(id, "@mail.baoopn.com>") || len(id) != len("<@mail.baoopn.com>")+24 {
				t.Fatalf("id %q, err %v", id, err)
			}
			got := c.taken()
			if len(got) != 1 {
				t.Fatalf("the server took %d messages", len(got))
			}
			h := headersOf(t, got[0])
			if h.Get("Message-ID") != id || h.Get("Reply-To") != "<"+reply.ReplyTo+">" ||
				h.Get("In-Reply-To") != "<in-2@mail.example>" || h.Get("From") != `"Bao Nguyen" <bao@mail.baoopn.com>` {
				t.Fatalf("headers: %v", h)
			}
			if refs := strings.Fields(h.Get("References")); len(refs) != 2 || refs[0] != "<in-1@mail.example>" {
				t.Fatalf("references: %q", h.Get("References"))
			}
			if h.Get("Auto-Submitted") != "" || h.Get("X-Auto-Response-Suppress") != "" {
				t.Fatalf("a person's mail is not marked automatic: %v", h)
			}
		})
	}
}

func TestSendRefusalCarriesOnlyTheCode(t *testing.T) {
	c := newCatcher(t, "550 5.7.1 priya@example.com: rejected", "221 bye")
	_, err := c.smtp(t).Send(context.Background(), reply)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "550") || strings.Contains(err.Error(), "priya") {
		t.Fatalf("err = %v", err)
	}
	c = newCatcher(t, "451 4.3.0 try later priya@example.com", "221 bye")
	_, err = c.smtp(t).Send(context.Background(), reply)
	if err == nil || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "451") || strings.Contains(err.Error(), "priya") {
		t.Fatalf("temporary failure: %v", err)
	}
}

func TestSendRejectsBadAddresses(t *testing.T) {
	c := newCatcher(t, "", "221 bye")
	for _, m := range []Message{
		{To: "not an address", Text: "x"},
		{To: "a@b.example\r\nBcc: evil@x.example", Text: "x"},
		{To: "a@b.example", ReplyTo: "nope", Text: "x"},
	} {
		if _, err := c.smtp(t).Send(context.Background(), m); !errors.Is(err, ErrRefused) {
			t.Errorf("%q: err = %v", m.To, err)
		}
	}
	if c.conns.Load() != 0 {
		t.Fatal("a bad address must not reach the server")
	}
}

func TestDisabledSendMakesNoConnection(t *testing.T) {
	c := newCatcher(t, "", "221 bye")
	s := c.smtp(t)
	s.cfg.Enabled = false
	if _, err := s.Send(context.Background(), reply); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err = %v", err)
	}
	if c.conns.Load() != 0 {
		t.Fatal("a disabled mailer dialed")
	}
	if s.Enabled() || s.FromName() != "Bao Nguyen" || s.FromAddress() != "bao@mail.baoopn.com" {
		t.Fatalf("disabled: %v %q %q", s.Enabled(), s.FromName(), s.FromAddress())
	}
}

func TestBuildHeadersAndQuotedPrintable(t *testing.T) {
	s := NewSMTP(Config{From: "Bao Nguyen <bao@mail.baoopn.com>"})
	text := "Line one with é and a very long line " + strings.Repeat("word ", 40) + "\n=== done"
	l, err := s.letterOf(Message{
		To: "Visitor <v@example.com>", Subject: "New message:\r\nBcc: evil@x.example ünïcode",
		Text: text, AutoSubmitted: "auto-generated",
		References: func() []string {
			var r []string
			for i := range 15 {
				r = append(r, "<r"+strconv.Itoa(i)+"@x>")
			}
			return r
		}(),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := build(l)
	if err != nil {
		t.Fatal(err)
	}
	h := headersOf(t, string(raw))
	if h.Get("Bcc") != "" || h.Get("Reply-To") != "" || h.Get("In-Reply-To") != "" {
		t.Fatalf("unexpected headers: %v", h)
	}
	if h.Get("Auto-Submitted") != "auto-generated" || h.Get("X-Auto-Response-Suppress") != "All" {
		t.Fatalf("automatic headers: %v", h)
	}
	if refs := strings.Fields(h.Get("References")); len(refs) != maxReferences || refs[0] != "<r5@x>" {
		t.Fatalf("references keep the last %d: %v", maxReferences, refs)
	}
	for line := range strings.SplitSeq(string(raw), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line too long: %d", len(line))
		}
	}
	env, err := enmime.ReadEnvelope(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(env.Text, "\r\n", "\n") != text || !strings.Contains(env.HTML, "=== done") || !strings.Contains(env.HTML, "<br>") {
		t.Fatalf("round trip: text %q", env.Text)
	}
	if got := env.GetHeader("Subject"); got != "New message: Bcc: evil@x.example ünïcode" {
		t.Fatalf("subject %q", got)
	}
	// A value that is not a keyword is not written.
	l, _ = s.letterOf(Message{To: "v@example.com", AutoSubmitted: "x\r\nBcc: y"})
	raw, _ = build(l)
	if h := headersOf(t, string(raw)); h.Get("Auto-Submitted") != "" || h.Get("Bcc") != "" {
		t.Fatalf("bad auto value: %v", h)
	}
}

func TestHeaderTextStripsInjection(t *testing.T) {
	for _, in := range []string{"Priya\r\nBcc: x@evil.example", "Priya\nTo: y", "a b", "<Priya> \"x\" \\"} {
		got := HeaderText(in)
		if strings.ContainsAny(got, "\r\n\"<>\\ ") {
			t.Errorf("HeaderText(%q) = %q", in, got)
		}
	}
	if got := HeaderText(strings.Repeat("é", 300)); len([]rune(got)) != maxNameRunes {
		t.Fatalf("cap: %d", len([]rune(got)))
	}
}

func TestVisibleKeepsJoinersAndTags(t *testing.T) {
	keep := string([]rune{0x0915, 0x094D, 0x200D, 0x0937, ' ', 0x2764, 0xFE0F, ' ', 0x1F3F4, 0xE0067, 0xE007F, 0x200C})
	if got := Visible(keep); got != keep {
		t.Fatalf("Visible(%+q) = %+q", keep, got)
	}
	in := string([]rune{0xFEFF, 'a', 0x00AD, 'b', 0x200E, 0x0085, '\t', 'c', '\n', 0x2067, 0xFEFF})
	if got, want := Visible(in), string([]rune{0xFEFF, 'a', 'b', '\t', 'c', '\n'}); got != want {
		t.Fatalf("Visible(%+q) = %+q, want %+q", in, got, want)
	}
}

func parseFile(t *testing.T, name string) Inbound {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	in, err := ParseInbound(raw)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func TestParseInbound(t *testing.T) {
	in := parseFile(t, "text-only.eml")
	if in.From != "priya@example.com" || in.FromName != "Priya Shah" || in.MessageID != "<text-1@mail.example>" ||
		in.Subject != "Re: Hello" || !strings.HasPrefix(in.Text, "Yes, the 14th works.") || in.Attachments != 0 ||
		len(in.Recipients) != 1 || in.Recipients[0] != "reply+0123456789abcdef0123456789abcdef@reply.baoopn.com" {
		t.Fatalf("text-only: %+v", in)
	}
	if body, _, _ := CleanBody(Received{Text: in.Text, Sender: "Bao Nguyen"}); body != "Yes, the 14th works." {
		t.Fatalf("cleaned: %q", body)
	}

	in = parseFile(t, "html-only.eml")
	if in.Text != "" || !strings.Contains(in.HTML, "<b>there</b>") || in.From != "sam@example.com" {
		t.Fatalf("html-only: text %q html %q", in.Text, in.HTML)
	}
	if body, full, _ := CleanBody(Received{Text: in.Text, HTML: in.HTML}); body != "Hello there" || !strings.Contains(full, "Old text") {
		t.Fatalf("html cleaned: %q / %q", body, full)
	}

	in = parseFile(t, "attachment.eml")
	if in.Attachments != 2 || strings.TrimSpace(in.Text) != "See attached." || in.Headers["subject"] != "With files" {
		t.Fatalf("attachment: %d %q", in.Attachments, in.Text)
	}
	if len(in.Recipients) != 3 || in.Recipients[1] != "other@example.com" {
		t.Fatalf("recipients: %v", in.Recipients)
	}

	in = parseFile(t, "latin1.eml")
	if strings.TrimSpace(in.Text) != "Bonjour, ça va très bien." || in.Subject != "Café" || in.FromName != "René" {
		t.Fatalf("latin1: %+v", in)
	}

	if _, err := ParseInbound([]byte("Content-Type: multipart/mixed\r\n\r\nno boundary")); err == nil {
		t.Log("enmime accepted a broken multipart; fine as long as it does not panic")
	}
}

func TestTextToHTMLEscapes(t *testing.T) {
	got := TextToHTML("<script>x</script>\nline & two")
	if strings.Contains(got, "<script>") || !strings.Contains(got, "&lt;script&gt;x&lt;/script&gt;<br>\nline &amp; two") {
		t.Fatalf("got %q", got)
	}
}
