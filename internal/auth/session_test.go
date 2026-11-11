package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clerk/clerk-sdk-go/v2"
)

func verifiedUser(id string, meta string) *clerk.User {
	return &clerk.User{ID: id, PublicMetadata: json.RawMessage(meta), EmailAddresses: []*clerk.EmailAddress{{
		ID: "idn_1", EmailAddress: id + "@example.com", Verification: &clerk.Verification{Status: "verified"},
	}}}
}

func clerkStatus(code int) func() (*clerk.User, error) {
	return func() (*clerk.User, error) {
		return nil, &clerk.APIErrorResponse{HTTPStatusCode: code}
	}
}

func TestSessionUser(t *testing.T) {
	for name, tc := range map[string]struct {
		fetch      func() (*clerk.User, error)
		claimRole  string
		wantOK     bool
		wantStatus int
		wantRole   string
	}{
		"lookup role wins over claim": {
			fetch:     func() (*clerk.User, error) { return verifiedUser("user_2", `{"role":"editor"}`), nil },
			claimRole: "admin", wantOK: true, wantRole: "editor",
		},
		"banned with admin claim": {
			fetch: func() (*clerk.User, error) {
				u := verifiedUser("user_2", `{"role":"admin"}`)
				u.Banned = true
				return u, nil
			},
			claimRole: "admin", wantStatus: http.StatusForbidden,
		},
		"locked allow-listed id": {
			fetch:     func() (*clerk.User, error) { u := verifiedUser("user_1", `{}`); u.Locked = true; return u, nil },
			claimRole: "admin", wantStatus: http.StatusForbidden,
		},
		"clerk down, claim fallback": {
			fetch:     func() (*clerk.User, error) { return nil, errors.New("dial tcp: refused") },
			claimRole: "admin", wantOK: true, wantRole: "admin",
		},
		"deleted user (404) with admin claim": {
			fetch:     clerkStatus(http.StatusNotFound),
			claimRole: "admin", wantStatus: http.StatusUnauthorized,
		},
		"clerk refuses our secret (401), claim not used": {
			fetch:     clerkStatus(http.StatusUnauthorized),
			claimRole: "admin", wantStatus: http.StatusServiceUnavailable,
		},
		"clerk 5xx, claim fallback": {
			fetch:     clerkStatus(http.StatusBadGateway),
			claimRole: "admin", wantOK: true, wantRole: "admin",
		},
		"clerk 429, claim fallback": {
			fetch:     clerkStatus(http.StatusTooManyRequests),
			claimRole: "admin", wantOK: true, wantRole: "admin",
		},
		"clerk down, no claim": {
			fetch:      func() (*clerk.User, error) { return nil, errors.New("dial tcp: refused") },
			wantStatus: http.StatusServiceUnavailable,
		},
	} {
		t.Run(name, func(t *testing.T) {
			a := newTest(false)
			a.fetchUser = func(context.Context, string) (*clerk.User, error) { return tc.fetch() }
			rec := httptest.NewRecorder()
			u, ok := a.sessionUser(rec, httptest.NewRequest("GET", "/api/admin/me", nil), "user_x", tc.claimRole)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v (status %d %s)", ok, rec.Code, rec.Body)
			}
			if ok && u.Role != tc.wantRole {
				t.Fatalf("role = %q, want %q", u.Role, tc.wantRole)
			}
			if !ok && rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

// A banned user on the ID allow-list must not get in with an API key.
func TestAPIKeyBannedUserForbidden(t *testing.T) {
	h := newKeyHarness(t)
	h.a.fetchUser = func(_ context.Context, id string) (*clerk.User, error) {
		u := verifiedUser(id, `{}`)
		u.Banned = true
		return u, nil
	}
	rec, seen := h.do(t, "Bearer ak_live_x_0123456789abcdef")
	if rec.Code != http.StatusForbidden || seen != nil {
		t.Fatalf("status %d, user %v", rec.Code, seen)
	}
	if code, _ := errBody(t, rec); code != "forbidden" {
		t.Fatalf("code = %q", code)
	}
}

// An API key whose Clerk user was deleted is refused with 401.
func TestAPIKeyDeletedUserUnauthorized(t *testing.T) {
	h := newKeyHarness(t)
	h.a.fetchUser = func(context.Context, string) (*clerk.User, error) { return clerkStatus(http.StatusNotFound)() }
	rec, seen := h.do(t, "Bearer ak_live_x_0123456789abcdef")
	if rec.Code != http.StatusUnauthorized || seen != nil {
		t.Fatalf("status %d, user %v", rec.Code, seen)
	}
}
