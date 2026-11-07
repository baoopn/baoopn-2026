// Package analytics is cookieless page-view analytics for the public site.
//
// The site sends a small beacon on each page view and when a page is hidden
// (time on page). Nothing identifies a person: no cookies or storage, IPs are
// never stored, and unique visitors come from a salted hash of IP + user
// agent that changes every UTC day. Views are aggregated in memory per UTC
// hour and dimension and flushed to Postgres every minute.
package analytics

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/logging"
)

const (
	maxEventBytes   = 2 << 10
	maxVisibleMS    = 30 * 60 * 1000 // cap one report at 30 minutes
	livePeriod      = 5 * time.Minute
	flushInterval   = time.Minute
	pruneDelay      = time.Minute
	pruneInterval   = 24 * time.Hour
	retentionDays   = 400
	maxSeen         = 200_000
	maxPendingRows  = 50_000
	finalFlushLimit = 5 * time.Second
)

// Deps are the Recorder's dependencies.
type Deps struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
	// Salt keys the visitor hash; see DeriveSalt.
	Salt string
	// SiteHost is the public site's host (from PUBLIC_URL); referrers from it
	// count as direct. The proxied request Host is the API's internal host.
	SiteHost string
}

type pageKey struct {
	bucket                                   time.Time
	path, device, browser, country, referrer string
}

type pageVal struct{ views, visibleMS int64 }

// dayVisitor is a page_visitors row; path "*" is site-wide.
type dayVisitor struct {
	day           time.Time
	path, visitor string
}

// hourVisitor is a page_visitor_hours row.
type hourVisitor struct {
	hour    time.Time
	visitor string
}

// Recorder collects page views and answers dashboard queries.
type Recorder struct {
	db       *pgxpool.Pool
	log      *slog.Logger
	salt     string
	siteHost string
	now      func() time.Time

	flushMu sync.Mutex // one flush at a time

	mu      sync.Mutex
	pages   map[pageKey]*pageVal
	days    []dayVisitor
	hours   []hourVisitor
	seenDay string
	seen    map[string]struct{}
	live    map[string]time.Time
}

// New creates a Recorder.
func New(d Deps) *Recorder {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	salt := d.Salt
	if salt == "" {
		salt = DeriveSalt("")
		log.Warn("analytics: SECRETS_KEY is not set; unique visitors reset on restart")
	}
	return &Recorder{db: d.DB, log: log, salt: salt, siteHost: d.SiteHost, now: time.Now,
		pages: map[pageKey]*pageVal{}, seen: map[string]struct{}{}, live: map[string]time.Time{}}
}

// Run flushes every minute, prunes old rows daily (first run one minute after
// start) and does a final flush on a fresh context when ctx is done.
func (r *Recorder) Run(ctx context.Context) {
	flush := time.NewTicker(flushInterval)
	defer flush.Stop()
	prune := time.NewTimer(pruneDelay)
	defer prune.Stop()
	for {
		select {
		case <-ctx.Done():
			fctx, cancel := context.WithTimeout(context.Background(), finalFlushLimit)
			if err := r.Flush(fctx); err != nil {
				r.log.Warn("analytics: final flush failed", logging.Err(err))
			}
			cancel()
			return
		case <-flush.C:
			if err := r.Flush(ctx); err != nil && ctx.Err() == nil {
				r.log.Warn("analytics: flush failed", logging.Err(err))
			}
		case <-prune.C:
			if err := r.Prune(ctx); err != nil && ctx.Err() == nil {
				r.log.Warn("analytics: prune failed", logging.Err(err))
			}
			prune.Reset(pruneInterval)
		}
	}
}

// LiveNow counts visitors seen in the last five minutes.
func (r *Recorder) LiveNow() int {
	cutoff := r.now().Add(-livePeriod)
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, t := range r.live {
		if t.After(cutoff) {
			n++
		}
	}
	return n
}

func (r *Recorder) observePage(k pageKey, views, visibleMS int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.pages[k]
	if v == nil {
		v = &pageVal{}
		r.pages[k] = v
	}
	v.views += views
	v.visibleMS += visibleMS
}

// Flush writes pending aggregates and visitor rows to Postgres, re-queueing
// them on error, and drops expired live entries.
func (r *Recorder) Flush(ctx context.Context) error {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()

	r.mu.Lock()
	pages, days, hours := r.pages, r.days, r.hours
	r.pages, r.days, r.hours = map[pageKey]*pageVal{}, nil, nil
	cutoff := r.now().Add(-livePeriod)
	for v, t := range r.live {
		if !t.After(cutoff) {
			delete(r.live, v)
		}
	}
	r.mu.Unlock()

	var errs []error
	if err := r.flushPages(ctx, pages); err != nil {
		r.requeuePages(pages)
		errs = append(errs, err)
	}
	if err := r.flushVisitors(ctx, days, hours); err != nil {
		r.requeueVisitors(days, hours)
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (r *Recorder) flushPages(ctx context.Context, pending map[pageKey]*pageVal) error {
	if len(pending) == 0 {
		return nil
	}
	if r.db == nil {
		return errors.New("analytics: no database")
	}
	batch := &pgx.Batch{}
	for k, v := range pending {
		batch.Queue(`
			INSERT INTO page_view_stats AS s (bucket, path, device, browser, country, referrer, views, visible_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (bucket, path, device, browser, country, referrer) DO UPDATE SET
				views = s.views + EXCLUDED.views, visible_ms = s.visible_ms + EXCLUDED.visible_ms`,
			k.bucket, k.path, k.device, k.browser, k.country, k.referrer, v.views, v.visibleMS)
	}
	return r.db.SendBatch(ctx, batch).Close()
}

func (r *Recorder) requeuePages(pending map[pageKey]*pageVal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k, v := range pending {
		if cur := r.pages[k]; cur != nil {
			cur.views += v.views
			cur.visibleMS += v.visibleMS
		} else {
			r.pages[k] = v
		}
	}
}

func (r *Recorder) flushVisitors(ctx context.Context, days []dayVisitor, hours []hourVisitor) error {
	if len(days) == 0 && len(hours) == 0 {
		return nil
	}
	if r.db == nil {
		return errors.New("analytics: no database")
	}
	dd := make([]time.Time, len(days))
	dp := make([]string, len(days))
	dv := make([]string, len(days))
	for i, d := range days {
		dd[i], dp[i], dv[i] = d.day, d.path, d.visitor
	}
	hh := make([]time.Time, len(hours))
	hv := make([]string, len(hours))
	for i, h := range hours {
		hh[i], hv[i] = h.hour, h.visitor
	}
	batch := &pgx.Batch{}
	batch.Queue(`INSERT INTO page_visitors (day, path, visitor)
		SELECT * FROM unnest($1::date[], $2::text[], $3::text[]) ON CONFLICT DO NOTHING`, dd, dp, dv)
	batch.Queue(`INSERT INTO page_visitor_hours (hour, visitor)
		SELECT * FROM unnest($1::timestamptz[], $2::text[]) ON CONFLICT DO NOTHING`, hh, hv)
	return r.db.SendBatch(ctx, batch).Close()
}

// requeueVisitors puts failed rows back in front of newer ones, keeping at
// most maxPendingRows in total (the oldest are dropped).
func (r *Recorder) requeueVisitors(days []dayVisitor, hours []hourVisitor) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.days = append(days, r.days...)
	r.hours = append(hours, r.hours...)
	if over := len(r.days) + len(r.hours) - maxPendingRows; over > 0 {
		cut := min(over, len(r.days))
		r.days = r.days[cut:]
		over -= cut
		r.hours = r.hours[min(over, len(r.hours)):]
	}
}
