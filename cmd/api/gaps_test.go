package main

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

// adminEndpoints is every authenticated admin route in docs/api.md.
var adminEndpoints = []struct{ method, path string }{
	{"GET", "/me"}, {"GET", "/overview"}, {"GET", "/profile"}, {"PUT", "/profile"},
	{"POST", "/profile/github"}, {"GET", "/projects"}, {"POST", "/projects"}, {"GET", "/projects/1"},
	{"PATCH", "/projects/1"}, {"DELETE", "/projects/1"}, {"POST", "/projects/sync"},
	{"POST", "/projects/reorder"}, {"GET", "/posts"}, {"POST", "/posts"}, {"GET", "/posts/1"},
	{"PUT", "/posts/1"}, {"DELETE", "/posts/1"}, {"POST", "/posts/preview"}, {"GET", "/media"},
	{"POST", "/media"}, {"GET", "/media/1"}, {"POST", "/media/1/retry"}, {"DELETE", "/media/1"}, {"GET", "/threads"}, {"GET", "/threads/unread-count"},
	{"GET", "/threads/1"}, {"POST", "/threads/1/read"}, {"POST", "/threads/1/unread"},
	{"PATCH", "/threads/1"}, {"DELETE", "/threads/1"}, {"POST", "/threads/1/messages"},
	{"POST", "/threads/1/messages/1/retry"}, {"GET", "/analytics"}, {"GET", "/settings"},
	{"PUT", "/settings"}, {"POST", "/settings/test-email"},
}

// Every admin route sits behind AdminAuth; only /config is open.
func TestAdminRoutesRequireAuth(t *testing.T) {
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			httpapi.Error(w, http.StatusUnauthorized, "unauthorized", "sign in")
		})
	}
	c := newHarness(t, func(d *httpapi.Deps) { d.AdminAuth = deny })
	for _, e := range adminEndpoints {
		c.fail(e.method, "/api/admin"+e.path, "{}", 401, "unauthorized")
	}
	c.ok("GET", "/api/admin/config", nil, 200, adminConfigShape, false)

	// With no auth configured the admin API is closed, not open.
	c = newHarness(t, func(d *httpapi.Deps) { d.AdminAuth = nil })
	for _, e := range adminEndpoints {
		c.fail(e.method, "/api/admin"+e.path, "{}", 503, "admin_disabled")
	}
}

// Concurrent sends of the same reply (a double click, a retry after a
// timeout) store one message and send one email.
func TestReplyIdempotentUnderConcurrency(t *testing.T) {
	c := newHarness(t)
	c.ok("POST", "/api/v1/contact", contactForm("Priya Shah", "priya@example.com",
		"Hello Bao, let's build something."), 201, okShape, false)
	c.wait()
	tid := fmt.Sprint(c.scalar(`SELECT id FROM threads`))
	before := c.mailer.count()

	const n = 8
	codes := make([]int, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			w := c.raw("POST", "/api/admin/threads/"+tid+"/messages",
				strings.NewReader(`{"body":"See you Monday.","request_id":"dbl-click-0001"}`), nil)
			codes[i] = w.Code
		})
	}
	wg.Wait()
	created := 0
	for _, code := range codes {
		switch code {
		case 201:
			created++
		case 200:
		default:
			t.Errorf("concurrent reply status %d", code)
		}
	}
	rows := c.scalar(`SELECT count(*) FROM thread_messages WHERE request_id = 'dbl-click-0001'`)
	if created != 1 || rows != int64(1) || c.mailer.count()-before != 1 {
		t.Errorf("created %d, rows %v, sends %d; want 1, 1, 1 (codes %v)", created, rows, c.mailer.count()-before, codes)
	}
	if mc := c.scalar(`SELECT message_count FROM threads WHERE id = $1`, tid); fmt.Sprint(mc) != "2" {
		t.Errorf("message_count = %v, want 2", mc)
	}
}

// With an internal token configured, a caller without it cannot pick its own
// X-Client-IP, so it cannot dodge the contact rate limit.
func TestContactRateLimitIgnoresUntrustedClientIP(t *testing.T) {
	c := newHarness(t, func(d *httpapi.Deps) { d.ClientIP = httpapi.TrustedClientIP("internal-token") })
	for i := range 6 {
		c.ip = fmt.Sprintf("192.0.2.%d", i+1) // forged, no token
		w := c.raw("POST", "/api/v1/contact", jsonBody(contactForm("Spammer", fmt.Sprintf("s%d@example.com", i),
			"Rotating my claimed IP every time.")), nil)
		if i < 5 && w.Code != 201 {
			t.Fatalf("submission %d = %d", i, w.Code)
		}
		if i == 5 {
			c.failRaw(w, "6th forged-IP submission", 429, "rate_limited")
		}
	}
	// Through the web proxy (token present) the forwarded IP is used.
	w := c.raw("POST", "/api/v1/contact", jsonBody(contactForm("Real", "r@example.com", "Proxied visitor here.")),
		map[string]string{"X-Internal-Token": "internal-token", "X-Client-IP": "198.51.100.200"})
	if w.Code != 201 {
		t.Errorf("proxied visitor = %d %s", w.Code, w.Body)
	}
}

// A signed hook for an unknown reply token is accepted (204, so the worker
// does not retry) but stores nothing in any thread.
func TestInboundUnknownTokenStoresNothing(t *testing.T) {
	c := newHarness(t)
	c.ok("POST", "/api/v1/contact", contactForm("Priya Shah", "priya@example.com",
		"Hello Bao, let's build something."), 201, okShape, false)
	c.wait()
	bogus := strings.Repeat("ab", 16)
	if w := c.hook(inboundEML(bogus, "<x-1@mail.example>", "<none@mail.example>"), "reply+"+bogus+"@"+ctInboundDomain,
		time.Now(), ctInboundSecret); w.Code != 204 {
		t.Fatalf("hook = %d %s", w.Code, w.Body)
	}
	if n := c.scalar(`SELECT count(*) FROM thread_messages WHERE source = 'email'`); n != int64(0) {
		t.Errorf("email messages = %v", n)
	}
	if n := c.scalar(`SELECT count(*) FROM inbound_rejects`); n != int64(1) {
		t.Errorf("inbound_rejects = %v, want 1", n)
	}
}

// Oversized uploads and invalid admin input map to the shared error codes.
func TestAdminLimitsAndValidation(t *testing.T) {
	c := newHarness(t)
	big := append(pngBytes(), make([]byte, 3<<20/2)...)
	c.failRaw(c.upload("big.png", big), "upload 1.5 MiB (limit 1 MiB)", 413, "too_large")
	c.fail("PUT", "/api/admin/settings", map[string]any{"notify_email": "nope", "contact_enabled": true,
		"auto_reply_enabled": false, "auto_reply_text": "", "signature": ""}, 422, "validation")
	prof := c.ok("GET", "/api/admin/profile", nil, 200, profileShape, false)
	if m, ok := prof.(map[string]any); ok {
		m["avatar_url"] = "javascript:alert(1)"
		c.fail("PUT", "/api/admin/profile", m, 422, "validation")
	}
	c.fail("POST", "/api/admin/posts", map[string]any{"title": "x", "cover_url": "//evil.example/x.png", "status": "draft",
		"parts": []map[string]string{{"title": "", "content_md": ""}}},
		422, "validation")
	c.fail("POST", "/api/admin/threads/1/messages", `{"body":"hi","request_id":"short"}`, 422, "validation")
}
