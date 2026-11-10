package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
)

type keyHarness struct {
	a     *Authenticator
	logs  *bytes.Buffer
	calls atomic.Int32
	clock time.Time
	key   func(secret string) (*clerk.APIKey, error)
}

func newKeyHarness(t *testing.T) *keyHarness {
	t.Helper()
	h := &keyHarness{logs: &bytes.Buffer{}, clock: time.Unix(1_800_000_000, 0)}
	h.a = New(Config{SecretKey: "sk_test_x", AllowedUserIDs: []string{"user_1"}, AdminRole: "admin",
		PublicURL: "https://baoopn.com"},
		slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	h.a.now = func() time.Time { return h.clock }
	h.key = func(string) (*clerk.APIKey, error) {
		return &clerk.APIKey{ID: "ak_id_1", Type: "api_key", Subject: "user_1"}, nil
	}
	h.a.verifyKey = func(_ context.Context, secret string) (*clerk.APIKey, error) {
		h.calls.Add(1)
		return h.key(secret)
	}
	h.a.fetchUser = func(_ context.Context, id string) (*clerk.User, error) {
		return &clerk.User{ID: id, EmailAddresses: []*clerk.EmailAddress{{
			ID: "idn_1", EmailAddress: id + "@example.com",
			Verification: &clerk.Verification{Status: "verified"},
		}}}, nil
	}
	return h
}

// do sends one request and returns the response and the user the handler saw.
func (h *keyHarness) do(t *testing.T, authz string) (*httptest.ResponseRecorder, *User) {
	t.Helper()
	var seen *User
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := UserFrom(r.Context()); ok {
			seen = &u
		}
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/me", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.a.Middleware(next).ServeHTTP(rec, req)
	return rec, seen
}

func errBody(t *testing.T, rec *httptest.ResponseRecorder) (code, msg string) {
	t.Helper()
	var b struct {
		Error struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return b.Error.Code, b.Error.Message
}

// assertNoSecret checks that neither the logs nor the cache keys hold the secret.
func (h *keyHarness) assertNoSecret(t *testing.T, secret string) {
	t.Helper()
	if strings.Contains(h.logs.String(), secret) {
		t.Errorf("logs contain the secret: %s", h.logs.String())
	}
	h.a.mu.Lock()
	defer h.a.mu.Unlock()
	for k, v := range h.a.keyCache {
		if bytes.Contains(k[:], []byte(secret)) {
			t.Errorf("cache key holds the secret")
		}
		if strings.Contains(v.userID+v.keyID, secret) {
			t.Errorf("cache value holds the secret")
		}
	}
}

func TestAPIKeyAdminAccepted(t *testing.T) {
	h := newKeyHarness(t)
	rec, u := h.do(t, "Bearer ak_live_x")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if u == nil || u.ID != "user_1" || u.Via != "api_key" {
		t.Fatalf("user = %+v", u)
	}
	h.assertNoSecret(t, "ak_live_x")
}

func TestAPIKeyNonAdminForbidden(t *testing.T) {
	h := newKeyHarness(t)
	h.key = func(string) (*clerk.APIKey, error) {
		return &clerk.APIKey{ID: "ak_id_2", Type: "api_key", Subject: "user_9"}, nil
	}
	rec, _ := h.do(t, "Bearer ak_live_other")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d", rec.Code)
	}
	if code, msg := errBody(t, rec); code != "forbidden" || msg != "this account is not an admin" {
		t.Fatalf("body = %s %s", code, msg)
	}
	h.assertNoSecret(t, "ak_live_other")
}

func TestAPIKeyUnusable(t *testing.T) {
	past := int64(1_700_000_000)
	pastMS := past * 1000
	for name, k := range map[string]clerk.APIKey{
		"revoked":         {ID: "ak_id", Subject: "user_1", Revoked: true},
		"expired":         {ID: "ak_id", Subject: "user_1", Expired: true},
		"expiration s":    {ID: "ak_id", Subject: "user_1", Expiration: &past},
		"expiration ms":   {ID: "ak_id", Subject: "user_1", Expiration: &pastMS},
		"org subject":     {ID: "ak_id", Subject: "org_x"},
		"m2m token type":  {ID: "ak_id", Subject: "user_1", Type: "m2m_token"},
		"empty subject":   {ID: "ak_id", Type: "api_key"},
		"wrong user pref": {ID: "ak_id", Subject: "usr_1"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newKeyHarness(t)
			h.key = func(string) (*clerk.APIKey, error) { return &k, nil }
			rec, _ := h.do(t, "Bearer ak_live_bad")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", rec.Code)
			}
			if code, msg := errBody(t, rec); code != "unauthorized" || msg != "invalid API key" {
				t.Fatalf("body = %s %s", code, msg)
			}
			if len(h.a.keyCache) != 0 {
				t.Fatal("unusable key was cached")
			}
			h.assertNoSecret(t, "ak_live_bad")
		})
	}
}

func TestAPIKeyVerifyErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		code   string
	}{
		"clerk 404": {&clerk.APIErrorResponse{HTTPStatusCode: 404, TraceID: "tr_1"}, 401, "unauthorized"},
		"clerk 400": {&clerk.APIErrorResponse{HTTPStatusCode: 400}, 401, "unauthorized"},
		"clerk 429": {&clerk.APIErrorResponse{HTTPStatusCode: 429}, 502, "auth_unavailable"},
		"clerk 503": {&clerk.APIErrorResponse{HTTPStatusCode: 503}, 502, "auth_unavailable"},
		"network":   {errors.New("dial tcp: ak_live_err refused"), 502, "auth_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newKeyHarness(t)
			h.key = func(string) (*clerk.APIKey, error) { return nil, tc.err }
			rec, _ := h.do(t, "Bearer ak_live_err")
			if rec.Code != tc.status {
				t.Fatalf("status = %d", rec.Code)
			}
			if code, _ := errBody(t, rec); code != tc.code {
				t.Fatalf("code = %s", code)
			}
			h.assertNoSecret(t, "ak_live_err")
		})
	}
}

func TestAPIKeyCache(t *testing.T) {
	h := newKeyHarness(t)
	for range 2 {
		if rec, _ := h.do(t, "Bearer ak_live_x"); rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	if n := h.calls.Load(); n != 1 {
		t.Fatalf("verify calls within TTL = %d, want 1", n)
	}
	h.assertNoSecret(t, "ak_live_x")

	h.clock = h.clock.Add(61 * time.Second)
	if rec, _ := h.do(t, "Bearer ak_live_x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if n := h.calls.Load(); n != 2 {
		t.Fatalf("verify calls after TTL = %d, want 2", n)
	}

	h.clock = h.clock.Add(61 * time.Second)
	h.key = func(string) (*clerk.APIKey, error) { return nil, errors.New("dial") }
	if rec, _ := h.do(t, "Bearer ak_live_x"); rec.Code != http.StatusBadGateway {
		t.Fatalf("status after expiry with network error = %d, want 502", rec.Code)
	}
	if n := h.calls.Load(); n != 3 {
		t.Fatalf("verify calls = %d, want 3", n)
	}
	h.assertNoSecret(t, "ak_live_x")
}

func TestAPIKeyCacheHonoursExpiration(t *testing.T) {
	h := newKeyHarness(t)
	exp := h.clock.Add(10 * time.Second).UnixMilli()
	h.key = func(string) (*clerk.APIKey, error) {
		return &clerk.APIKey{ID: "ak_id_1", Subject: "user_1", Expiration: &exp}, nil
	}
	if rec, _ := h.do(t, "Bearer ak_live_x"); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	h.clock = h.clock.Add(11 * time.Second)
	if rec, _ := h.do(t, "Bearer ak_live_x"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status after key expiry = %d, want 401", rec.Code)
	}
	if n := h.calls.Load(); n != 2 {
		t.Fatalf("verify calls = %d, want 2", n)
	}
}

func TestAPIKeyDistinctSecrets(t *testing.T) {
	h := newKeyHarness(t)
	h.do(t, "Bearer ak_live_a")
	h.do(t, "Bearer ak_live_b")
	if n := h.calls.Load(); n != 2 {
		t.Fatalf("verify calls = %d, want 2", n)
	}
	h.assertNoSecret(t, "ak_live_a")
	h.assertNoSecret(t, "ak_live_b")
}

func TestAPIKeyCacheBounded(t *testing.T) {
	h := newKeyHarness(t)
	for i := range keyCacheMax + 10 {
		h.a.storeKey(strings.Repeat("k", i+1), keyCached{userID: "user_1", exp: h.clock.Add(time.Minute)})
	}
	if n := len(h.a.keyCache); n > keyCacheMax {
		t.Fatalf("cache size = %d, max %d", n, keyCacheMax)
	}
}

func TestSessionPathUnchanged(t *testing.T) {
	for name, authz := range map[string]string{
		"bad jwt":        "Bearer eyJ.bad.jwt",
		"missing header": "",
	} {
		t.Run(name, func(t *testing.T) {
			h := newKeyHarness(t)
			rec, _ := h.do(t, authz)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d", rec.Code)
			}
			if code, msg := errBody(t, rec); code != "unauthorized" || msg != "sign in required" {
				t.Fatalf("body = %s %s", code, msg)
			}
			if n := h.calls.Load(); n != 0 {
				t.Fatalf("verifyKey called %d times", n)
			}
		})
	}
}
