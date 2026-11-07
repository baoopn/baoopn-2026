package contact

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

const (
	testDomain = "reply.baoopn.com"
	testSecret = "hook-secret"
	ownerAddr  = "owner@example.com"
)

type fakeMailer struct {
	enabled bool
	mu      sync.Mutex
	fail    error
	sent    []bmail.Message
	n       int
}

func (f *fakeMailer) Enabled() bool       { return f.enabled }
func (f *fakeMailer) FromName() string    { return "Bao Nguyen" }
func (f *fakeMailer) FromAddress() string { return "bao@mail.baoopn.com" }
func (f *fakeMailer) Send(_ context.Context, m bmail.Message) (string, error) {
	if !f.enabled {
		return "", bmail.ErrDisabled
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	if f.fail != nil {
		return "", f.fail
	}
	f.n++
	return fmt.Sprintf("<out-%d-%d@mail.baoopn.com>", f.n, rand.Int64()), nil
}

func (f *fakeMailer) messages() []bmail.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bmail.Message(nil), f.sent...)
}

type env struct {
	t      *testing.T
	db     *pgxpool.Pool
	mailer *fakeMailer
	site   *sitesettings.Store
	m      *Module
	h      http.Handler

	noTurnstileToken bool // form() omits the captcha token
}

func newEnv(t *testing.T, enabled bool, domain string) *env {
	t.Helper()
	db := testdb.New(t)
	mailer := &fakeMailer{enabled: enabled}
	site := sitesettings.New(sitesettings.Deps{DB: db, DefaultNotify: ownerAddr, Mailer: mailer})
	m := New(Deps{DB: db, Mailer: mailer, Settings: site, PublicURL: "https://baoopn.com",
		InboundDomain: domain, InboundSecret: testSecret})
	r := chi.NewRouter()
	m.Public(r)
	m.Admin(r)
	m.Hooks(r)
	site.Admin(r)
	return &env{t: t, db: db, mailer: mailer, site: site, m: m, h: r}
}

// do sends a request from a random client IP (the form is rate limited).
func (e *env) do(method, path, body string, hdr map[string]string) (int, json.RawMessage) {
	e.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = fmt.Sprintf("10.%d.%d.%d:1234", rand.IntN(250), rand.IntN(250), rand.IntN(250))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var out struct {
		Data  json.RawMessage `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Error != nil {
		return rec.Code, out.Error
	}
	return rec.Code, out.Data
}

// wait lets background sends finish.
func (e *env) wait() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e.m.Shutdown(ctx)
}

func (e *env) form(name, email, subject, message string) (int, json.RawMessage) {
	body, _ := json.Marshal(ContactInput{Name: name, Email: email, Subject: subject, Message: message,
		StartedAt: time.Now().Add(-10 * time.Second).UnixMilli(), TurnstileToken: "test-token"})
	if e.noTurnstileToken {
		body, _ = json.Marshal(ContactInput{Name: name, Email: email, Subject: subject, Message: message,
			StartedAt: time.Now().Add(-10 * time.Second).UnixMilli()})
	}
	return e.do("POST", "/contact", string(body), nil)
}

// thread makes a thread straight in the database and returns its id and token.
func (e *env) thread(email string) (int64, string) {
	e.t.Helper()
	tok := newToken()
	id, err := e.m.storeSubmission(context.Background(), submission{name: "Priya Shah", email: email, subject: "Hello", message: "Hello there, Bao!"}, tok)
	if err != nil {
		e.t.Fatal(err)
	}
	return id, tok
}

func (e *env) detail(id int64) ThreadDetail {
	e.t.Helper()
	code, raw := e.do("GET", "/threads/"+strconv.FormatInt(id, 10), "", nil)
	if code != 200 {
		e.t.Fatalf("detail: %d %s", code, raw)
	}
	var d ThreadDetail
	if err := json.Unmarshal(raw, &d); err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

// email is a raw message to a reply address.
func email(from, to, msgID, body string, extra ...string) string {
	h := "From: " + from + "\r\nTo: " + to + "\r\nSubject: Re: Hello\r\n"
	if msgID != "" {
		h += "Message-ID: " + msgID + "\r\n"
	}
	for _, x := range extra {
		h += x + "\r\n"
	}
	return h + "MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
}

// hook posts raw to the inbound hook, signed now.
func (e *env) hook(raw, envTo string) int {
	e.t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	code, _ := e.do("POST", "/inbound-email", raw, map[string]string{
		"Content-Type": "message/rfc822", "X-Envelope-To": envTo, "X-Envelope-From": "bounce@example.com",
		"X-Timestamp": ts, "X-Signature": Sign(testSecret, ts, envTo, []byte(raw)),
	})
	return code
}

func replyAddr(tok string) string { return "reply+" + tok + "@" + testDomain }

// withTurnstile enables the captcha against a fake that accepts every token
// (visitor acknowledgements are only sent when Turnstile is configured).
func (e *env) withTurnstile() {
	e.t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	e.t.Cleanup(srv.Close)
	e.m.d.TurnstileSecret = "test-secret"
	e.m.turnstileURL = srv.URL
}
