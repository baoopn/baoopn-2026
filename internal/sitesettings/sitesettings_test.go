package sitesettings

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

type fakeMailer struct {
	enabled bool
	err     error
	mu      sync.Mutex
	sent    []bmail.Message
}

func (f *fakeMailer) Enabled() bool       { return f.enabled }
func (f *fakeMailer) FromName() string    { return "Bao Nguyen" }
func (f *fakeMailer) FromAddress() string { return "bao@mail.baoopn.com" }
func (f *fakeMailer) Send(_ context.Context, m bmail.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	if f.err != nil {
		return "", f.err
	}
	return "<id@mail.baoopn.com>", nil
}

func do(t *testing.T, h http.Handler, method, path, body string) (int, map[string]json.RawMessage) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]json.RawMessage
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func router(s *Store) http.Handler {
	r := chi.NewRouter()
	s.Admin(r)
	return r
}

func TestNormalise(t *testing.T) {
	for name, tc := range map[string]struct {
		in   Settings
		ok   bool
		want string
	}{
		"empty notify":   {Settings{}, true, ""},
		"valid notify":   {Settings{NotifyEmail: " Me <me@baoopn.com> "}, true, "me@baoopn.com"},
		"bad notify":     {Settings{NotifyEmail: "nope"}, false, ""},
		"long notify":    {Settings{NotifyEmail: strings.Repeat("a", 250) + "@b.com"}, false, ""},
		"long auto text": {Settings{AutoReplyText: strings.Repeat("a", 2001)}, false, ""},
		"long signature": {Settings{Signature: strings.Repeat("a", 501)}, false, ""},
	} {
		got, problem := normalise(tc.in)
		if (problem == "") != tc.ok || (tc.ok && got.NotifyEmail != tc.want) {
			t.Errorf("%s: %+v, %q", name, got, problem)
		}
	}
}

func TestSettingsRoundTripAndNotifyTo(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	s := New(Deps{DB: db, DefaultNotify: "owner@example.com", Mailer: &fakeMailer{}})
	h := router(s)

	code, out := do(t, h, "GET", "/settings", "")
	var got Settings
	_ = json.Unmarshal(out["data"], &got)
	if code != 200 || got != Defaults() {
		t.Fatalf("defaults: %d %+v", code, got)
	}
	if s.NotifyTo(ctx) != "owner@example.com" {
		t.Fatal("empty notify_email falls back to NOTIFY_EMAIL")
	}
	code, _ = do(t, h, "PUT", "/settings", `{"notify_email":"bad","contact_enabled":true,"auto_reply_enabled":false,"auto_reply_text":"","signature":""}`)
	if code != 422 {
		t.Fatalf("invalid: %d", code)
	}
	code, _ = do(t, h, "PUT", "/settings", `{"notify_email":"me@baoopn.com","contact_enabled":false,"auto_reply_enabled":true,"auto_reply_text":"Hi {name}","signature":"B"}`)
	if code != 200 || s.NotifyTo(ctx) != "me@baoopn.com" {
		t.Fatalf("put: %d", code)
	}
	// A fresh store reads what was saved.
	v, err := New(Deps{DB: db}).Get(ctx)
	if err != nil || v.ContactEnabled || !v.AutoReplyEnabled || v.AutoReplyText != "Hi {name}" || v.Signature != "B" {
		t.Fatalf("stored: %+v %v", v, err)
	}
}

func TestTestEmail(t *testing.T) {
	db := testdb.New(t)
	m := &fakeMailer{}
	s := New(Deps{DB: db, DefaultNotify: "owner@example.com", Mailer: m})
	h := router(s)

	if code, out := do(t, h, "POST", "/settings/test-email", ""); code != 503 || !strings.Contains(string(out["error"]), "mail_disabled") {
		t.Fatalf("disabled: %d %s", code, out["error"])
	}
	if len(m.sent) != 0 {
		t.Fatal("disabled must not send")
	}
	m.enabled = true
	code, out := do(t, h, "POST", "/settings/test-email", "")
	if code != 200 || !strings.Contains(string(out["data"]), "owner@example.com") {
		t.Fatalf("sent: %d %s", code, out["data"])
	}
	if len(m.sent) != 1 || m.sent[0].AutoSubmitted != "auto-generated" || m.sent[0].To != "owner@example.com" {
		t.Fatalf("message: %+v", m.sent)
	}
	m.err = errors.New("smtp 451")
	if code, out := do(t, h, "POST", "/settings/test-email", ""); code != 502 || !strings.Contains(string(out["error"]), "mail_failed") {
		t.Fatalf("failed: %d %s", code, out["error"])
	}
}
