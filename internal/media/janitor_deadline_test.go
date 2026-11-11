package media

import (
	"context"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/testimg"
)

// With less than fallbackMinLeft of the tick left, no new fallback job
// starts; the next tick publishes it.
func TestFallbackStopsNearDeadline(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.FallbackAfter = time.Minute })
	jpg := mustSave(t, s, "photo.jpg", testimg.JPEGWithExif(testimg.Halves(8, 4), 1, true), nil)
	exec(t, s, `UPDATE media_jobs SET next_attempt_at = now() - interval '2 minutes'`)
	short, cancel := context.WithTimeout(context.Background(), fallbackMinLeft/3)
	defer cancel()
	if err := s.Tick(short); err != nil {
		t.Fatal(err)
	}
	if phase, status, _ := jobState(t, s, jpg.ID); phase != PhaseFallback || status != "queued" {
		t.Fatalf("job %s/%s, want a queued fallback", phase, status)
	}
	if err := s.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(context.Background(), jpg.ID); got.Status != StatusReady {
		t.Fatalf("next tick: %+v", got)
	}
}

// The rename hook runs on its own deadline: a tick context that already
// expired after the rename committed does not stop it.
func TestRenameHookIgnoresTickDeadline(t *testing.T) {
	var hookErr error
	var called bool
	s, _ := newQueueStore(t, func(d *Deps) {
		d.OnRename = func(ctx context.Context, _, _ string) error {
			called, hookErr = true, ctx.Err()
			if _, ok := ctx.Deadline(); !ok {
				t.Error("hook context has no deadline")
			}
			return nil
		}
	})
	expired, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()
	s.followRename(expired, "0123456789abcdef01234567.webp", "0123456789abcdef01234567.png")
	if !called || hookErr != nil {
		t.Fatalf("hook called=%v ctx err=%v", called, hookErr)
	}
}
