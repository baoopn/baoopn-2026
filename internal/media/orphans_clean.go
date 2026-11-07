package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/mediarefs"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

var (
	// ErrBusy means another cleanup run holds the run lock.
	ErrBusy = errors.New("media: an orphan cleanup is already running")
	// ErrDisabled means MEDIA_ORPHAN_GRACE is 0.
	ErrDisabled = errors.New("media: orphan cleanup is off")
)

// orphanRunLock is the session advisory lock that allows one run at a time.
const orphanRunLock int64 = 0x6d656469615f636c // "media_cl"

// OrphanSettingsKey is the settings key holding the auto flag and the last
// run's counts. Counts only: a file name there would count as a reference.
const OrphanSettingsKey = "media.orphans"

// Triggers of a cleanup run.
const (
	TriggerAuto   = "auto"
	TriggerManual = "manual"
)

const autoEvery = 24 * time.Hour

// OrphanRun summarizes a cleanup run.
type OrphanRun struct {
	At      time.Time `json:"at"`
	Trigger string    `json:"trigger"`
	Deleted int       `json:"deleted"`
	Bytes   int64     `json:"bytes"`
	Errors  int       `json:"errors"`
}

// OrphanSettings is the stored media.orphans value.
type OrphanSettings struct {
	Auto       bool       `json:"auto"`
	LastRun    *OrphanRun `json:"last_run"`
	LastAutoAt *time.Time `json:"last_auto_at,omitempty"`
}

// OrphanDeleted counts deletions by kind.
type OrphanDeleted struct {
	Rows        int `json:"rows"`
	Objects     int `json:"objects"`
	Local       int `json:"local"`
	LocalCopies int `json:"local_copies"`
	Originals   int `json:"originals"`
	Temp        int `json:"temp"`
}

func (d OrphanDeleted) total() int {
	return d.Rows + d.Objects + d.Local + d.LocalCopies + d.Originals + d.Temp
}

func (d *OrphanDeleted) add(kind string) {
	switch kind {
	case KindRow:
		d.Rows++
	case KindObject:
		d.Objects++
	case KindLocal:
		d.Local++
	case KindLocalCopy:
		d.LocalCopies++
	case KindOriginal:
		d.Originals++
	case KindTemp:
		d.Temp++
	}
}

// OrphanSkip is a requested key that was not deleted, and why.
type OrphanSkip struct {
	Key string `json:"key"`
	Why string `json:"why"`
}

// OrphanResult is what a cleanup run did (POST /media/orphans).
type OrphanResult struct {
	Deleted     OrphanDeleted `json:"deleted"`
	Bytes       int64         `json:"bytes"`
	AlreadyGone int           `json:"already_gone"`
	Skipped     []OrphanSkip  `json:"skipped"`
	Errors      int           `json:"errors"`
	Capped      bool          `json:"capped"`
	Remaining   int           `json:"remaining"`
	Reason      string        `json:"reason,omitempty"` // "brake": the auto run deleted nothing
}

// CleanOrphans deletes eligible candidates of a fresh plan: those in keys,
// or (keys nil) all of them except local copies. Each is re-checked under
// the exclusive references lock before its row goes; files are removed
// after the commit.
func (s *Store) CleanOrphans(ctx context.Context, trigger string, keys []string) (OrphanResult, error) {
	res := OrphanResult{Skipped: []OrphanSkip{}}
	if !s.OrphansEnabled() {
		return res, ErrDisabled
	}
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, orphanRunLock).Scan(&locked); err != nil {
		return res, err
	}
	if !locked {
		return res, ErrBusy
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, orphanRunLock); err != nil {
			_ = conn.Conn().Close(context.Background()) // drops the session lock
		}
	}()

	plan, err := s.PlanOrphans(ctx)
	if err != nil {
		return res, err
	}
	work := s.selectWork(plan, keys, &res)
	if trigger == TriggerAuto {
		if why := brake(plan, work); why != "" {
			s.log.Warn("media: orphan cleanup braked, nothing deleted", slog.String("why", why))
			res.Reason = "brake"
			s.recordRun(ctx, trigger, res)
			return res, nil
		}
	}
	if len(work) > s.orphanMax {
		res.Capped, res.Remaining = true, len(work)-s.orphanMax
		work = work[:s.orphanMax]
	}

	passed, err := s.recheckAndDeleteRows(ctx, work, &res)
	if err != nil {
		return res, err
	}
	for _, c := range passed {
		s.removeCandidate(ctx, c, trigger, &res)
	}
	s.recordRun(ctx, trigger, res)
	return res, nil
}

// selectWork picks the eligible candidates a run deletes.
func (s *Store) selectWork(plan OrphanPlan, keys []string, res *OrphanResult) []OrphanCandidate {
	var work []OrphanCandidate
	if keys == nil {
		for _, c := range plan.Candidates {
			if c.Eligible && c.Kind != KindLocalCopy { // the last local backup: keys only
				work = append(work, c)
			}
		}
		return work
	}
	byKey := make(map[string]OrphanCandidate, len(plan.Candidates))
	for _, c := range plan.Candidates {
		byKey[c.Key] = c
	}
	seen := map[string]bool{}
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		c, ok := byKey[k]
		switch {
		case !ok:
			res.AlreadyGone++
		case !c.Eligible:
			why := c.Blocked
			if why == "" {
				why = "not eligible"
			}
			res.Skipped = append(res.Skipped, OrphanSkip{Key: k, Why: why})
		default:
			work = append(work, c)
		}
	}
	return work
}

// brake stops an automatic run that looks wrong: it would delete many rows,
// or no reference was found although files are in use.
func brake(plan OrphanPlan, work []OrphanCandidate) string {
	rows := 0
	for _, c := range work {
		if c.Kind == KindRow {
			rows++
		}
	}
	if rows > max(10, plan.rows/2) {
		return fmt.Sprintf("%d of %d media rows would be deleted", rows, plan.rows)
	}
	if plan.refs == 0 && plan.readyRows >= 3 {
		return "no media references found in any content"
	}
	return ""
}

// recheckAndDeleteRows re-checks every candidate against the current
// references under the exclusive lock and deletes the rows that still
// qualify, all in one transaction. It returns the candidates whose files
// may now be removed; the rest are recorded as skipped.
func (s *Store) recheckAndDeleteRows(ctx context.Context, work []OrphanCandidate, res *OrphanResult) ([]OrphanCandidate, error) {
	var passed []OrphanCandidate
	var skipped []OrphanSkip
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		passed, skipped = nil, nil
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, mediarefs.LockKey); err != nil {
			return err
		}
		refs, err := mediarefs.Scan(ctx, tx)
		if err != nil {
			return err
		}
		for _, c := range work {
			why, err := s.recheck(ctx, tx, c, refs)
			if err != nil {
				return err
			}
			if why != "" {
				skipped = append(skipped, OrphanSkip{Key: c.Key, Why: why})
				continue
			}
			passed = append(passed, c)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	res.Skipped = append(res.Skipped, skipped...)
	return passed, nil
}

// recheck returns why c may no longer be deleted ("" = go ahead; a row is
// deleted here).
func (s *Store) recheck(ctx context.Context, tx pgx.Tx, c OrphanCandidate, refs map[string]bool) (string, error) {
	switch c.Kind {
	case KindRow:
		var status, stem string
		var old bool
		var job *string
		var deleted *bool
		err := tx.QueryRow(ctx, `SELECT m.status, m.stem, m.touched_at < now() - make_interval(secs => $2),
			j.status, j.original_deleted FROM media m LEFT JOIN media_jobs j ON j.media_id = m.id
			WHERE m.id = $1 FOR UPDATE OF m`, c.MediaID, s.orphanGrace.Seconds()).
			Scan(&status, &stem, &old, &job, &deleted)
		if errors.Is(err, pgx.ErrNoRows) {
			return "gone", nil
		}
		if err != nil {
			return "", err
		}
		r := orphanRow{status: status, jobStatus: job, originalDeleted: deleted}
		switch {
		case status == StatusProcessing || r.active() || (status == StatusFailed && r.retained()):
			return "processing", nil
		case refs[stem]:
			return "referenced", nil
		case !old:
			return "touched", nil
		}
		_, err = tx.Exec(ctx, `DELETE FROM media WHERE id = $1`, c.MediaID)
		return "", err
	case KindObject, KindLocal:
		if why, err := stemTaken(ctx, tx, c.stem); why != "" || err != nil {
			return why, err
		}
		if refs[c.stem] {
			return "referenced", nil
		}
	case KindLocalCopy:
		var ok bool
		err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media WHERE filename = $1 AND status = 'ready')`,
			c.Name).Scan(&ok)
		if err != nil || !ok {
			return "gone", err
		}
	case KindOriginal:
		var job *string
		var deleted *bool
		err := tx.QueryRow(ctx, `SELECT j.status, j.original_deleted FROM media m
			LEFT JOIN media_jobs j ON j.media_id = m.id WHERE m.stem = $1 LIMIT 1`, c.stem).Scan(&job, &deleted)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		r := orphanRow{jobStatus: job, originalDeleted: deleted}
		if r.active() || r.retained() {
			return "processing", nil
		}
	}
	return "", nil
}

func stemTaken(ctx context.Context, tx pgx.Tx, stem string) (string, error) {
	var taken bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM media WHERE stem = $1)`, stem).Scan(&taken); err != nil {
		return "", err
	}
	if taken {
		return "has a media row", nil
	}
	return "", nil
}

// removeCandidate deletes c's files: the local copy first (MigrateToBucket
// would upload it again for a ready row), then bucket objects, then the
// original. A file already gone counts as removed.
func (s *Store) removeCandidate(ctx context.Context, c OrphanCandidate, trigger string, res *OrphanResult) {
	var errs []error
	local := func(path string) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	object := func(key string) {
		if s.s3 == nil {
			return
		}
		if err := s.s3.remove(ctx, key); err != nil && !isNotFound(err) {
			errs = append(errs, err)
		}
	}
	switch c.Kind {
	case KindRow:
		names := append([]string{c.Name}, c.locals...)
		for _, n := range uniq(names) {
			local(s.localPath(n))
		}
		for _, k := range uniq(append([]string{c.Name}, c.objects...)) {
			object(k)
		}
		local(s.originalPath(c.stem))
	case KindObject:
		object(c.Name)
	case KindLocal, KindLocalCopy, KindTemp:
		local(s.localPath(c.Name))
	case KindOriginal:
		local(s.originalPath(c.Name))
	}
	log := s.log.With(slog.String("kind", c.Kind), slog.String("name", c.Name), slog.Int64("media_id", c.MediaID),
		slog.Int64("size_bytes", c.SizeBytes), slog.Duration("age", time.Duration(c.AgeSeconds)*time.Second),
		slog.String("trigger", trigger))
	if len(errs) > 0 {
		log.Warn("media: orphan delete failed", logging.Err(errors.Join(errs...)))
		res.Errors++
		if c.Kind != KindRow { // the row is gone; its object is an "object" next time
			return
		}
	} else {
		log.Info("media: orphan deleted")
	}
	res.Deleted.add(c.Kind)
	res.Bytes += c.SizeBytes
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0:0]
	for _, x := range xs {
		if x != "" && !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// orphanSettings loads media.orphans (zero value when absent).
func (s *Store) orphanSettings(ctx context.Context) (OrphanSettings, error) {
	var st OrphanSettings
	_, err := settings.Get(ctx, s.db, OrphanSettingsKey, &st)
	return st, err
}

// updateOrphanSettings applies fn to media.orphans. It must not run inside
// the cleanup transaction: settings.Put takes the shared references lock.
func (s *Store) updateOrphanSettings(ctx context.Context, fn func(*OrphanSettings)) error {
	s.orphanMu.Lock()
	defer s.orphanMu.Unlock()
	st, err := s.orphanSettings(ctx)
	if err != nil {
		return err
	}
	fn(&st)
	return settings.Put(ctx, s.db, OrphanSettingsKey, st)
}

// SetOrphanAuto turns the daily automatic cleanup on or off.
func (s *Store) SetOrphanAuto(ctx context.Context, on bool) error {
	return s.updateOrphanSettings(ctx, func(st *OrphanSettings) { st.Auto = on })
}

func (s *Store) recordRun(ctx context.Context, trigger string, res OrphanResult) {
	run := &OrphanRun{At: time.Now().UTC(), Trigger: trigger, Deleted: res.Deleted.total(), Bytes: res.Bytes,
		Errors: res.Errors}
	if err := s.updateOrphanSettings(context.WithoutCancel(ctx), func(st *OrphanSettings) { st.LastRun = run }); err != nil {
		s.log.Warn("media: record orphan cleanup", logging.Err(err))
	}
}

// maybeOrphanPass runs the daily automatic cleanup when it is turned on.
// Failures are logged; the janitor goes on.
func (s *Store) maybeOrphanPass(ctx context.Context) {
	if !s.OrphansEnabled() {
		return
	}
	st, err := s.orphanSettings(ctx)
	if err != nil {
		s.log.Warn("media: orphan settings", logging.Err(err))
		return
	}
	if !st.Auto || (st.LastAutoAt != nil && time.Since(*st.LastAutoAt) < autoEvery) {
		return
	}
	// Recorded first, so a failing or braking run is not retried every tick.
	now := time.Now().UTC()
	if err := s.updateOrphanSettings(ctx, func(st *OrphanSettings) { st.LastAutoAt = &now }); err != nil {
		s.log.Warn("media: orphan settings", logging.Err(err))
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	res, err := s.CleanOrphans(cctx, TriggerAuto, nil)
	if err != nil {
		if !errors.Is(err, ErrBusy) {
			s.log.Warn("media: orphan cleanup", logging.Err(err))
		}
		return
	}
	s.log.Info("media: orphan cleanup done", slog.Int("deleted", res.Deleted.total()), slog.Int64("bytes", res.Bytes),
		slog.Int("errors", res.Errors), slog.Int("remaining", res.Remaining), slog.String("reason", res.Reason))
}
