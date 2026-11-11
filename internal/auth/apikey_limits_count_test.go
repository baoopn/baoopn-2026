package auth

import (
	"net/http"
	"strings"
	"testing"
)

// TestAPIKeyLocalRefusalsCountTowardLimit: refusals that never reach Clerk
// (a bad shape, a negative-cache hit) still count toward the client's limit,
// so a client cannot probe without end by repeating a refused key.
func TestAPIKeyLocalRefusalsCountTowardLimit(t *testing.T) {
	t.Run("bad shape", func(t *testing.T) {
		h := newKeyHarness(t)
		for i := range keyFailuresPerMinute {
			if rec := h.doFrom(t, "10.0.0.1:1", "Bearer ak_"+strings.Repeat("x", i%15)); rec.Code != http.StatusUnauthorized {
				t.Fatalf("bad shape %d: %d", i, rec.Code)
			}
		}
		if rec := h.doFrom(t, "10.0.0.1:1", "Bearer ak_live_fresh_0123456789abcdef"); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("after %d bad shapes: %d", keyFailuresPerMinute, rec.Code)
		}
		if n := h.calls.Load(); n != 0 {
			t.Fatalf("verify calls = %d, want 0", n)
		}
	})
	t.Run("negative cache hit", func(t *testing.T) {
		const secret = "ak_live_gone_0123456789abcdef"
		h := newKeyHarness(t)
		h.key = refusedBy(http.StatusNotFound)
		for i := range keyFailuresPerMinute {
			if rec := h.doFrom(t, "10.0.0.1:1", "Bearer "+secret); rec.Code != http.StatusUnauthorized {
				t.Fatalf("refused %d: %d", i, rec.Code)
			}
		}
		if rec := h.doFrom(t, "10.0.0.1:1", "Bearer "+secret); rec.Code != http.StatusTooManyRequests {
			t.Fatalf("after %d refusals: %d", keyFailuresPerMinute, rec.Code)
		}
		if n := h.calls.Load(); n != 1 {
			t.Fatalf("verify calls = %d, want 1", n)
		}
	})
}
