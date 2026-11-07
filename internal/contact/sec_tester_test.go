package contact

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
)

// countParses swaps the module's MIME parser for one that counts calls.
func countParses(m *Module) *int {
	n := 0
	m.parse = func(raw []byte) (bmail.Inbound, error) {
		n++
		return bmail.ParseInbound(raw)
	}
	return &n
}

// Mail that cannot be stored never reaches the MIME parser: a bad signature,
// a spam thread and a flooded thread, as well as the junk cases in
// TestInboundTokenOnlyFromEnvelopeAndParsedLast.
func TestInboundRejectedMailIsNeverParsed(t *testing.T) {
	e := newEnv(t, true, testDomain)
	parsed := countParses(e.m)
	_, tok := e.thread("priya@example.com")
	garbage := "\x00\xff\xfe MIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\r\n--\r\n"

	// Bad signature, valid-looking headers.
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	code, _ := e.do("POST", "/inbound-email", garbage, map[string]string{
		"Content-Type": "message/rfc822", "X-Envelope-To": replyAddr(tok),
		"X-Timestamp": ts, "X-Signature": strings.Repeat("ab", 32),
	})
	if code != 401 || *parsed != 0 {
		t.Fatalf("bad signature: %d, parsed %d", code, *parsed)
	}
	// Signed for one recipient, delivered as another: the signature fails.
	code, _ = e.do("POST", "/inbound-email", garbage, map[string]string{
		"Content-Type": "message/rfc822", "X-Envelope-To": replyAddr(tok),
		"X-Timestamp": ts, "X-Signature": Sign(testSecret, ts, replyAddr(strings.Repeat("cd", 16)), []byte(garbage)),
	})
	if code != 401 || *parsed != 0 {
		t.Fatalf("recipient swap: %d, parsed %d", code, *parsed)
	}

	// Spam thread.
	spamID, spamTok := e.thread("spammer@example.com")
	if _, err := e.db.Exec(t.Context(), `UPDATE threads SET status = 'spam' WHERE id = $1`, spamID); err != nil {
		t.Fatal(err)
	}
	if code := e.hook(garbage, replyAddr(spamTok)); code != 204 || *parsed != 0 {
		t.Fatalf("spam thread: %d, parsed %d", code, *parsed)
	}

	// Flooded thread: floodLimit stored replies, then the next is not parsed.
	for i := range floodLimit {
		raw := email("priya@example.com", replyAddr(tok), fmt.Sprintf("<flood%d@x>", i), "reply number "+strconv.Itoa(i))
		if code := e.hook(raw, replyAddr(tok)); code != 204 {
			t.Fatalf("reply %d: %d", i, code)
		}
	}
	*parsed = 0
	if code := e.hook(garbage, replyAddr(tok)); code != 204 || *parsed != 0 {
		t.Fatalf("flooded thread: %d, parsed %d", code, *parsed)
	}
	if e.count(`SELECT count(*) FROM inbound_rejects WHERE reason = 'spam_thread'`) != 1 ||
		e.count(`SELECT count(*) FROM inbound_rejects WHERE reason = 'flood'`) != 1 {
		t.Fatal("reject rows")
	}
}

// The acknowledgement never carries the visitor's subject, in any field,
// whatever the subject holds.
func TestAckNeverCarriesTheSubject(t *testing.T) {
	e := newEnv(t, true, testDomain)
	e.withTurnstile()
	s := sitesettings.Defaults()
	s.AutoReplyEnabled = true
	putSettings(t, e, s)
	subjects := []string{
		"QZXmarker1 win a prize at http://evil.example",
		"QZXmarker2\r\nBcc: victim@example.com",
		"=?UTF-8?B?UVpYbWFya2VyMw==?= encoded",
		"QZXmarker4 " + strings.Repeat("long ", 27), // near the 150-character limit
	}
	for i, subj := range subjects {
		addr := fmt.Sprintf("visitor%d@example.com", i)
		if code, raw := e.form("Sam", addr, subj, "a long enough message for the form"); code != 201 {
			t.Fatalf("form %d: %d %s", i, code, raw)
		}
		e.wait()
		acks := 0
		for _, m := range e.mailer.messages() {
			if m.To != addr {
				continue
			}
			acks++
			if m.Subject != "Re: your message to Bao Nguyen" {
				t.Errorf("ack subject %q", m.Subject)
			}
			all := fmt.Sprintf("%+v", m)
			for _, needle := range []string{"QZXmarker", "UVpYbWFya2Vy", "evil.example", "victim@", "Bcc:"} {
				if strings.Contains(all, needle) {
					t.Errorf("subject %d: the ack contains %q", i, needle)
				}
			}
		}
		if acks != 1 {
			t.Fatalf("subject %d: %d acks", i, acks)
		}
	}
}

// Both caps live in contact_acks, so a restarted API (a new Module on the
// same database) keeps them. Gmail spellings of one mailbox share a cap.
func TestAckCapsSurviveRestart(t *testing.T) {
	e := newEnv(t, true, testDomain)
	ctx := context.Background()
	tid, _ := e.thread("visitor@example.com")

	for _, addr := range []string{"joe@gmail.com", "j.o.e+x@googlemail.com", "J.O.E@Gmail.com"} {
		if !e.m.reserveAck(ctx, tid, addr) {
			t.Fatalf("%s refused under the cap", addr)
		}
	}
	restarted := New(e.m.d)
	for _, addr := range []string{"jo.e@gmail.com", "JOE+y@googlemail.com"} {
		if restarted.reserveAck(ctx, tid, addr) {
			t.Fatalf("after a restart %s passed the per-address cap", addr)
		}
	}
	if !restarted.reserveAck(ctx, tid, "joe@example.com") {
		t.Fatal("a non-Gmail joe is a different mailbox")
	}

	for i := e.globalAcks(); i < ackPerHour; i++ {
		if !e.m.reserveAck(ctx, tid, "fill"+strconv.Itoa(i)+"@example.com") {
			t.Fatalf("fill %d refused", i)
		}
	}
	again := New(e.m.d)
	if again.reserveAck(ctx, tid, "fresh@example.com") {
		t.Fatal("after a restart the hourly cap was reset")
	}
	if e.globalAcks() != ackPerHour {
		t.Fatalf("site-wide acks %d", e.globalAcks())
	}
}

// Concurrent reservations from two API instances cannot pass a cap together.
func TestAckCapHoldsAcrossInstances(t *testing.T) {
	e := newEnv(t, true, testDomain)
	ctx := context.Background()
	tid, _ := e.thread("visitor@example.com")
	a, b := e.m, New(e.m.d)
	results := make(chan bool, 20)
	for i := range 20 {
		m := a
		if i%2 == 1 {
			m = b
		}
		go func() { results <- m.reserveAck(ctx, tid, "same.person@gmail.com") }()
	}
	ok := 0
	for range 20 {
		if <-results {
			ok++
		}
	}
	if ok != ackPerAddress {
		t.Fatalf("%d acks passed, want %d", ok, ackPerAddress)
	}
}
