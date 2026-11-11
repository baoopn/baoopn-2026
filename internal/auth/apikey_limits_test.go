package auth

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
)

// doFrom is do with the request coming from addr.
func (h *keyHarness) doFrom(t *testing.T, addr, authz string) *httptest.ResponseRecorder {
	t.Helper()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	req.RemoteAddr = addr
	req.Header.Set("Authorization", authz)
	rec := httptest.NewRecorder()
	h.a.Middleware(next).ServeHTTP(rec, req)
	return rec
}

// refusedBy makes Clerk refuse every key with status.
func refusedBy(status int) func(string) (*clerk.APIKey, error) {
	return func(string) (*clerk.APIKey, error) {
		return nil, &clerk.APIErrorResponse{HTTPStatusCode: status}
	}
}

func TestAPIKeyShapeAcceptsRealKeys(t *testing.T) {
	for _, s := range []string{
		"ak_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX", // the Clerk SDK's example secret
		"ak_live_x_0123456789abcdef",
		"ak_" + strings.Repeat("a", 16),
		"ak_" + strings.Repeat("Z", 256),
		"ak_abc-DEF_123-456-789",
	} {
		if !apiKeyShape.MatchString(s) {
			t.Errorf("%q refused", s)
		}
	}
}

func TestAPIKeyBadShapeNeverReachesClerk(t *testing.T) {
	for name, secret := range map[string]string{
		"too short":    "ak_short",
		"15 chars":     "ak_" + strings.Repeat("a", 15),
		"too long":     "ak_" + strings.Repeat("a", 257),
		"space":        "ak_0123456789abcdef 01",
		"dot":          "ak_0123456789abcdef.01",
		"slash":        "ak_0123456789abcdef/01",
		"percent":      "ak_0123456789abcdef%00",
		"unicode":      "ak_0123456789abcdefé",
		"prefix only":  "ak_",
		"double colon": "ak_:0123456789abcdef",
	} {
		t.Run(name, func(t *testing.T) {
			h := newKeyHarness(t)
			rec, _ := h.do(t, "Bearer "+secret)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", rec.Code)
			}
			if code, msg := errBody(t, rec); code != "unauthorized" || msg != "invalid API key" {
				t.Fatalf("body = %s %s", code, msg)
			}
			if n := h.calls.Load(); n != 0 {
				t.Fatalf("verifyKey called %d times", n)
			}
			h.assertNoSecret(t, secret)
		})
	}
}

func TestAPIKeyRejectedIsNegativelyCached(t *testing.T) {
	const secret = "ak_live_gone_0123456789abcdef"
	h := newKeyHarness(t)
	h.key = refusedBy(http.StatusNotFound)
	for range 2 {
		if rec, _ := h.do(t, "Bearer "+secret); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if n := h.calls.Load(); n != 1 {
		t.Fatalf("verify calls within %v = %d, want 1", rejectTTL, n)
	}
	h.clock = h.clock.Add(61 * time.Second)
	if rec, _ := h.do(t, "Bearer "+secret); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status after TTL = %d", rec.Code)
	}
	if n := h.calls.Load(); n != 2 {
		t.Fatalf("verify calls after TTL = %d, want 2", n)
	}
	h.assertNoSecret(t, secret)
	h.a.mu.Lock()
	for k := range h.a.rejectedKeys {
		if strings.Contains(string(k[:]), secret) {
			t.Error("reject cache key holds the secret")
		}
	}
	h.a.mu.Unlock()
}

func TestAPIKeyUnusableNegativelyCached(t *testing.T) {
	const secret = "ak_live_revoked_0123456789abcdef"
	h := newKeyHarness(t)
	h.key = func(string) (*clerk.APIKey, error) {
		return &clerk.APIKey{ID: "ak_id", Subject: "user_1", Revoked: true}, nil
	}
	for range 3 {
		if rec, _ := h.do(t, "Bearer "+secret); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if n := h.calls.Load(); n != 1 {
		t.Fatalf("verify calls = %d, want 1", n)
	}
	if len(h.a.keyCache) != 0 {
		t.Fatal("unusable key was cached as valid")
	}
}

func TestAPIKeyFailureLimiterPerClient(t *testing.T) {
	const good = "ak_live_good_0123456789abcdef"
	h := newKeyHarness(t)
	valid := h.key
	h.key = func(s string) (*clerk.APIKey, error) {
		if s == good {
			return valid(s)
		}
		return nil, &clerk.APIErrorResponse{HTTPStatusCode: http.StatusNotFound}
	}
	// A's valid key is cached before A starts failing.
	if rec := h.doFrom(t, "10.0.0.1:1", "Bearer "+good); rec.Code != http.StatusOK {
		t.Fatalf("good key: %d", rec.Code)
	}
	for i := range keyFailuresPerMinute {
		if rec := h.doFrom(t, "10.0.0.1:1", fmt.Sprintf("Bearer ak_live_bad%02d_0123456789abcdef", i)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("bad %d: %d", i, rec.Code)
		}
	}
	before := h.calls.Load()
	rec := h.doFrom(t, "10.0.0.1:1", "Bearer ak_live_next_0123456789abcdef")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("over the limit: %d %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	if code, msg := errBody(t, rec); code != "rate_limited" || msg != "too many invalid API keys" {
		t.Fatalf("body = %s %s", code, msg)
	}
	if h.calls.Load() != before {
		t.Fatal("a blocked client reached Clerk")
	}
	// Bad shapes are refused with 429 too once blocked.
	if rec := h.doFrom(t, "10.0.0.1:1", "Bearer ak_x"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked bad shape: %d", rec.Code)
	}
	// Another client still reaches Clerk.
	if rec := h.doFrom(t, "10.0.0.2:1", "Bearer ak_live_other_0123456789abcdef"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("client B: %d", rec.Code)
	}
	if h.calls.Load() != before+1 {
		t.Fatal("client B did not reach Clerk")
	}
	// A's cached valid key is never limited.
	if rec := h.doFrom(t, "10.0.0.1:1", "Bearer "+good); rec.Code != http.StatusOK {
		t.Fatalf("cached good key while blocked: %d", rec.Code)
	}
	// The limit refills.
	h.clock = h.clock.Add(time.Minute)
	if rec := h.doFrom(t, "10.0.0.1:1", "Bearer ak_live_later_0123456789abcdef"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after a minute: %d", rec.Code)
	}
}

func TestAPIKeyClerkOutageNotCachedOrCounted(t *testing.T) {
	const secret = "ak_live_x_0123456789abcdef"
	for name, err := range map[string]error{
		"clerk 401": &clerk.APIErrorResponse{HTTPStatusCode: 401},
		"clerk 403": &clerk.APIErrorResponse{HTTPStatusCode: 403},
		"clerk 429": &clerk.APIErrorResponse{HTTPStatusCode: 429},
		"clerk 500": &clerk.APIErrorResponse{HTTPStatusCode: 500},
		"clerk 503": &clerk.APIErrorResponse{HTTPStatusCode: 503},
		"network":   errors.New("dial tcp: connection refused"),
	} {
		t.Run(name, func(t *testing.T) {
			h := newKeyHarness(t)
			h.key = func(string) (*clerk.APIKey, error) { return nil, err }
			n := keyFailuresPerMinute + 5
			for i := range n {
				if rec := h.doFrom(t, "10.0.0.1:1", "Bearer "+secret); rec.Code != http.StatusBadGateway {
					t.Fatalf("call %d: %d", i, rec.Code)
				}
			}
			if c := h.calls.Load(); int(c) != n {
				t.Fatalf("verify calls = %d, want %d (outage was cached or counted)", c, n)
			}
			if len(h.a.rejectedKeys) != 0 {
				t.Fatal("outage stored as a refusal")
			}
			// Clerk is back: the key works at once.
			h.key = func(string) (*clerk.APIKey, error) {
				return &clerk.APIKey{ID: "ak_id_1", Type: "api_key", Subject: "user_1"}, nil
			}
			if rec := h.doFrom(t, "10.0.0.1:1", "Bearer "+secret); rec.Code != http.StatusOK {
				t.Fatalf("after outage: %d", rec.Code)
			}
		})
	}
}

func TestAPIKeyRejectCacheBounded(t *testing.T) {
	h := newKeyHarness(t)
	for i := range rejectMax + 10 {
		h.a.storeRefused(fmt.Sprintf("ak_%d", i))
	}
	if n := len(h.a.rejectedKeys); n > rejectMax {
		t.Fatalf("reject cache size = %d, max %d", n, rejectMax)
	}

	// Full with expired entries: those go first, live ones stay.
	h = newKeyHarness(t)
	for i := range rejectMax - 1 {
		h.a.storeRefused(fmt.Sprintf("old_%d", i))
	}
	h.clock = h.clock.Add(rejectTTL)
	h.a.storeRefused("live")
	h.a.storeRefused("newer")
	if n := len(h.a.rejectedKeys); n != 2 {
		t.Fatalf("size after pruning = %d, want 2", n)
	}
	if !h.a.keyRefused("live") || !h.a.keyRefused("newer") {
		t.Fatal("live entry dropped")
	}

	// Full with only live entries: the table is cleared.
	h = newKeyHarness(t)
	for i := range rejectMax {
		h.a.storeRefused(fmt.Sprintf("k_%d", i))
	}
	h.a.storeRefused("last")
	if n := len(h.a.rejectedKeys); n != 1 || !h.a.keyRefused("last") {
		t.Fatalf("size after clear = %d", n)
	}
}

func TestKeyFailLimiterBounded(t *testing.T) {
	f := newKeyFailLimiter(keyFailuresPerMinute)
	now := time.Unix(1_800_000_000, 0)
	for i := range maxKeyFailClients + 10 {
		f.fail(fmt.Sprintf("10.%d.%d.1", i/256, i%256), now)
	}
	if n := len(f.clients); n > maxKeyFailClients {
		t.Fatalf("clients = %d, max %d", n, maxKeyFailClients)
	}
}
