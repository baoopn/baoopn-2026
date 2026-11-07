package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/baoopn-2026/internal/imagestrip"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Job phases: workers lease convert jobs; the API runs fallback jobs.
const (
	PhaseConvert  = "convert"
	PhaseFallback = "fallback"
)

var (
	// ErrLeaseLost means the job is not (or no longer) leased with that token.
	ErrLeaseLost = errors.New("media: job is not leased with this token")
	// ErrStorageUnavailable means the bucket (or disk) refused the file; the
	// job was re-queued with backoff, or failed after its last attempt.
	ErrStorageUnavailable = errors.New("media: storage unavailable")
	// ErrGone means a failed job's original was already cleaned up.
	ErrGone = errors.New("media: original no longer available")
	// ErrNotFailed means a retry was asked for media that has not failed.
	ErrNotFailed = errors.New("media: media has not failed")
)

// Job is a leased conversion job.
type Job struct {
	ID          int64
	MediaID     int64
	Filename    string // the stored name (<stem>.webp)
	ContentType string // the original's
	Size        int64
	Orientation int
	Crop        *Crop
	Attempt     int
	MaxAttempts int
	LeaseToken  string
	LeaseUntil  time.Time
}

func (s *Store) originalsDir() string            { return filepath.Join(s.dir, "originals") }
func (s *Store) originalPath(stem string) string { return filepath.Join(s.originalsDir(), stem) }

// stemOf strips the extension of a stored filename.
func stemOf(filename string) string { return strings.TrimSuffix(filename, filepath.Ext(filename)) }

func (s *Store) removeOriginal(stem string) {
	if stem == "" {
		return
	}
	if err := os.Remove(s.originalPath(stem)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.log.Warn("media: remove original", slog.String("stem", stem), logging.Err(err))
	}
}

// enqueue moves the upload into the private originals store and inserts a
// processing media row with its conversion job.
func (s *Store) enqueue(ctx context.Context, stem, original, ctype, sum string, size int64,
	orientation int, crop *Crop, tmpName string) (Media, error) {
	if err := os.MkdirAll(s.originalsDir(), 0o700); err != nil {
		return Media{}, fmt.Errorf("%w: %v", ErrOriginalsUnavailable, err)
	}
	orig := s.originalPath(stem)
	if err := os.Rename(tmpName, orig); err != nil {
		return Media{}, fmt.Errorf("%w: %v", ErrOriginalsUnavailable, err)
	}
	origSum := sum
	var cx, cy, cw, ch *int
	if crop != nil {
		origSum = "" // a cropped result is not "the same file" as its original
		cx, cy, cw, ch = &crop.X, &crop.Y, &crop.W, &crop.H
	}
	m := Media{Filename: stem + ".webp", Original: original, ContentType: "image/webp", Status: StatusProcessing}
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO media (filename, original, content_type, size_bytes, sha256, original_sha256, status)
			VALUES ($1, $2, 'image/webp', 0, '', $3, 'processing') RETURNING id, created_at`,
			m.Filename, original, origSum).Scan(&m.ID, &m.CreatedAt); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO media_jobs (media_id, original_content_type, original_size, orientation,
				crop_x, crop_y, crop_w, crop_h, max_attempts)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			m.ID, ctype, size, orientation, cx, cy, cw, ch, s.maxAttempts)
		return err
	})
	if err != nil {
		_ = os.Remove(orig)
		return Media{}, fmt.Errorf("media: enqueue: %w", err)
	}
	return s.finish(m), nil
}

// Lease hands the next due conversion job to a worker: a queued job whose
// backoff is over, or a running one whose lease expired (its worker died).
// It returns nil when there is none. Concurrent callers never get the same
// job (FOR UPDATE SKIP LOCKED).
func (s *Store) Lease(ctx context.Context, workerID string) (*Job, error) {
	return s.lease(ctx, PhaseConvert, workerID, s.jobLease)
}

func (s *Store) lease(ctx context.Context, phase, workerID string, d time.Duration) (*Job, error) {
	j := &Job{LeaseToken: randomStem()}
	var cx, cy, cw, ch *int
	err := s.db.QueryRow(ctx, `
		UPDATE media_jobs j SET status = 'running', attempts = j.attempts + 1, worker_id = $1,
			lease_token = $2, lease_until = now() + make_interval(secs => $3), updated_at = now()
		FROM media m
		WHERE m.id = j.media_id AND j.id = (
			SELECT id FROM media_jobs
			WHERE phase = $4 AND ((status = 'queued' AND next_attempt_at <= now())
				OR (status = 'running' AND lease_until < now() AND attempts < max_attempts))
			ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING j.id, j.media_id, m.filename, j.original_content_type, j.original_size, j.orientation,
			j.crop_x, j.crop_y, j.crop_w, j.crop_h, j.attempts, j.max_attempts, j.lease_until`,
		workerID, j.LeaseToken, d.Seconds(), phase).Scan(&j.ID, &j.MediaID, &j.Filename, &j.ContentType,
		&j.Size, &j.Orientation, &cx, &cy, &cw, &ch, &j.Attempt, &j.MaxAttempts, &j.LeaseUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("media: lease: %w", err)
	}
	if cx != nil && cy != nil && cw != nil && ch != nil {
		j.Crop = &Crop{X: *cx, Y: *cy, W: *cw, H: *ch}
	}
	return j, nil
}

// OpenOriginal opens a leased job's original.
func (s *Store) OpenOriginal(ctx context.Context, jobID int64, token string) (*os.File, error) {
	var filename string
	err := s.db.QueryRow(ctx, `SELECT m.filename FROM media_jobs j JOIN media m ON m.id = j.media_id
		WHERE j.id = $1 AND j.lease_token = $2 AND j.status = 'running'`, jobID, token).Scan(&filename)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLeaseLost
	}
	if err != nil {
		return nil, err
	}
	return os.Open(s.originalPath(stemOf(filename)))
}

// Complete stores a leased job's WebP (the file at path; it may be moved),
// marks the media ready and deletes the original. When storage fails the
// job is re-queued with backoff (or failed after its last attempt) and
// ErrStorageUnavailable is returned.
func (s *Store) Complete(ctx context.Context, jobID int64, token, path string, size int64, sum string) (Media, error) {
	var filename string
	var mediaID int64
	err := s.db.QueryRow(ctx, `SELECT m.filename, m.id FROM media_jobs j JOIN media m ON m.id = j.media_id
		WHERE j.id = $1 AND j.lease_token = $2 AND j.status = 'running' AND j.phase = 'convert'`,
		jobID, token).Scan(&filename, &mediaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Media{}, ErrLeaseLost
	}
	if err != nil {
		return Media{}, err
	}
	if err := s.files.put(ctx, filename, "image/webp", path, size); err != nil {
		s.log.Warn("media: store converted file", slog.Int64("job", jobID), logging.Err(err))
		if serr := s.retryStorage(ctx, jobID, token); serr != nil {
			return Media{}, serr
		}
		return Media{}, fmt.Errorf("%w: %v", ErrStorageUnavailable, err)
	}
	err = s.withJob(ctx, jobID, `lease_token = $2 AND status = 'running'`, []any{token}, func(tx pgx.Tx, j jobRow) error {
		if _, err := tx.Exec(ctx, `UPDATE media_jobs SET status = 'done', lease_token = '', lease_until = NULL,
			last_error = '', original_deleted = true, updated_at = now() WHERE id = $1`, jobID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE media SET status = 'ready', converted = true, error = '',
			content_type = 'image/webp', size_bytes = $2, sha256 = $3 WHERE id = $1`, j.mediaID, size, sum)
		return err
	})
	if err != nil {
		if errors.Is(err, ErrLeaseLost) {
			s.dropIfOrphan(ctx, mediaID, filename)
		}
		return Media{}, err
	}
	s.removeOriginal(stemOf(filename))
	return s.Get(ctx, mediaID)
}

// dropIfOrphan removes a just-stored file whose media row was deleted meanwhile.
func (s *Store) dropIfOrphan(ctx context.Context, mediaID int64, filename string) {
	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media WHERE id = $1)`, mediaID).Scan(&exists); err == nil && !exists {
		s.removeFile(ctx, filename)
	}
}

// Fail records a worker's failure. A retryable failure is retried with
// backoff until the job's last attempt; then, or for a permanent failure,
// the job falls back (the stripped original is published when browsers can
// show it, else the media fails). It reports whether the job will be retried.
func (s *Store) Fail(ctx context.Context, jobID int64, token, msg string, retryable bool) (bool, error) {
	retry := false
	err := s.withJob(ctx, jobID, `lease_token = $2 AND status = 'running' AND phase = 'convert'`, []any{token},
		func(tx pgx.Tx, j jobRow) error {
			if retryable && j.attempts < j.maxAttempts {
				retry = true
				return s.requeue(ctx, tx, j, msg)
			}
			return s.toFallback(ctx, tx, j, "WebP conversion failed: "+msg)
		})
	return retry, err
}

// jobRow is a locked media_jobs row.
type jobRow struct {
	id, mediaID           int64
	phase, ctype          string
	attempts, maxAttempts int
}

// withJob locks job id (if cond, with extra args from $2, holds) and runs fn
// in a transaction. A missing or locked row is ErrLeaseLost.
func (s *Store) withJob(ctx context.Context, id int64, cond string, args []any, fn func(pgx.Tx, jobRow) error) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var j jobRow
		err := tx.QueryRow(ctx, `SELECT id, media_id, phase, original_content_type, attempts, max_attempts
			FROM media_jobs WHERE id = $1 AND `+cond+` FOR UPDATE SKIP LOCKED`, append([]any{id}, args...)...).
			Scan(&j.id, &j.mediaID, &j.phase, &j.ctype, &j.attempts, &j.maxAttempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrLeaseLost
		}
		if err != nil {
			return err
		}
		return fn(tx, j)
	})
}

// backoff is 5s·2^(attempt-1), capped at 10 minutes, with ±50% jitter
// (uniform in [d/2, d]).
func backoff(attempt int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < attempt && d < 10*time.Minute; i++ {
		d *= 2
	}
	d = min(d, 10*time.Minute)
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

func (s *Store) requeue(ctx context.Context, tx pgx.Tx, j jobRow, msg string) error {
	_, err := tx.Exec(ctx, `UPDATE media_jobs SET status = 'queued', lease_token = '', lease_until = NULL,
		next_attempt_at = now() + make_interval(secs => $2), last_error = $3, updated_at = now() WHERE id = $1`,
		j.id, backoff(j.attempts).Seconds(), msg)
	return err
}

// toFallback turns a conversion that cannot succeed into a fallback job when
// the original is browser-safe, and fails the media otherwise.
func (s *Store) toFallback(ctx context.Context, tx pgx.Tx, j jobRow, reason string) error {
	if !imagestrip.BrowserSafe(j.ctype) {
		return s.markFailed(ctx, tx, j, reason)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_jobs SET phase = 'fallback', status = 'queued', attempts = 0,
		next_attempt_at = now(), lease_token = '', lease_until = NULL, last_error = $2, updated_at = now()
		WHERE id = $1`, j.id, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE media SET error = $2 WHERE id = $1`, j.mediaID, reason)
	return err
}

// markFailed fails the job and its media; the original is kept for
// FailedRetention so the media can be retried.
func (s *Store) markFailed(ctx context.Context, tx pgx.Tx, j jobRow, reason string) error {
	s.log.Warn("media: job failed", slog.Int64("job", j.id), slog.Int64("media", j.mediaID), slog.String("reason", reason))
	if _, err := tx.Exec(ctx, `UPDATE media_jobs SET status = 'failed', failed_at = now(), lease_token = '',
		lease_until = NULL, last_error = $2, updated_at = now() WHERE id = $1`, j.id, reason); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE media SET status = 'failed', error = $2 WHERE id = $1`, j.mediaID, reason)
	return err
}

// retryStorage handles a storage failure of a leased job (either phase):
// backoff while attempts remain, then "storage unavailable".
func (s *Store) retryStorage(ctx context.Context, jobID int64, token string) error {
	return s.withJob(ctx, jobID, `lease_token = $2 AND status = 'running'`, []any{token}, func(tx pgx.Tx, j jobRow) error {
		if j.attempts < j.maxAttempts {
			return s.requeue(ctx, tx, j, "storage unavailable")
		}
		return s.markFailed(ctx, tx, j, "storage unavailable")
	})
}

// Retry re-queues the conversion of failed media whose original is still kept.
func (s *Store) Retry(ctx context.Context, mediaID int64) (Media, error) {
	var jobID *int64
	var status *string
	var deleted *bool
	var mstatus, filename string
	err := s.db.QueryRow(ctx, `SELECT j.id, j.status, j.original_deleted, m.status, m.filename
		FROM media m LEFT JOIN media_jobs j ON j.media_id = m.id WHERE m.id = $1`, mediaID).
		Scan(&jobID, &status, &deleted, &mstatus, &filename)
	if err != nil {
		return Media{}, err // pgx.ErrNoRows: no such media
	}
	if mstatus != StatusFailed {
		return Media{}, ErrNotFailed
	}
	if jobID == nil || *status != "failed" || *deleted {
		return Media{}, ErrGone
	}
	if _, err := os.Stat(s.originalPath(stemOf(filename))); err != nil {
		return Media{}, ErrGone
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE media_jobs SET phase = 'convert', status = 'queued', attempts = 0,
			next_attempt_at = now(), failed_at = NULL, last_error = '', lease_token = '', lease_until = NULL,
			updated_at = now() WHERE id = $1`, *jobID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE media SET status = 'processing', error = '' WHERE id = $1`, mediaID)
		return err
	})
	if err != nil {
		return Media{}, err
	}
	return s.Get(ctx, mediaID)
}

// WaitDone polls media id until it is no longer processing or d passed.
func (s *Store) WaitDone(ctx context.Context, id int64, d time.Duration) (Media, error) {
	deadline := time.Now().Add(d)
	for {
		m, err := s.Get(ctx, id)
		if err != nil || m.Status != StatusProcessing || !time.Now().Before(deadline) {
			return m, err
		}
		select {
		case <-ctx.Done():
			return m, nil
		case <-time.After(200 * time.Millisecond):
		}
	}
}
