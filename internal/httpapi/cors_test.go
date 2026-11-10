package httpapi

import (
	"net/http/httptest"
	"testing"
)

func TestAllowedOrigin(t *testing.T) {
	list := []string{"https://listening.baoopn.com", "http://localhost:5173"}
	cases := []struct {
		name, origin, want string
	}{
		{"exact match", "https://listening.baoopn.com", "https://listening.baoopn.com"},
		{"uppercase", "HTTPS://Listening.Baoopn.com", "https://listening.baoopn.com"},
		{"null", "null", ""},
		{"empty", "", ""},
		{"path suffix", "https://listening.baoopn.com/x", ""},
		{"unlisted", "https://evil.example", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if got := AllowedOrigin(r, list); got != c.want {
				t.Fatalf("AllowedOrigin(%q) = %q, want %q", c.origin, got, c.want)
			}
		})
	}
}
