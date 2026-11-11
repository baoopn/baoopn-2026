package contact

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// signedReq is a request to the hook from addr, signed now over body unless
// sig is set.
func signedReq(envTo, body, addr, sig string) *http.Request {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest("POST", "/inbound-email", strings.NewReader(body))
	req.RemoteAddr = addr
	req.Header.Set("X-Envelope-To", envTo)
	req.Header.Set("X-Timestamp", ts)
	if sig == "" {
		sig = Sign(testSecret, ts, envTo, []byte(body))
	}
	req.Header.Set("X-Signature", sig)
	return req
}

func (e *env) serve(req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// TestInboundCheapChecksTakeNoSlot: with every read slot held, requests that
// are too large or carry no plausible signature are still answered by their
// own status, not 503: they never wait for a slot.
func TestInboundCheapChecksTakeNoSlot(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	for range inboundReads {
		e.m.reads <- struct{}{}
	}
	big := signedReq(replyAddr(tok), "small", "10.0.0.1:1", "")
	big.ContentLength = maxInboundBytes + 1
	if rec := e.serve(big); rec.Code != 413 {
		t.Fatalf("announced too large: %d", rec.Code)
	}
	old := strconv.FormatInt(time.Now().Add(-10*time.Minute).Unix(), 10)
	for name, mod := range map[string]func(*http.Request){
		"stale":        func(r *http.Request) { r.Header.Set("X-Timestamp", old) },
		"no time":      func(r *http.Request) { r.Header.Del("X-Timestamp") },
		"not hex":      func(r *http.Request) { r.Header.Set("X-Signature", strings.Repeat("z", 64)) },
		"short":        func(r *http.Request) { r.Header.Set("X-Signature", "abcd") },
		"no signature": func(r *http.Request) { r.Header.Del("X-Signature") },
	} {
		req := signedReq(replyAddr(tok), "hi", "10.0.0.2:1", "")
		mod(req)
		if rec := e.serve(req); rec.Code != 401 {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if rec := e.serve(signedReq(replyAddr(tok), "hi", "10.0.0.3:1", "")); rec.Code != 503 {
		t.Fatalf("a plausible request waits for a slot: %d", rec.Code)
	}
}

// TestInboundSlowBodyTimesOut: a client that stops sending its body gets 408
// after the read timeout, and its slot is free again.
func TestInboundSlowBodyTimesOut(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	e.m.readTimeout = 300 * time.Millisecond
	srv := httptest.NewServer(e.h)
	t.Cleanup(srv.Close)
	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	_, err = conn.Write([]byte("POST /inbound-email HTTP/1.1\r\nHost: x\r\nContent-Length: 1000\r\n" +
		"X-Envelope-To: " + replyAddr(tok) + "\r\nX-Timestamp: " + ts + "\r\n" +
		"X-Signature: " + Sign(testSecret, ts, replyAddr(tok), nil) + "\r\n\r\nonly a little"))
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v", d)
	}
	if n := len(e.m.reads); n != 0 {
		t.Fatalf("%d slots still held", n)
	}
}

// TestInboundBadSignaturesLimitedPerClient: one client's failed checks are
// answered 429 once over the limit, for that client only; signed calls never
// count toward it and are never limited by it.
func TestInboundBadSignaturesLimitedPerClient(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	raw := email("priya@example.com", replyAddr(tok), "<g1@x>", "hello")
	// Signed calls from the worker's address are never limited.
	for i := range badSignaturesPerMinute + 5 {
		body := email("priya@example.com", replyAddr(tok), "<w"+strconv.Itoa(i)+"@x>", "hello")
		if rec := e.serve(signedReq(replyAddr(tok), body, "10.9.9.9:1", "")); rec.Code != 204 {
			t.Fatalf("signed %d: %d", i, rec.Code)
		}
	}
	bad := Sign("wrong", "1", "x", nil)
	for i := range badSignaturesPerMinute {
		if rec := e.serve(signedReq(replyAddr(tok), raw, "10.6.6.6:1", bad)); rec.Code != 401 {
			t.Fatalf("bad %d: %d", i, rec.Code)
		}
	}
	if rec := e.serve(signedReq(replyAddr(tok), raw, "10.6.6.6:1", bad)); rec.Code != 429 {
		t.Fatalf("over the limit: %d", rec.Code)
	}
	// Another client's failures are its own.
	if rec := e.serve(signedReq(replyAddr(tok), raw, "10.7.7.7:1", bad)); rec.Code != 401 {
		t.Fatalf("other client, bad: %d", rec.Code)
	}
	if rec := e.serve(signedReq(replyAddr(tok), raw, "10.9.9.9:1", "")); rec.Code != 204 {
		t.Fatalf("other client: %d", rec.Code)
	}
}

// TestInboundSignedCallNotBlockedAfterJunk: junk from the worker's own
// address cannot lock out real deliveries. After 40 bad posts a signed post
// still gets 204 and is stored.
func TestInboundSignedCallNotBlockedAfterJunk(t *testing.T) {
	e := newEnv(t, true, testDomain)
	id, tok := e.thread("priya@example.com")
	bad := Sign("wrong", "1", "x", nil)
	junk := email("x@example.com", replyAddr(tok), "<j@x>", "junk")
	for range 40 {
		e.serve(signedReq(replyAddr(tok), junk, "10.5.5.5:1", bad))
	}
	raw := email("priya@example.com", replyAddr(tok), "<real-1@x>", "still here")
	if rec := e.serve(signedReq(replyAddr(tok), raw, "10.5.5.5:1", "")); rec.Code != 204 {
		t.Fatalf("signed after junk: %d %s", rec.Code, rec.Body)
	}
	if n := e.count(`SELECT count(*) FROM thread_messages WHERE thread_id = $1 AND rfc_message_id = '<real-1@x>'`, id); n != 1 {
		t.Fatalf("stored %d", n)
	}
}

// TestInboundBlockedClientFailureGets429: once a client is over the limit,
// each further failure, whether at the header check or the body check, gets
// 429 rate_limited.
func TestInboundBlockedClientFailureGets429(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	raw := email("priya@example.com", replyAddr(tok), "<k1@x>", "hello")
	bad := Sign("wrong", "1", "x", nil)
	for range badSignaturesPerMinute {
		e.serve(signedReq(replyAddr(tok), raw, "10.4.4.4:1", bad))
	}
	if rec := e.serve(signedReq(replyAddr(tok), raw, "10.4.4.4:1", bad)); rec.Code != 429 ||
		!strings.Contains(rec.Body.String(), "rate_limited") {
		t.Fatalf("bad body signature: %d %s", rec.Code, rec.Body)
	}
	noSig := signedReq(replyAddr(tok), raw, "10.4.4.4:1", "")
	noSig.Header.Del("X-Signature")
	if rec := e.serve(noSig); rec.Code != 429 || !strings.Contains(rec.Body.String(), "rate_limited") {
		t.Fatalf("no signature header: %d %s", rec.Code, rec.Body)
	}
}

// failingBody fails every read with an error readInbound cannot classify.
type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// TestInboundBodyReadErrorIs503: an unclassified body read error is
// retryable: 503 read_failed, never 400, which the worker would drop.
func TestInboundBodyReadErrorIs503(t *testing.T) {
	e := newEnv(t, true, testDomain)
	_, tok := e.thread("priya@example.com")
	req := signedReq(replyAddr(tok), "", "10.3.3.3:1", "")
	req.Body = io.NopCloser(failingBody{})
	req.ContentLength = -1
	rec := e.serve(req)
	if rec.Code != 503 || !strings.Contains(rec.Body.String(), "read_failed") {
		t.Fatalf("read error: %d %s", rec.Code, rec.Body)
	}
	if n := len(e.m.reads); n != 0 {
		t.Fatalf("%d slots still held", n)
	}
}
