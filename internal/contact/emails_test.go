package contact

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
)

var update = flag.Bool("update", false, "rewrite the golden emails in testdata/email")

var (
	goldenBrand = bmail.BrandOf("https://example.test")
	goldenAt    = time.Date(2026, 11, 7, 14, 5, 0, 0, time.UTC)
)

const goldenLink = "https://example.test/admin/inbox/123"

func goldenSub() submission {
	return submission{name: "Priya Shah", email: "priya@example.test", subject: "A new portfolio site",
		message: "Hi! I saw your work and would like to talk about a project.\n\nCould we have a call next week?\nThanks,\nPriya",
		at:      goldenAt}
}

// hostileSub is a form a relay or phishing attempt might send.
func hostileSub() submission {
	return submission{name: "<img src=x onerror=alert(1)>", email: "victim@example.test", subject: `"><script>x</script>`,
		message: "QZXMARKERQZX unique words </td></table>{{.}} javascript:alert(1) visit evil.example now or https://evil.example/login?next=x, www.evil.example, mail admin@evil.example", at: goldenAt}
}

func goldenThread() thread {
	return thread{ThreadSummary: ThreadSummary{ID: 123, Name: "Priya Shah", Email: "priya@example.test", Subject: "A new portfolio site"}}
}

// goldenSettings are fixed settings for the golden files, so they do not
// change with the site's defaults.
func goldenSettings() sitesettings.Settings {
	return sitesettings.Settings{
		AutoReplyText: "Hi {name},\n\nThanks for reaching out. I got your message and will get back to you soon. You can reply to this email to add anything.\n\nSam",
		Signature:     "Sam Owner\nhttps://example.test",
	}
}

func goldenDocs() map[string]bmail.Doc {
	s := goldenSettings()
	ack, _ := ackDoc(goldenBrand, 123, goldenSub(), s.AutoReplyText, s.Signature)
	hostile, _ := ackDoc(goldenBrand, 123, hostileSub(), s.AutoReplyText, s.Signature)
	return map[string]bmail.Doc{
		"new-thread":         newThreadDoc(goldenBrand, goldenLink, 123, goldenSub()),
		"ack":                ack,
		"ack-hostile":        hostile,
		"new-thread-hostile": newThreadDoc(goldenBrand, goldenLink, 123, hostileSub()),
		"inbound-hostile": inboundDoc(goldenBrand, goldenLink, thread{ThreadSummary: ThreadSummary{ID: 123,
			Name: "Support team support@evil.example", Email: "x@evil.example", Subject: "Reset at http://evil.example"}},
			hostileSub().message),
		"inbound": inboundDoc(goldenBrand, goldenLink, goldenThread(), "Sounds good. Tuesday works for me.\n\nPriya"),
		"reply": replyDoc(goldenBrand, "Re: A new portfolio site",
			"Hi Priya,\n\nTuesday at 10 works. I will send a link.\n\n> Could we have a call next week?\n> Thanks,\n\nTalk soon.", s.Signature),
	}
}

func renderDoc(t *testing.T, d bmail.Doc) (string, string) {
	t.Helper()
	text, html, err := d.Render()
	if err != nil {
		t.Fatal(err)
	}
	if text == "" || html == "" {
		t.Fatal("both parts must be present")
	}
	return text, html
}

// TestEmailGolden renders every email; `make mail-preview` rewrites the files,
// which are also the preview to open in a browser.
func TestEmailGolden(t *testing.T) {
	dir := filepath.Join("testdata", "email")
	for name, d := range goldenDocs() {
		text, html := renderDoc(t, d)
		for ext, got := range map[string]string{".txt": text, ".html": html} {
			path := filepath.Join(dir, name+ext)
			if *update {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				continue
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run make mail-preview)", err)
			}
			if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
				t.Errorf("%s differs from the golden file (run make mail-preview and review the diff)", path)
			}
		}
	}
}

var (
	tagRe   = regexp.MustCompile(`<\s*/?\s*([a-zA-Z][a-zA-Z0-9]*)`)
	inTagRe = regexp.MustCompile(`<[a-zA-Z][^>]*>`)
	hrefRe  = regexp.MustCompile(`href="([^"]*)"`)
	allowed = map[string]bool{"html": true, "head": true, "meta": true, "title": true, "style": true,
		"body": true, "div": true, "table": true, "tr": true, "td": true, "span": true, "a": true, "br": true, "strong": true}
)

func TestEmailsAreSafeHTML(t *testing.T) {
	for name, d := range goldenDocs() {
		_, html := renderDoc(t, d)
		body := regexp.MustCompile(`<!--\[if mso\]>.*?<!\[endif\]-->`).ReplaceAllString(html, "")
		for _, m := range tagRe.FindAllStringSubmatch(body, -1) {
			if !allowed[strings.ToLower(m[1])] {
				t.Errorf("%s: tag <%s>", name, m[1])
			}
		}
		if l := strings.ToLower(body); strings.Contains(l, "<script") || strings.Contains(l, "<img") {
			t.Errorf("%s: raw script or img", name)
		}
		for _, tag := range inTagRe.FindAllString(body, -1) {
			if l := strings.ToLower(tag); strings.Contains(l, "onerror") || strings.Contains(l, "javascript:") {
				t.Errorf("%s: %s", name, tag)
			}
		}
		for _, m := range hrefRe.FindAllStringSubmatch(html, -1) {
			if !strings.HasPrefix(m[1], goldenBrand.URL) {
				t.Errorf("%s: href %q", name, m[1])
			}
		}
	}
}

// TestEmailParity checks every value shown in HTML is also in the text part.
func TestEmailParity(t *testing.T) {
	for name, d := range goldenDocs() {
		text, _ := renderDoc(t, d)
		for _, b := range d.Blocks {
			for _, f := range b.Fields {
				if !strings.Contains(text, f.Label+": "+f.Value) {
					t.Errorf("%s: field %q missing from text", name, f.Label)
				}
			}
			for _, l := range strings.Split(b.Text, "\n") {
				if !strings.Contains(text, l) {
					t.Errorf("%s: line %q missing from text", name, l)
				}
			}
			if b.Kind == bmail.Button && !strings.Contains(text, b.URL) {
				t.Errorf("%s: button url missing from text", name)
			}
		}
	}
}

func TestReplyTextIsUnchanged(t *testing.T) {
	body := "Hi,\n\n> old line\n>> older\nnew line"
	sig := "Name\nhttps://example.test"
	text, html := renderDoc(t, replyDoc(goldenBrand, "Re: x", body, sig))
	if text != body+sigSep+sig {
		t.Fatalf("reply text %q", text)
	}
	if !strings.Contains(html, "border-left:3px solid #e3d7fb") || !strings.Contains(html, "old line<br>&gt; older") {
		t.Fatalf("no quote cell in %s", html)
	}
	if text, _ := renderDoc(t, replyDoc(goldenBrand, "Re: x", "plain", "")); text != "plain" {
		t.Fatalf("no signature: %q", text)
	}
}

func TestAckShowsNoMessage(t *testing.T) {
	s := sitesettings.Defaults()
	for _, sub := range []submission{goldenSub(), hostileSub()} {
		d, _ := ackDoc(goldenBrand, 123, sub, s.AutoReplyText, s.Signature)
		text, html := renderDoc(t, d)
		r := []rune(sub.message)
		for i := 0; i+12 <= len(r); i++ {
			w := string(r[i : i+12])
			if strings.Contains(text, w) || strings.Contains(html, w) {
				t.Fatalf("the ack quotes the message: %q", w)
			}
		}
		if d.Preheader != "Got your message. Reference #123." {
			t.Fatalf("preheader %q", d.Preheader)
		}
		if !strings.HasSuffix(text, "If that wasn't you, you can ignore it.") || !strings.Contains(html, "If that wasn&#39;t you") {
			t.Fatal("footer missing")
		}
	}
	d, header := ackDoc(goldenBrand, 123, hostileSub(), s.AutoReplyText, s.Signature)
	text, _ := renderDoc(t, d)
	if header != "Re: your message" || !strings.Contains(text, "Subject: your message") || !strings.HasPrefix(text, "Hi there,") {
		t.Fatalf("hostile ack: %q %q", header, text)
	}
	_, header = ackDoc(goldenBrand, 123, goldenSub(), s.AutoReplyText, s.Signature)
	if header != "Re: A new portfolio site" {
		t.Fatalf("header %q", header)
	}
	want := strings.ReplaceAll(s.AutoReplyText, "{name}", "Priya Shah") +
		"\n\nYour message\nReference: #123\nSubject: A new portfolio site\nSent: 7 Nov 2026, 14:05 UTC" +
		sigSep + s.Signature + "\n\nYou got this email because this address was entered on the contact form at example.test. If that wasn't you, you can ignore it."
	if d, _ := ackDoc(goldenBrand, 123, goldenSub(), s.AutoReplyText, s.Signature); mustText(t, d) != want {
		t.Fatalf("ack text:\n%q\nwant\n%q", mustText(t, d), want)
	}
}

func mustText(t *testing.T, d bmail.Doc) string {
	text, _ := renderDoc(t, d)
	return text
}

func TestSafeSubject(t *testing.T) {
	for in, ok := range map[string]bool{
		"Hello there":            true,
		"A new portfolio site":   true,
		"Mr. Kim asks":           true,
		"example.com":            false,
		"exa mple.com":           false,
		"exam．com":               false,
		"see example。com":        false,
		"ехаmрlе.соm":            false, // Cyrillic lookalikes
		"call 5551234567":        false,
		"call 555 1234":          true,
		"hello​world":            false, // zero-width space
		"hello ‮gnp.exe":         false, // RLO
		"ｈｔｔｐ stuff":             false, // full-width http
		"ｅｘａｍｐｌｅ．ｃｏｍ":            false, // full-width domain
		"me@x":                   false,
		"a/b":                    false,
		"WWW thing":              false,
		"a\r\nBcc: x@example.co": false,
		strings.Repeat("a", 81):  false,
		strings.Repeat("a", 80):  true,
		"":                       false,
	} {
		got, gotOK := safeSubject(in)
		if gotOK != ok {
			t.Errorf("safeSubject(%q) = %v, want %v", in, gotOK, ok)
		}
		if gotOK && (got == "" || !utf8.ValidString(got)) {
			t.Errorf("safeSubject(%q) returned %q", in, got)
		}
	}
	if got, _ := safeSubject("Ｈｅｌｌｏ"); got != "Hello" {
		t.Errorf("the NFKC form is shown: %q", got)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"Priya Shah": "Priya Shah", "": "there", "visit http://evil.example": "there",
		"www.evil.com": "there", "a@b.c": "there", "Dr. J. Smith": "there", "Mr. Kim": "Mr. Kim",
		"evil.com": "there", "exa mple.com": "there", "exam．com": "there", "ехаmрlе.соm": "there",
		"call 5551234567": "there", "Pri​ya": "there", "Priya ‮Shah": "there", "ｈｔｔｐ": "there",
		"ｅｘａｍｐｌｅ．ｃｏｍ": "there", "Ｐｒｉｙａ": "Priya", "<img src=x onerror=alert(1)>": "there",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalAddress(t *testing.T) {
	for in, want := range map[string]string{"A@X.com": "a@x.com", "a+tag@x.com": "a@x.com", " a+b+c@x.com ": "a@x.com", "nope": "nope"} {
		if got := normalAddress(in); got != want {
			t.Errorf("normalAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

// acks counts the acknowledgements sent to addr.
func (e *env) acks(addr string) int {
	n := 0
	for _, m := range e.mailer.messages() {
		if m.AutoSubmitted == "auto-replied" && m.To == addr {
			n++
		}
	}
	return n
}

func (e *env) globalAcks() int {
	e.m.ackMu.Lock()
	defer e.m.ackMu.Unlock()
	return len(e.m.acks)
}

func TestAckPerAddressCap(t *testing.T) {
	e := newEnv(t, true, testDomain)
	e.withTurnstile()
	s := sitesettings.Defaults()
	s.AutoReplyEnabled = true
	putSettings(t, e, s)
	send := func(addr string) {
		t.Helper()
		if code, raw := e.form("Sam", addr, "Hi", "a long enough message"); code != 201 {
			t.Fatalf("form: %d %s", code, raw)
		}
		e.wait()
	}
	for range 2 {
		send("a@x.com")
	}
	send("a+tag@x.com") // the +tag counts with the plain address
	if n := e.acks("a@x.com") + e.acks("a+tag@x.com"); n != 3 {
		t.Fatalf("3 acks under the cap, got %d", n)
	}
	slots := e.globalAcks()
	before := len(e.mailer.messages())
	send("A@x.com")
	sent := e.mailer.messages()[before:]
	if len(sent) != 1 || sent[0].To != ownerAddr {
		t.Fatalf("the 4th form only notifies the owner: %+v", sent)
	}
	if n := e.count(`SELECT count(*) FROM threads`); n != 4 {
		t.Fatalf("the 4th thread is stored: %d", n)
	}
	if e.globalAcks() != slots {
		t.Fatal("a refused ack used a slot of the site-wide cap")
	}
	send("other@x.com")
	if e.acks("other@x.com") != 1 {
		t.Fatal("another address is capped too")
	}
	if _, err := e.db.Exec(t.Context(), `UPDATE threads SET created_at = now() - interval '25 hours' WHERE email LIKE 'a%@x.com'`); err != nil {
		t.Fatal(err)
	}
	send("a@x.com")
	if n := e.acks("a@x.com"); n != 3 {
		t.Fatalf("after 24 hours the address gets an ack again: %d", n)
	}
}

func TestAckCapFailsClosed(t *testing.T) {
	e := newEnv(t, true, testDomain)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if e.m.ackAllowed(ctx, 1, "a@x.com") {
		t.Fatal("a failed count allowed the ack")
	}
}

func TestReplyWithQuoteKeepsThreading(t *testing.T) {
	e := newEnv(t, true, testDomain)
	id, tok := e.thread("priya@example.com")
	if code := e.hook(email("Priya <priya@example.com>", replyAddr(tok), "<in-1@mail.example>", "Answer"), replyAddr(tok)); code != 204 {
		t.Fatalf("hook: %d", code)
	}
	e.wait()
	before := len(e.mailer.messages())
	body := `{"body":"Sure.\n\n> Answer\n> more\n\nBye","request_id":"req-quote-1"}`
	if code, raw := e.do("POST", "/threads/"+strconv.FormatInt(id, 10)+"/messages", body, nil); code != 201 {
		t.Fatalf("reply: %d %s", code, raw)
	}
	m := e.mailer.messages()[before]
	if m.Text != "Sure.\n\n> Answer\n> more\n\nBye\n\n-- \nBao Nguyen\nhttps://baoopn.com" ||
		!strings.Contains(m.HTML, "border-left:3px solid #e3d7fb") || !strings.Contains(m.HTML, "Answer<br>more") {
		t.Fatalf("reply: %q", m.Text)
	}
	if m.To != "priya@example.com" || m.ReplyTo != replyAddr(tok) || m.Subject != "Re: Hello" || m.AutoSubmitted != "" ||
		m.InReplyTo != "<in-1@mail.example>" || len(m.References) != 1 || m.References[0] != "<in-1@mail.example>" {
		t.Fatalf("threading changed: %+v", m)
	}
}

// TestAlertsHaveNoLiveLinks checks a stranger's URLs and addresses never form
// one text run (which mail clients would turn into a link), and the only
// links are the inbox button and the footer.
func TestAlertsHaveNoLiveLinks(t *testing.T) {
	email := regexp.MustCompile(`[^\s<>@]+@[^\s<>@]+\.[a-z]{2,}`)
	for _, name := range []string{"new-thread-hostile", "inbound-hostile"} {
		_, html := renderDoc(t, goldenDocs()[name])
		body := html[strings.Index(html, "<body"):]
		for _, run := range regexp.MustCompile(`<[^>]*>`).Split(body, -1) {
			l := strings.ToLower(run)
			if strings.Contains(l, "http://") || strings.Contains(l, "https://") || strings.Contains(l, "www.") ||
				email.MatchString(l) || strings.Contains(l, "evil.example") {
				t.Errorf("%s: live link text %q", name, run)
			}
		}
		links := regexp.MustCompile(`<a [^>]*href="([^"]*)"`).FindAllStringSubmatch(html, -1)
		if len(links) != 2 || links[0][1] != goldenLink || links[1][1] != goldenBrand.URL {
			t.Errorf("%s: links %v", name, links)
		}
	}
}
