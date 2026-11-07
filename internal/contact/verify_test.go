package contact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The shared signature vector, pinned here and in
// workers/inbound-email/test/sign.test.js.
const (
	vectorSecret = "test-secret"
	vectorTS     = "1700000000"
	vectorBody   = "hello"
	vectorTo     = "reply+abc@reply.example.com"
	vectorSig    = "16e360ecccd3d3712a57caff006a9e5306bd55e25b11570e07a6d2a2ca87a313"
)

func TestSignVector(t *testing.T) {
	if got := Sign(vectorSecret, vectorTS, vectorTo, []byte(vectorBody)); got != vectorSig {
		t.Fatalf("Sign = %s", got)
	}
}

func TestVerify(t *testing.T) {
	at := time.Unix(1700000000, 0)
	body := []byte(vectorBody)
	for name, tc := range map[string]struct {
		secret, ts, sig string
		now             time.Time
		want            bool
	}{
		"good":                   {vectorSecret, vectorTS, vectorSig, at, true},
		"good, upper-case hex":   {vectorSecret, vectorTS, strings.ToUpper(vectorSig), at, true},
		"good, 299 s later":      {vectorSecret, vectorTS, vectorSig, at.Add(299 * time.Second), true},
		"stale":                  {vectorSecret, vectorTS, vectorSig, at.Add(301 * time.Second), false},
		"future":                 {vectorSecret, vectorTS, vectorSig, at.Add(-301 * time.Second), false},
		"wrong secret":           {"other", vectorTS, vectorSig, at, false},
		"no secret":              {"", vectorTS, vectorSig, at, false},
		"bad signature":          {vectorSecret, vectorTS, strings.Repeat("0", 64), at, false},
		"short signature":        {vectorSecret, vectorTS, vectorSig[:62], at, false},
		"not hex":                {vectorSecret, vectorTS, "zz" + vectorSig[2:], at, false},
		"empty signature":        {vectorSecret, vectorTS, "", at, false},
		"timestamp not a number": {vectorSecret, "17e8", vectorSig, at, false},
		"other timestamp":        {vectorSecret, "1700000001", vectorSig, at, false},
	} {
		if got := verify(tc.secret, tc.ts, vectorTo, tc.sig, body, tc.now); got != tc.want {
			t.Errorf("%s: verify = %v", name, got)
		}
	}
}

func TestTokenExtraction(t *testing.T) {
	const tok = "0123456789abcdef0123456789abcdef"
	const domain = "reply.baoopn.com"
	for name, tc := range map[string]struct {
		env  string
		want string
	}{
		"envelope":             {"reply+" + tok + "@reply.baoopn.com", tok},
		"envelope upper case":  {"REPLY+" + strings.ToUpper(tok) + "@Reply.Baoopn.com", tok},
		"envelope in brackets": {"<reply+" + tok + "@reply.baoopn.com>", tok},
		"wrong domain":         {"reply+" + tok + "@evil.example", ""},
		"subdomain of ours":    {"reply+" + tok + "@x.reply.baoopn.com", ""},
		"short token":          {"reply+0123@reply.baoopn.com", ""},
		"not hex":              {"reply+" + strings.Repeat("g", 32) + "@reply.baoopn.com", ""},
		"not a reply address":  {"someone@reply.baoopn.com", ""},
		"nothing":              {"", ""},
	} {
		if got := tokenOf(tc.env, domain); got != tc.want {
			t.Errorf("%s: token %q", name, got)
		}
	}
	if got := redact("Reply+" + tok + "@reply.baoopn.com"); got != "reply+…@reply.baoopn.com" {
		t.Fatalf("redact = %q", got)
	}
	if got := redact("someone@example.com"); got != "someone@example.com" {
		t.Fatalf("redact = %q", got)
	}
}

func TestValidate(t *testing.T) {
	now := time.UnixMilli(1_800_000_000_000)
	ok := ContactInput{Name: "  Priya Shah ", Email: "Priya@Example.com", Message: "  Hello there, Bao!  ", StartedAt: now.UnixMilli() - 10_000}
	s, problem := validate(ok, now)
	if problem != "" || s.name != "Priya Shah" || s.email != "priya@example.com" || s.subject != "Message from Priya Shah" || s.message != "Hello there, Bao!" {
		t.Fatalf("valid: %+v %q", s, problem)
	}
	with := func(f func(*ContactInput)) ContactInput { c := ok; f(&c); return c }
	for name, tc := range map[string]struct {
		in   ContactInput
		want string
	}{
		"too fast":           {with(func(c *ContactInput) { c.StartedAt = now.UnixMilli() - 1000 }), "wait a moment"},
		"too old":            {with(func(c *ContactInput) { c.StartedAt = now.Add(-25 * time.Hour).UnixMilli() }), "expired"},
		"future":             {with(func(c *ContactInput) { c.StartedAt = now.Add(2 * time.Minute).UnixMilli() }), "expired"},
		"missing started_at": {with(func(c *ContactInput) { c.StartedAt = 0 }), "expired"},
		"no name":            {with(func(c *ContactInput) { c.Name = "   " }), "name"},
		"long name":          {with(func(c *ContactInput) { c.Name = strings.Repeat("a", 101) }), "name"},
		"name of brackets":   {with(func(c *ContactInput) { c.Name = "<>" }), "name"},
		"bad email":          {with(func(c *ContactInput) { c.Email = "nope" }), "email"},
		"dotless domain":     {with(func(c *ContactInput) { c.Email = "a@localhost" }), "email"},
		"email injection":    {with(func(c *ContactInput) { c.Email = "a@b.com\r\nBcc: c@d.com" }), "email"},
		"long subject":       {with(func(c *ContactInput) { c.Subject = strings.Repeat("s", 151) }), "subject"},
		"short message":      {with(func(c *ContactInput) { c.Message = " short  " }), "message"},
		"long message":       {with(func(c *ContactInput) { c.Message = strings.Repeat("m", 5001) }), "message"},
	} {
		if _, problem := validate(tc.in, now); !strings.Contains(problem, tc.want) {
			t.Errorf("%s: problem %q, want %q", name, problem, tc.want)
		}
	}
	// Header injection through name and subject ends up on one line.
	s, problem = validate(with(func(c *ContactInput) {
		c.Name = "Priya\r\nBcc: x@evil.example"
		c.Subject = "Hi\r\nBcc: x@evil.example"
	}), now)
	if problem != "" || strings.ContainsAny(s.name+s.subject, "\r\n") {
		t.Fatalf("injection: %+v %q", s, problem)
	}
}

func TestTurnstile(t *testing.T) {
	var gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm.Encode()
		if r.PostForm.Get("response") == "good" {
			_, _ = w.Write([]byte(`{"success":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	defer srv.Close()
	m := New(Deps{TurnstileSecret: "ts-secret", HTTP: srv.Client()})
	m.turnstileURL = srv.URL
	ctx := context.Background()
	if ok, err := m.verifyTurnstile(ctx, "good", "203.0.113.5"); !ok || err != nil {
		t.Fatalf("good: %v %v", ok, err)
	}
	if !strings.Contains(gotForm, "remoteip=203.0.113.5") || !strings.Contains(gotForm, "secret=ts-secret") {
		t.Fatalf("form: %s", gotForm)
	}
	if ok, err := m.verifyTurnstile(ctx, "bad", ""); ok || err != nil {
		t.Fatalf("bad: %v %v", ok, err)
	}
	srv.Close()
	if _, err := m.verifyTurnstile(ctx, "good", ""); err == nil {
		t.Fatal("a transport error is an error")
	}
}

func TestVerifyCoversEnvelope(t *testing.T) {
	at := time.Unix(1700000000, 0)
	if verify(vectorSecret, vectorTS, "reply+other@reply.example.com", vectorSig, []byte(vectorBody), at) {
		t.Fatal("a signature must not verify for a different envelope recipient")
	}
}
