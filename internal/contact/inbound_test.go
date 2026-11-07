package contact

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

func TestInboundHappyPathAndDuplicate(t *testing.T) {
	e := newEnv(t, true, testDomain)
	ctx := context.Background()
	id, tok := e.thread("priya@example.com")
	path := "/threads/" + strconv.FormatInt(id, 10)
	if code, _ := e.do("POST", path+"/read", "", nil); code != 204 {
		t.Fatal("read")
	}
	// Our reply, which the visitor's mail app quotes back.
	if code, _ := e.do("POST", path+"/messages", `{"body":"Shall we talk on Monday?","request_id":"req-00000001"}`, nil); code != 201 {
		t.Fatal("reply")
	}
	raw := email("Priya Shah <Priya@Example.com>", "Bao <"+replyAddr(tok)+">", "<in-1@mail.example>",
		"Yes, Monday works.\r\n\r\nOn Mon, Oct 5, 2026 at 3:14 PM Bao Nguyen <bao@mail.baoopn.com> wrote:\r\n> Shall we talk on Monday?")
	if code := e.hook(raw, replyAddr(tok)); code != 204 {
		t.Fatalf("hook: %d", code)
	}
	e.wait()
	d := e.detail(id)
	last := d.Messages[len(d.Messages)-1]
	if len(d.Messages) != 3 || last.Source != "email" || last.Body != "Yes, Monday works." || last.OtherSender ||
		!strings.Contains(last.FullBody, "> Shall we talk on Monday?") || last.FromAddress != "priya@example.com" {
		t.Fatalf("messages: %+v", d.Messages)
	}
	if !d.Thread.Unread || d.Thread.LastDirection != "inbound" || d.Thread.MessageCount != 3 || d.Thread.LastPreview != "Yes, Monday works." {
		t.Fatalf("thread: %+v", d.Thread)
	}
	notes := 0
	for _, m := range e.mailer.messages() {
		if m.To == ownerAddr {
			notes++
			if !strings.Contains(m.Subject, "New reply") || strings.Contains(m.Text, tok) {
				t.Fatalf("notification: %+v", m)
			}
		}
	}
	if notes != 1 {
		t.Fatalf("notifications: %d", notes)
	}
	// The worker delivers again: stored once.
	if code := e.hook(raw, replyAddr(tok)); code != 204 {
		t.Fatalf("again: %d", code)
	}
	// A second email within ten minutes stores but does not notify again.
	if code := e.hook(email("someone.else@example.com", replyAddr(tok), "<in-2@mail.example>", "Me too."), replyAddr(tok)); code != 204 {
		t.Fatal("second")
	}
	e.wait()
	if n := e.count(`SELECT count(*) FROM thread_messages WHERE thread_id = $1 AND source = 'email'`, id); n != 2 {
		t.Fatalf("emails stored: %d", n)
	}
	var other bool
	_ = e.db.QueryRow(ctx, `SELECT other_sender FROM thread_messages WHERE rfc_message_id = '<in-2@mail.example>'`).Scan(&other)
	if !other {
		t.Fatal("other_sender")
	}
	if got := len(e.mailer.messages()); got != 2 {
		t.Fatalf("one reply and one notification sent, got %d", got)
	}
}

func TestInboundRejects(t *testing.T) {
	e := newEnv(t, true, testDomain)
	ctx := context.Background()
	_, tok := e.thread("priya@example.com")
	spamID, spamTok := e.thread("spam@example.com")
	if _, err := e.db.Exec(ctx, `UPDATE threads SET status = 'spam' WHERE id = $1`, spamID); err != nil {
		t.Fatal(err)
	}
	unknown := strings.Repeat("ab", 16)
	cases := []struct {
		name, raw, envTo, reason string
	}{
		{"unknown token", email("p@example.com", replyAddr(unknown), "<u1@x>", "hi there"), replyAddr(unknown), "unknown_token"},
		{"no token", email("p@example.com", "bao@baoopn.com", "<u2@x>", "hi there"), "bao@baoopn.com", "no_token"},
		{"automatic", email("p@example.com", replyAddr(tok), "<u3@x>", "I am away", "Auto-Submitted: auto-replied"), replyAddr(tok), "automatic"},
		{"report", "From: mailer@example.com\r\nTo: " + replyAddr(tok) + "\r\nMIME-Version: 1.0\r\nContent-Type: multipart/report; report-type=delivery-status; boundary=\"b\"\r\n\r\n--b\r\nContent-Type: text/plain\r\n\r\nfailed\r\n--b--\r\n", replyAddr(tok), "automatic"},
		{"self", email("Bao <bao@mail.baoopn.com>", replyAddr(tok), "<u4@x>", "loop"), replyAddr(tok), "self"},
		{"spam thread", email("spam@example.com", replyAddr(spamTok), "<u5@x>", "more spam"), replyAddr(spamTok), "spam_thread"},
		{"empty", email("p@example.com", replyAddr(tok), "<u6@x>", "   "), replyAddr(tok), "empty"},
	}
	for _, c := range cases {
		if code := e.hook(c.raw, c.envTo); code != 204 {
			t.Fatalf("%s: %d", c.name, code)
		}
	}
	rows, err := e.db.Query(ctx, `SELECT reason, recipient, sender, subject FROM inbound_rejects ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var reason, recipient, sender, subject string
		if err := rows.Scan(&reason, &recipient, &sender, &subject); err != nil {
			t.Fatal(err)
		}
		if i >= len(cases) || reason != cases[i].reason {
			t.Fatalf("reject %d: %q", i, reason)
		}
		if strings.Contains(recipient, unknown) || strings.Contains(recipient, tok) || strings.Contains(recipient, spamTok) {
			t.Fatalf("token not redacted: %q", recipient)
		}
		// Not parsed: the sender is the envelope's and there is no subject.
		if reason == "unknown_token" && (recipient != "reply+…@"+testDomain || sender != "bounce@example.com" || subject != "") {
			t.Fatalf("unknown token row: %q %q %q", recipient, sender, subject)
		}
		i++
	}
	if i != len(cases) {
		t.Fatalf("rejects: %d", i)
	}
	if n := e.count(`SELECT count(*) FROM thread_messages WHERE source = 'email'`); n != 0 {
		t.Fatalf("nothing stored: %d", n)
	}
}

func TestInboundReopensArchivedAndFloodLimit(t *testing.T) {
	e := newEnv(t, false, testDomain) // mail disabled: inbound still stores
	ctx := context.Background()
	id, tok := e.thread("priya@example.com")
	if _, err := e.db.Exec(ctx, `UPDATE threads SET status = 'archived', read_at = now() WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	if code := e.hook(email("priya@example.com", replyAddr(tok), "<r1@x>", "Back again"), replyAddr(tok)); code != 204 {
		t.Fatal(code)
	}
	d := e.detail(id)
	if d.Thread.Status != "open" || !d.Thread.Unread || len(d.Messages) != 2 {
		t.Fatalf("reopened: %+v", d.Thread)
	}
	if len(e.mailer.messages()) != 0 {
		t.Fatal("no notification without mail")
	}
	// Up to the limit in 24 hours: 50 stored, the next one rejected.
	for i := 2; i <= floodLimit; i++ {
		if _, err := e.db.Exec(ctx, `INSERT INTO thread_messages (thread_id, direction, source, body, rfc_message_id)
			VALUES ($1, 'inbound', 'email', 'x', $2)`, id, "<f"+strconv.Itoa(i)+"@x>"); err != nil {
			t.Fatal(err)
		}
	}
	if code := e.hook(email("priya@example.com", replyAddr(tok), "<over@x>", "one too many"), replyAddr(tok)); code != 204 {
		t.Fatal(code)
	}
	if e.count(`SELECT count(*) FROM inbound_rejects WHERE reason = 'flood'`) != 1 ||
		e.count(`SELECT count(*) FROM thread_messages WHERE rfc_message_id = '<over@x>'`) != 0 {
		t.Fatal("flood limit")
	}
}

func TestInboundSignatureAndLimits(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	raw := email("priya@example.com", replyAddr(tok), "<s1@x>", "hello")
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	for name, hdr := range map[string]map[string]string{
		"no signature":  {"X-Timestamp": ts},
		"bad signature": {"X-Timestamp": ts, "X-Signature": Sign("wrong", ts, replyAddr(tok), []byte(raw))},
		"stale": func() map[string]string {
			old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
			return map[string]string{"X-Timestamp": old, "X-Signature": Sign(testSecret, old, replyAddr(tok), []byte(raw))}
		}(),
	} {
		hdr["X-Envelope-To"] = replyAddr(tok)
		if code, _ := e.do("POST", "/inbound-email", raw, hdr); code != 401 {
			t.Errorf("%s: %d", name, code)
		}
	}
	big := strings.Repeat("a", maxInboundBytes+10)
	if code, _ := e.do("POST", "/inbound-email", big, map[string]string{"X-Timestamp": ts, "X-Signature": Sign(testSecret, ts, "", []byte(big))}); code != 413 {
		t.Fatalf("too large: %d", code)
	}
	// A message without a Message-ID gets one from its content.
	if code := e.hook(email("priya@example.com", replyAddr(tok), "", "no id here"), replyAddr(tok)); code != 204 {
		t.Fatal(code)
	}
	var mid string
	_ = e.db.QueryRow(context.Background(), `SELECT rfc_message_id FROM thread_messages WHERE body = 'no id here'`).Scan(&mid)
	if !strings.HasSuffix(mid, "@inbound.invalid>") || len(mid) != len("<@inbound.invalid>")+32 {
		t.Fatalf("generated id %q", mid)
	}

	off := newEnv(t, true, testDomain)
	off.m.d.InboundSecret = ""
	if code := off.hook(raw, replyAddr(tok)); code != 404 {
		t.Fatalf("no secret: %d", code)
	}
}

func TestInboundTokenOnlyFromEnvelopeAndParsedLast(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	parsed := 0
	e.m.parse = func(raw []byte) (bmail.Inbound, error) {
		parsed++
		return bmail.ParseInbound(raw)
	}
	unknown := strings.Repeat("cd", 16)
	garbage := "\x00\xff not a mail at all \r\n--\r\n\x01"
	// A token in the headers only does not count: the envelope is what was signed.
	if code := e.hook(email("p@example.com", replyAddr(tok), "<h1@x>", "via header"), "bao@"+testDomain); code != 204 {
		t.Fatal(code)
	}
	if code := e.hook(garbage, replyAddr(unknown)); code != 204 {
		t.Fatal(code)
	}
	if parsed != 0 {
		t.Fatalf("parsed %d junk emails", parsed)
	}
	if e.count(`SELECT count(*) FROM inbound_rejects WHERE reason = 'no_token'`) != 1 ||
		e.count(`SELECT count(*) FROM inbound_rejects WHERE reason = 'unknown_token'`) != 1 ||
		e.count(`SELECT count(*) FROM thread_messages WHERE source = 'email'`) != 0 {
		t.Fatal("rejects")
	}
	// A live thread: parsed once.
	if code := e.hook(email("priya@example.com", replyAddr(tok), "<h2@x>", "real"), replyAddr(tok)); code != 204 || parsed != 1 {
		t.Fatalf("live: %d, parsed %d", code, parsed)
	}
}

func TestInboundRetriedDeliveryStoresAndNotifiesOnce(t *testing.T) {
	e := newEnv(t, true, testDomain)
	id, tok := e.thread("priya@example.com")
	raw := email("priya@example.com", replyAddr(tok), "<again@x>", "Only once, please.")
	for i := 0; i < 3; i++ {
		if code := e.hook(raw, replyAddr(tok)); code != 204 {
			t.Fatalf("delivery %d: %d", i, code)
		}
		// Past the ten-minute notification throttle: only the duplicate check holds.
		e.m.mu.Lock()
		clear(e.m.notified)
		e.m.mu.Unlock()
	}
	e.wait()
	if n := e.count(`SELECT count(*) FROM thread_messages WHERE thread_id = $1 AND source = 'email'`, id); n != 1 {
		t.Fatalf("stored %d", n)
	}
	notes := 0
	for _, m := range e.mailer.messages() {
		if m.To == ownerAddr && strings.Contains(m.Subject, "New reply") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("notifications: %d", notes)
	}
}

func TestInboundJunkRejectsCapped(t *testing.T) {
	e := newEnv(t, true, testDomain)
	for i := 0; i < junkRejectsPerMinute+5; i++ {
		unknown := fmt.Sprintf("%032x", i+1)
		if code := e.hook("junk", replyAddr(unknown)); code != 204 {
			t.Fatalf("junk %d: %d", i, code)
		}
	}
	if n := e.count(`SELECT count(*) FROM inbound_rejects`); n != junkRejectsPerMinute {
		t.Fatalf("rows: %d", n)
	}
	if e.m.junkSkipped.Load() != 5 {
		t.Fatalf("skipped: %d", e.m.junkSkipped.Load())
	}
	// Other rejects are not capped.
	_, tok := e.thread("priya@example.com")
	if code := e.hook(email("p@example.com", replyAddr(tok), "<a1@x>", "away", "Auto-Submitted: auto-replied"), replyAddr(tok)); code != 204 ||
		e.count(`SELECT count(*) FROM inbound_rejects WHERE reason = 'automatic'`) != 1 {
		t.Fatal("automatic reject")
	}
}

func TestInboundBusyAndRateLimited(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	raw := email("priya@example.com", replyAddr(tok), "<b1@x>", "hello")
	post := func() *httptest.ResponseRecorder {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req := httptest.NewRequest("POST", "/inbound-email", strings.NewReader(raw))
		req.Header.Set("X-Envelope-To", replyAddr(tok))
		req.Header.Set("X-Timestamp", ts)
		req.Header.Set("X-Signature", Sign(testSecret, ts, replyAddr(tok), []byte(raw)))
		rec := httptest.NewRecorder()
		e.h.ServeHTTP(rec, req)
		return rec
	}
	// Every read slot taken: 503 without reading.
	for i := 0; i < inboundReads; i++ {
		e.m.reads <- struct{}{}
	}
	if rec := post(); rec.Code != 503 || !strings.Contains(rec.Body.String(), "busy") {
		t.Fatalf("busy: %d %s", rec.Code, rec.Body)
	}
	for i := 0; i < inboundReads; i++ {
		<-e.m.reads
	}
	// Over the site-wide rate: 503 with Retry-After; a bad signature stays 401.
	e.m.inboundRate = newBucket(1, time.Hour)
	if rec := post(); rec.Code != 204 {
		t.Fatalf("first: %d", rec.Code)
	}
	if rec := post(); rec.Code != 503 || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	if code, _ := e.do("POST", "/inbound-email", raw, map[string]string{
		"X-Envelope-To": replyAddr(tok), "X-Timestamp": ts, "X-Signature": Sign("wrong", ts, replyAddr(tok), []byte(raw)),
	}); code != 401 {
		t.Fatalf("bad signature: %d", code)
	}
}

func TestBucket(t *testing.T) {
	b := newBucket(2, time.Minute)
	now := time.Unix(1_800_000_000, 0)
	if !b.take(now) || !b.take(now) || b.take(now) {
		t.Fatal("size")
	}
	if !b.take(now.Add(30*time.Second)) || b.take(now.Add(30*time.Second)) {
		t.Fatal("refill half a window")
	}
	if !b.take(now.Add(time.Hour)) || !b.take(now.Add(time.Hour)) || b.take(now.Add(time.Hour)) {
		t.Fatal("refill caps at size")
	}
}
