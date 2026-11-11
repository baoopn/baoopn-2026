package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/baoopn-2026/internal/imagestrip"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// JanitorInterval is how often RunJanitor ticks.
const JanitorInterval = 15 * time.Second

// tickTimeout bounds one janitor pass, so a hung database or S3 call cannot
// stall the janitor forever. It is longer than the orphan pass's own 2 min.
const tickTimeout = 5 * time.Minute

// RunJanitor runs Tick every interval until ctx is done.
func (s *Store) RunJanitor(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		tctx, cancel := context.WithTimeout(ctx, tickTimeout)
		err := s.safeTick(tctx)
		cancel()
		if err != nil && ctx.Err() == nil {
			s.log.Warn("media janitor", logging.Err(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// safeTick runs Tick and turns a panic into an error. publishOriginal parses
// untrusted bytes in the api process; a bug there must not take the api down.
func (s *Store) safeTick(ctx context.Context) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("janitor panic: %v", p)
		}
	}()
	return s.Tick(ctx)
}

// Tick is one janitor pass:
//  1. jobs whose lease expired on their last attempt fall back / fail;
//  2. conversion jobs nobody leased for FallbackAfter fall back
//     ("converter unavailable"), and so do running ones whose lease expired
//     FallbackAfter ago, at any attempt count, because no worker re-leased
//     them (cropped uploads fail instead; see toFallback);
//  3. due fallback jobs publish their stripped original;
//  4. originals of jobs failed longer than FailedRetention ago are deleted;
//  5. once a day, when turned on, unused media are deleted (orphans.go).
func (s *Store) Tick(ctx context.Context) error {
	if err := s.expireLeases(ctx); err != nil {
		return fmt.Errorf("expire leases: %w", err)
	}
	if err := s.fallbackStale(ctx); err != nil {
		return fmt.Errorf("stale jobs: %w", err)
	}
	for range 20 {
		j, err := s.lease(ctx, PhaseFallback, "api", s.jobLease)
		if err != nil {
			return err
		}
		if j == nil {
			break
		}
		s.publishOriginal(ctx, j)
	}
	if err := s.cleanupOriginals(ctx); err != nil {
		return err
	}
	s.maybeOrphanPass(ctx)
	return nil
}

func (s *Store) ids(ctx context.Context, q string, args ...any) ([]int64, error) {
	rows, err := s.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func (s *Store) expireLeases(ctx context.Context) error {
	ids, err := s.ids(ctx, `SELECT id FROM media_jobs
		WHERE status = 'running' AND lease_until < now() AND attempts >= max_attempts`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		err := s.withJob(ctx, id, `status = 'running' AND lease_until < now()`, nil, func(tx pgx.Tx, j jobRow) error {
			if j.phase == PhaseConvert {
				return s.toFallback(ctx, tx, j, "WebP conversion failed: the worker did not finish in time")
			}
			return s.markFailed(ctx, tx, j, "the original could not be published")
		})
		if err != nil && !errors.Is(err, ErrLeaseLost) {
			return err
		}
	}
	return nil
}

// staleJobs are the conversion jobs fallbackStale gives up on: sel picks ids
// ($1 = FallbackAfter in seconds) and cond re-checks the row under its lock
// (args from $2), so a job a worker leased meanwhile is skipped (ErrLeaseLost).
var staleJobs = []struct {
	sel, cond, reason string
	afterArg          bool // cond takes FallbackAfter as $2
}{
	{
		sel:    `phase = 'convert' AND status = 'queued' AND next_attempt_at < now() - make_interval(secs => $1)`,
		cond:   `phase = 'convert' AND status = 'queued'`,
		reason: "converter unavailable",
	},
	{
		sel:      `phase = 'convert' AND status = 'running' AND lease_until < now() - make_interval(secs => $1)`,
		cond:     `phase = 'convert' AND status = 'running' AND lease_until < now() - make_interval(secs => $2)`,
		reason:   "converter unavailable: the worker stopped responding",
		afterArg: true,
	},
}

func (s *Store) fallbackStale(ctx context.Context) error {
	after := s.fallbackAfter.Seconds()
	for _, st := range staleJobs {
		ids, err := s.ids(ctx, `SELECT id FROM media_jobs WHERE `+st.sel, after)
		if err != nil {
			return err
		}
		var args []any
		if st.afterArg {
			args = []any{after}
		}
		for _, id := range ids {
			err := s.withJob(ctx, id, st.cond, args, func(tx pgx.Tx, j jobRow) error {
				s.log.Warn("media: converter unavailable, falling back", slog.Int64("job", j.id),
					slog.Int64("media", j.mediaID), slog.String("reason", st.reason))
				return s.toFallback(ctx, tx, j, st.reason)
			})
			if err != nil && !errors.Is(err, ErrLeaseLost) {
				return err
			}
		}
	}
	return nil
}

// publishOriginal publishes a fallback job's original, metadata stripped,
// under its own extension. Storage failures back off like conversions.
func (s *Store) publishOriginal(ctx context.Context, j *Job) {
	log := s.log.With(slog.Int64("job", j.ID), slog.Int64("media", j.MediaID))
	stem := stemOf(j.Filename)
	fail := func(reason string) {
		err := s.withJob(ctx, j.ID, `lease_token = $2 AND status = 'running'`, []any{j.LeaseToken},
			func(tx pgx.Tx, r jobRow) error { return s.markFailed(ctx, tx, r, reason) })
		if err != nil {
			log.Warn("media fallback: mark failed", logging.Err(err))
		}
	}
	var reason string
	if err := s.db.QueryRow(ctx, `SELECT error FROM media WHERE id = $1`, j.MediaID).Scan(&reason); err != nil {
		log.Warn("media fallback: load media", logging.Err(err))
		return
	}
	if j.Crop != nil {
		// A cropped fallback row queued before cropped uploads stopped
		// falling back: publishing the original would undo the crop.
		fail(reason + msgCropKept)
		return
	}
	data, err := os.ReadFile(s.originalPath(stem))
	if err != nil {
		fail(reason + "; the original is missing")
		return
	}
	clean, err := imagestrip.Strip(data, j.ContentType, j.Orientation)
	if err != nil {
		fail(reason + "; the original could not be published")
		return
	}
	tmp, err := os.CreateTemp(s.dir, ".fb-*")
	if err != nil {
		log.Warn("media fallback: temp file", logging.Err(err))
		_ = s.retryStorage(ctx, j.ID, j.LeaseToken)
		return
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	_, werr := tmp.Write(clean)
	if cerr := tmp.Close(); werr != nil || cerr != nil {
		_ = s.retryStorage(ctx, j.ID, j.LeaseToken)
		return
	}
	name := stem + extByType[j.ContentType]
	if err := s.files.put(ctx, name, j.ContentType, tmpName, int64(len(clean))); err != nil {
		log.Warn("media fallback: store original", logging.Err(err))
		_ = s.retryStorage(ctx, j.ID, j.LeaseToken)
		return
	}
	sum := sha256.Sum256(clean)
	final := reason + "; saved the original"
	err = s.withJob(ctx, j.ID, `lease_token = $2 AND status = 'running'`, []any{j.LeaseToken}, func(tx pgx.Tx, r jobRow) error {
		if _, err := tx.Exec(ctx, `UPDATE media_jobs SET status = 'done', lease_token = '', lease_until = NULL,
			original_deleted = true, updated_at = now() WHERE id = $1`, r.id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE media SET status = 'ready', converted = false, error = $2, filename = $3,
			content_type = $4, size_bytes = $5, sha256 = $6 WHERE id = $1`,
			r.mediaID, final, name, j.ContentType, len(clean), hex.EncodeToString(sum[:]))
		return err
	})
	if err != nil {
		log.Warn("media fallback: finish", logging.Err(err))
		s.dropIfOrphan(ctx, j.MediaID, name)
		return
	}
	s.removeOriginal(stem)
	log.Warn("media: published the original instead of a WebP", slog.String("reason", reason))
	if name != j.Filename && s.onRename != nil {
		if err := s.onRename(ctx, s.URL(j.Filename), s.URL(name)); err != nil {
			log.Warn("media fallback: update references", logging.Err(err))
		}
	}
}

func (s *Store) cleanupOriginals(ctx context.Context) error {
	list, err := s.staleOriginals(ctx)
	if err != nil {
		return err
	}
	for _, id := range list {
		if err := s.dropOriginal(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// staleOriginals lists the jobs failed longer than FailedRetention ago whose
// original is still kept.
func (s *Store) staleOriginals(ctx context.Context) ([]int64, error) {
	return s.ids(ctx, `SELECT id FROM media_jobs
		WHERE status = 'failed' AND NOT original_deleted AND failed_at < now() - make_interval(secs => $1)`,
		s.failedRetention.Seconds())
}

// dropOriginal claims job id's original first and deletes the file only when
// the claim took. The claim re-checks the row, so a Retry that requeued the
// job after staleOriginals listed it keeps its original.
func (s *Store) dropOriginal(ctx context.Context, id int64) error {
	var filename string
	err := s.db.QueryRow(ctx, `UPDATE media_jobs j SET original_deleted = true, updated_at = now()
		FROM media m WHERE m.id = j.media_id AND j.id = $1 AND j.status = 'failed' AND NOT j.original_deleted
		RETURNING m.filename`, id).Scan(&filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	s.removeOriginal(stemOf(filename))
	return nil
}

// QueueStats counts jobs by state (admin overview).
type QueueStats struct {
	Queued, Running, Failed int64
}

// Stats counts queued (incl. fallbacks), running and failed jobs.
func (s *Store) Stats(ctx context.Context) (QueueStats, error) {
	var st QueueStats
	err := s.db.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'queued'),
		count(*) FILTER (WHERE status = 'running'), count(*) FILTER (WHERE status = 'failed') FROM media_jobs`).
		Scan(&st.Queued, &st.Running, &st.Failed)
	return st, err
}
