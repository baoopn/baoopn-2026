package media

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestStaleRunningRecheckSkipsReleasedJob: fallbackStale picks a dead running
// job, but a worker re-leases it before the janitor takes the row lock. The
// re-check under the lock must see the fresh lease and skip the job
// (ErrLeaseLost), so the new worker's attempt is not undone.
func TestStaleRunningRecheckSkipsReleasedJob(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.FallbackAfter = time.Minute; d.MaxAttempts = 3 })
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), nil)
	j := mustLease(t, s)
	deadWorker(t, s, m, time.Minute+time.Second)

	var running *struct {
		sel, cond, reason string
		afterArg          bool
	}
	for i := range staleJobs {
		if staleJobs[i].afterArg {
			running = &staleJobs[i]
		}
	}
	if running == nil {
		t.Fatal("no running-job entry in staleJobs")
	}
	after := s.fallbackAfter.Seconds()
	ids, err := s.ids(ctx, `SELECT id FROM media_jobs WHERE `+running.sel, after)
	if err != nil || len(ids) != 1 || ids[0] != j.ID {
		t.Fatalf("stale ids %v %v, want [%d]", ids, err, j.ID)
	}

	// A worker re-leases it between the select and the lock.
	exec(t, s, `UPDATE media_jobs SET lease_until = now() + interval '5 minutes' WHERE id = $1`, j.ID)

	called := false
	err = s.withJob(ctx, j.ID, running.cond, []any{after}, func(pgx.Tx, jobRow) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrLeaseLost) || called {
		t.Fatalf("withJob err=%v called=%v, want ErrLeaseLost and not called", err, called)
	}
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, m.ID); got.Status != StatusProcessing || got.Error != "" {
		t.Fatalf("media %+v", got)
	}
	if phase, status, _ := jobState(t, s, m.ID); phase != PhaseConvert || status != "running" {
		t.Fatalf("job %s %s", phase, status)
	}
}
