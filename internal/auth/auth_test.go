package auth

import (
	"io"
	"log/slog"
	"testing"
)

func newTest(dev bool) *Authenticator {
	return New(Config{SecretKey: "sk_test_x", AllowedEmails: []string{" Me@Baoopn.com "},
		AllowedUserIDs: []string{"user_1"}, AdminRole: "admin", PublicURL: "https://baoopn.com/x", Dev: dev},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestPartyAllowed(t *testing.T) {
	prod, dev := newTest(false), newTest(true)
	for azp, want := range map[string][2]bool{ // [prod, dev]
		"https://baoopn.com":      {true, true},
		"":                        {false, true},
		"https://evil.com":        {false, false},
		"http://localhost:5173":   {false, true},
		"http://127.0.0.1:4173":   {false, true},
		"https://baoopn.com.evil": {false, false},
	} {
		if got := prod.partyAllowed(azp); got != want[0] {
			t.Errorf("prod partyAllowed(%q) = %v", azp, got)
		}
		if got := dev.partyAllowed(azp); got != want[1] {
			t.Errorf("dev partyAllowed(%q) = %v", azp, got)
		}
	}
}

func TestAllowed(t *testing.T) {
	a := newTest(false)
	for name, tc := range map[string]struct {
		u    User
		want bool
	}{
		"allow-listed id":        {User{ID: "user_1"}, true},
		"admin role":             {User{ID: "user_2", Role: "admin"}, true},
		"allow-listed email":     {User{ID: "user_3", Email: "me@baoopn.com"}, true},
		"other role":             {User{ID: "user_4", Role: "editor"}, false},
		"other email":            {User{ID: "user_5", Email: "you@example.com"}, false},
		"banned (empty profile)": {User{ID: "user_6"}, false},
	} {
		if got := a.allowed(tc.u); got != tc.want {
			t.Errorf("%s: allowed = %v", name, got)
		}
	}
}
