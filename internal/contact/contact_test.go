package contact

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
)

func TestFormCreatesThreadAndNotifies(t *testing.T) {
	e := newEnv(t, true, testDomain)
	e.withTurnstile()
	ctx := context.Background()
	s := sitesettings.Defaults()
	s.AutoReplyEnabled = true
	putSettings(t, e, s)

	code, raw := e.form("Priya Shah", "Priya@Example.com", "", "I would like to talk about a project.\nSecond line.")
	if code != 201 || string(raw) != `{"ok":true}` {
		t.Fatalf("form: %d %s", code, raw)
	}
	e.wait()
	var id int64
	var tok, name, mail, subject, preview string
	if err := e.db.QueryRow(ctx, `SELECT id, reply_token, name, email, subject, last_preview FROM threads`).Scan(&id, &tok, &name, &mail, &subject, &preview); err != nil {
		t.Fatal(err)
	}
	if name != "Priya Shah" || mail != "priya@example.com" || subject != "Message from Priya Shah" || len(tok) != 32 ||
		preview != "I would like to talk about a project. Second line." {
		t.Fatalf("thread: %q %q %q %q", name, mail, subject, preview)
	}
	d := e.detail(id)
	if len(d.Messages) != 1 || d.Messages[0].Source != "form" || d.Messages[0].Direction != "inbound" || !d.Thread.Unread || !d.ReplyEnabled {
		t.Fatalf("detail: %+v", d)
	}
	raw, _ = json.Marshal(d)
	if strings.Contains(string(raw), tok) || strings.Contains(string(raw), "reply_token") {
		t.Fatal("the reply token must not appear in JSON")
	}

	sent := e.mailer.messages()
	if len(sent) != 2 {
		t.Fatalf("want notification and acknowledgement, got %d", len(sent))
	}
	note, ack := sent[0], sent[1]
	if note.To != ownerAddr || note.ReplyTo != "" || note.AutoSubmitted != "auto-generated" ||
		!strings.Contains(note.Text, "https://baoopn.com/admin/inbox/"+strconv.FormatInt(id, 10)) || strings.Contains(note.Text, tok) {
		t.Fatalf("notification: %+v", note)
	}
	if ack.To != "priya@example.com" || ack.ReplyTo != replyAddr(tok) || ack.AutoSubmitted != "auto-replied" ||
		ack.Subject != "Re: your message to Bao Nguyen" || strings.Contains(ack.Text, "Message from Priya") || !strings.HasPrefix(ack.Text, "Hi Priya Shah,") ||
		strings.Contains(ack.Text, "I would like to talk about a project") ||
		!strings.Contains(ack.Text, "\n\n-- \nBao Nguyen\nhttps://baoopn.com\n\nYou got this email") ||
		!strings.Contains(ack.HTML, "Your message") || !strings.Contains(note.HTML, "Open in inbox") {
		t.Fatalf("acknowledgement: %+v", ack)
	}
	if n := e.count(`SELECT count(*) FROM thread_messages`); n != 1 {
		t.Fatalf("the acknowledgement is not stored: %d messages", n)
	}
}

func putSettings(t *testing.T, e *env, s sitesettings.Settings) {
	t.Helper()
	body, _ := json.Marshal(s)
	code, raw := e.do("PUT", "/settings", string(body), nil)
	if code != 200 {
		t.Fatalf("settings: %d %s", code, raw)
	}
}

func TestFormRules(t *testing.T) {
	e := newEnv(t, true, testDomain)
	body, _ := json.Marshal(ContactInput{Name: "Bot", Email: "bot@example.com", Message: "buy things now please", Website: "http://spam.example", StartedAt: 1})
	if code, raw := e.do("POST", "/contact", string(body), nil); code != 201 || string(raw) != `{"ok":true}` {
		t.Fatalf("honeypot: %d %s", code, raw)
	}
	body, _ = json.Marshal(ContactInput{Name: "Fast", Email: "f@example.com", Message: "a message typed too fast", StartedAt: time.Now().UnixMilli() - 500})
	if code, raw := e.do("POST", "/contact", string(body), nil); code != 422 || !strings.Contains(string(raw), "wait a moment") {
		t.Fatalf("started_at: %d %s", code, raw)
	}
	if code, _ := e.do("POST", "/contact", `{"name":"x","extra":1}`, nil); code != 400 {
		t.Fatalf("unknown field: %d", code)
	}
	if code, raw := e.form("A", "a@example.com", "", "short"); code != 422 || !strings.Contains(string(raw), "validation") {
		t.Fatalf("validation: %d %s", code, raw)
	}
	e.wait()
	if n := e.count(`SELECT count(*) FROM threads`); n != 0 || len(e.mailer.messages()) != 0 {
		t.Fatalf("nothing is stored or sent: %d threads", n)
	}

	s := sitesettings.Defaults()
	s.ContactEnabled = false
	putSettings(t, e, s)
	if code, raw := e.form("Priya", "p@example.com", "", "a long enough message"); code != 503 || !strings.Contains(string(raw), "contact_disabled") {
		t.Fatalf("disabled: %d %s", code, raw)
	}
}

func TestFormRequiresTurnstileWhenConfigured(t *testing.T) {
	e := newEnv(t, false, testDomain)
	e.m.d.TurnstileSecret = "secret"
	e.noTurnstileToken = true
	if code, raw := e.form("Priya", "p@example.com", "", "a long enough message"); code != 422 || !strings.Contains(string(raw), "captcha_failed") {
		t.Fatalf("missing token: %d %s", code, raw)
	}
}

func TestMailDisabled(t *testing.T) {
	e := newEnv(t, false, testDomain)
	code, _ := e.form("Priya", "p@example.com", "Hi", "a long enough message")
	e.wait()
	if code != 201 || e.count(`SELECT count(*) FROM threads`) != 1 || len(e.mailer.messages()) != 0 {
		t.Fatalf("form: %d, %d sends", code, len(e.mailer.messages()))
	}
	var id int64
	_ = e.db.QueryRow(context.Background(), `SELECT id FROM threads`).Scan(&id)
	path := "/threads/" + strconv.FormatInt(id, 10)
	if code, raw := e.do("POST", path+"/messages", `{"body":"hello","request_id":"req-00000001"}`, nil); code != 503 || !strings.Contains(string(raw), "mail_disabled") {
		t.Fatalf("reply: %d %s", code, raw)
	}
	if code, _ := e.do("POST", path+"/messages/1/retry", "", nil); code != 503 {
		t.Fatalf("retry: %d", code)
	}
	if n := e.count(`SELECT count(*) FROM thread_messages WHERE direction = 'outbound'`); n != 0 {
		t.Fatalf("no outbound row: %d", n)
	}
	if e.detail(id).ReplyEnabled {
		t.Fatal("reply_enabled is false without mail")
	}
}

func TestReplyIsIdempotentAndThreaded(t *testing.T) {
	e := newEnv(t, true, testDomain)
	id, tok := e.thread("priya@example.com")
	// Two inbound emails give the threading headers.
	for _, mid := range []string{"<in-1@mail.example>", "<in-2@mail.example>"} {
		if code := e.hook(email("Priya <priya@example.com>", replyAddr(tok), mid, "Answer "+mid), replyAddr(tok)); code != 204 {
			t.Fatalf("hook: %d", code)
		}
	}
	e.wait()
	before := len(e.mailer.messages())
	path := "/threads/" + strconv.FormatInt(id, 10) + "/messages"
	body := `{"body":"  Thanks, let us talk.  ","request_id":"req-abcdefgh"}`
	code, raw := e.do("POST", path, body, nil)
	var first ThreadMessage
	_ = json.Unmarshal(raw, &first)
	if code != 201 || first.Status != "sent" || first.Body != "Thanks, let us talk." || first.SentAt == nil || first.FromAddress != "bao@mail.baoopn.com" {
		t.Fatalf("reply: %d %s", code, raw)
	}
	code, raw = e.do("POST", path, body, nil)
	var again ThreadMessage
	_ = json.Unmarshal(raw, &again)
	if code != 200 || again.ID != first.ID {
		t.Fatalf("replay: %d %s", code, raw)
	}
	sent := e.mailer.messages()[before:]
	if len(sent) != 1 || e.count(`SELECT count(*) FROM thread_messages WHERE request_id = 'req-abcdefgh'`) != 1 {
		t.Fatalf("one send, one row: %d sends", len(sent))
	}
	m := sent[0]
	if m.To != "priya@example.com" || m.ReplyTo != replyAddr(tok) || m.Subject != "Re: Hello" || m.AutoSubmitted != "" ||
		m.InReplyTo != "<in-2@mail.example>" || len(m.References) != 2 || m.References[0] != "<in-1@mail.example>" ||
		m.Text != "Thanks, let us talk.\n\n-- \nBao Nguyen\nhttps://baoopn.com" || !strings.Contains(m.HTML, "<br>") {
		t.Fatalf("mail: %+v", m)
	}
	d := e.detail(id)
	if d.Thread.LastDirection != "outbound" || d.Thread.MessageCount != 4 || d.Thread.LastPreview != "Thanks, let us talk." {
		t.Fatalf("thread: %+v", d.Thread)
	}
	if code, _ := e.do("POST", path, `{"body":"x","request_id":"bad id"}`, nil); code != 422 {
		t.Fatalf("bad request_id: %d", code)
	}
	if code, _ := e.do("POST", "/threads/999999/messages", `{"body":"x","request_id":"req-abcdefgh"}`, nil); code != 404 {
		t.Fatalf("no thread: %d", code)
	}
	if code, _ := e.do("PATCH", "/threads/"+strconv.FormatInt(id, 10), `{"status":"spam"}`, nil); code != 200 {
		t.Fatal("patch")
	}
	if code, _ := e.do("POST", path, `{"body":"x","request_id":"req-zzzzzzzz"}`, nil); code != 409 {
		t.Fatalf("spam thread: %d", code)
	}
}

func TestReplyFailsThenRetries(t *testing.T) {
	e := newEnv(t, true, testDomain)
	id, _ := e.thread("priya@example.com")
	path := "/threads/" + strconv.FormatInt(id, 10) + "/messages"
	e.mailer.fail = errors.Join(bmail.ErrRefused, errors.New("smtp 550"))
	code, raw := e.do("POST", path, `{"body":"hello","request_id":"req-00000001"}`, nil)
	var msg ThreadMessage
	_ = json.Unmarshal(raw, &msg)
	if code != 201 || msg.Status != "failed" || !strings.Contains(msg.Error, "550") {
		t.Fatalf("failed: %d %s", code, raw)
	}
	e.mailer.fail = nil
	retry := path + "/" + strconv.FormatInt(msg.ID, 10) + "/retry"
	code, raw = e.do("POST", retry, "", nil)
	_ = json.Unmarshal(raw, &msg)
	if code != 200 || msg.Status != "sent" || msg.Error != "" {
		t.Fatalf("retry: %d %s", code, raw)
	}
	var status string
	_ = e.db.QueryRow(context.Background(), `SELECT status FROM thread_messages WHERE id = $1`, msg.ID).Scan(&status)
	if status != "sent" {
		t.Fatalf("stored status %q", status)
	}
	if code, raw := e.do("POST", retry, "", nil); code != 409 || !strings.Contains(string(raw), "not_retryable") {
		t.Fatalf("retry of a sent message: %d %s", code, raw)
	}
	if code, _ := e.do("POST", path+"/999999/retry", "", nil); code != 404 {
		t.Fatalf("retry of nothing: %d", code)
	}
}

func TestReplyToFallsBackToNotifyAddress(t *testing.T) {
	e := newEnv(t, true, "")
	e.withTurnstile()
	s := sitesettings.Defaults()
	s.AutoReplyEnabled = true
	putSettings(t, e, s)
	id, _ := e.thread("priya@example.com")
	if code, _ := e.do("POST", "/threads/"+strconv.FormatInt(id, 10)+"/messages", `{"body":"hello","request_id":"req-00000001"}`, nil); code != 201 {
		t.Fatal(code)
	}
	e.form("Sam", "sam@example.com", "Hi", "a long enough message")
	e.wait()
	sent := e.mailer.messages()
	if len(sent) != 3 || sent[0].ReplyTo != ownerAddr || sent[2].ReplyTo != ownerAddr || sent[2].AutoSubmitted != "auto-replied" {
		t.Fatalf("sent: %+v", sent)
	}
	// Without an inbound domain the hook is off.
	if code := e.hook(email("p@example.com", "x@y.z", "<a@b>", "hi"), ""); code != 404 {
		t.Fatalf("hook: %d", code)
	}
}

func TestThreadsListReadUnread(t *testing.T) {
	e := newEnv(t, true, testDomain)
	a, _ := e.thread("a@example.com")
	b, _ := e.thread("b_100%@example.com")
	sa, sb := strconv.FormatInt(a, 10), strconv.FormatInt(b, 10)
	unread := func() int64 {
		_, raw := e.do("GET", "/threads/unread-count", "", nil)
		var out struct{ Count int64 }
		_ = json.Unmarshal(raw, &out)
		return out.Count
	}
	if unread() != 2 {
		t.Fatal("two unread")
	}
	if code, _ := e.do("POST", "/threads/"+sa+"/read", "", nil); code != 204 || unread() != 1 {
		t.Fatalf("read: %d", code)
	}
	e.detail(b) // opening does not mark read
	if unread() != 1 {
		t.Fatal("opening a thread does not mark it read")
	}
	if code, _ := e.do("POST", "/threads/"+sa+"/unread", "", nil); code != 204 || unread() != 2 {
		t.Fatalf("unread: %d", code)
	}
	if code, _ := e.do("POST", "/threads/999999/read", "", nil); code != 404 {
		t.Fatalf("read missing: %d", code)
	}
	list := func(q string) []ThreadSummary {
		code, raw := e.do("GET", "/threads"+q, "", nil)
		if code != 200 {
			t.Fatalf("list %s: %d %s", q, code, raw)
		}
		var out []ThreadSummary
		_ = json.Unmarshal(raw, &out)
		return out
	}
	if got := list(""); len(got) != 2 || got[0].ID != b {
		t.Fatalf("list: %+v", got)
	}
	if got := list("?q=100%25"); len(got) != 1 || got[0].ID != b {
		t.Fatalf("q escapes %%: %+v", got)
	}
	if got := list("?q=_"); len(got) != 1 {
		t.Fatalf("q escapes _: %+v", got)
	}
	if code, _ := e.do("GET", "/threads?status=nope", "", nil); code != 422 {
		t.Fatal("bad status")
	}
	if code, _ := e.do("PATCH", "/threads/"+sb, `{"status":"archived"}`, nil); code != 200 || len(list("?status=archived")) != 1 || unread() != 1 {
		t.Fatal("archive")
	}
	if code, _ := e.do("PATCH", "/threads/"+sb, `{"status":"gone"}`, nil); code != 422 {
		t.Fatal("bad patch")
	}
	if code, _ := e.do("DELETE", "/threads/"+sb, "", nil); code != 204 || e.count(`SELECT count(*) FROM thread_messages WHERE thread_id = $1`, b) != 0 {
		t.Fatal("delete cascades")
	}
	if code, _ := e.do("DELETE", "/threads/"+sb, "", nil); code != 404 {
		t.Fatal("delete twice")
	}
	if code, _ := e.do("GET", "/threads/abc", "", nil); code != 404 {
		t.Fatal("bad id")
	}
}

func TestAcknowledgementNeedsTurnstileAndIsCapped(t *testing.T) {
	e := newEnv(t, true, testDomain)
	s := sitesettings.Defaults()
	s.AutoReplyEnabled = true
	putSettings(t, e, s)
	e.form("Sam", "sam@example.com", "Hi", "a long enough message")
	e.wait()
	if sent := e.mailer.messages(); len(sent) != 1 || sent[0].To != ownerAddr {
		t.Fatalf("without Turnstile only the owner is notified: %+v", sent)
	}

	ctx := context.Background()
	tid, _ := e.thread("visitor@example.com")
	for i := range ackPerHour {
		if !e.m.reserveAck(ctx, tid, "visitor"+strconv.Itoa(i)+"@example.com") {
			t.Fatalf("ack %d refused under the cap", i)
		}
	}
	if e.m.reserveAck(ctx, tid, "one-more@example.com") {
		t.Fatal("ack over the hourly cap allowed")
	}
	if _, err := e.db.Exec(ctx, `UPDATE contact_acks SET sent_at = now() - interval '61 minutes'`); err != nil {
		t.Fatal(err)
	}
	if !e.m.reserveAck(ctx, tid, "one-more@example.com") {
		t.Fatal("cap did not reset after an hour")
	}
}

func TestAcknowledgementPerAddressCap(t *testing.T) {
	e := newEnv(t, true, testDomain)
	ctx := context.Background()
	tid, _ := e.thread("visitor@example.com")
	// One Gmail mailbox under three spellings.
	for i, addr := range []string{"a.b@gmail.com", "AB+x@googlemail.com", "a.b+y@Gmail.com"} {
		if !e.m.reserveAck(ctx, tid, addr) {
			t.Fatalf("ack %d (%s) refused under the cap", i, addr)
		}
	}
	if e.m.reserveAck(ctx, tid, "ab@gmail.com") {
		t.Fatal("fourth ack to one mailbox allowed")
	}
	if !e.m.reserveAck(ctx, tid, "ab@example.com") {
		t.Fatal("another address refused")
	}
	if n := e.count(`SELECT count(*) FROM contact_acks WHERE address_key = 'ab@gmail.com'`); n != ackPerAddress {
		t.Fatalf("rows: %d", n)
	}
	// Older than 24 hours no longer counts.
	if _, err := e.db.Exec(ctx, `UPDATE contact_acks SET sent_at = now() - interval '25 hours'`); err != nil {
		t.Fatal(err)
	}
	if !e.m.reserveAck(ctx, tid, "ab@gmail.com") {
		t.Fatal("cap did not reset after a day")
	}
}
