package analytics

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Report is the admin analytics response (AnalyticsReport in types.ts).
type Report struct {
	Range      string      `json:"range"`
	Bucket     string      `json:"bucket"`
	TimeZone   string      `json:"time_zone"`
	Totals     Totals      `json:"totals"`
	Timeseries []Point     `json:"timeseries"`
	Paths      []Breakdown `json:"paths"`
	Referrers  []Breakdown `json:"referrers"`
	Devices    []Breakdown `json:"devices"`
	Browsers   []Breakdown `json:"browsers"`
	Countries  []Breakdown `json:"countries"`
}

// Totals summarises the range and the same-length previous period.
type Totals struct {
	Views        int64 `json:"views"`
	Visitors     int64 `json:"visitors"`
	AvgVisibleMS int64 `json:"avg_visible_ms"`
	LiveNow      int   `json:"live_now"`
	ViewsPrev    int64 `json:"views_prev"`
	VisitorsPrev int64 `json:"visitors_prev"`
}

// Point is one time-series bucket (its start, UTC).
type Point struct {
	T        time.Time `json:"t"`
	Views    int64     `json:"views"`
	Visitors int64     `json:"visitors"`
}

// Breakdown is the view count for one value of a dimension. An empty Key
// means direct visits (referrers) or unknown (countries).
type Breakdown struct {
	Key          string `json:"key"`
	Views        int64  `json:"views"`
	Visitors     *int64 `json:"visitors,omitempty"`
	AvgVisibleMS *int64 `json:"avg_visible_ms,omitempty"`
}

// ErrBadRange is returned for unknown ranges.
var ErrBadRange = errors.New("range must be one of 24h, 7d, 30d, 90d")

// ErrBadZone is returned for unknown time zones.
var ErrBadZone = errors.New("tz must be an IANA time zone name, e.g. Asia/Ho_Chi_Minh")

// LoadZone validates an IANA zone name ("" means UTC).
func LoadZone(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	if len(name) > 64 || name == "Local" {
		return nil, ErrBadZone
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, ErrBadZone
	}
	return loc, nil
}

// rangeSpec is a report window [since, until) and its previous period
// [prevSince, since).
type rangeSpec struct {
	name                    string
	since, until, prevSince time.Time
	daily                   bool // buckets are calendar days in loc (else UTC hours)
	bucket                  string
	loc                     *time.Location
}

// parseRange computes the window. Hourly ranges use absolute UTC hours.
// Daily ranges use calendar days in loc: midnights come from time.Date and
// AddDate in that zone (never from adding 24h), so 23/25-hour days around
// DST transitions are bucketed correctly.
func parseRange(s string, now time.Time, loc *time.Location) (rangeSpec, bool) {
	hour := now.UTC().Truncate(time.Hour)
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	hourly := func(name string, n int) rangeSpec {
		since := hour.Add(-time.Duration(n-1) * time.Hour)
		return rangeSpec{name: name, since: since, until: hour.Add(time.Hour),
			prevSince: since.Add(-time.Duration(n) * time.Hour), bucket: "hour", loc: loc}
	}
	daily := func(name string, n int) rangeSpec {
		since := today.AddDate(0, 0, -(n - 1))
		return rangeSpec{name: name, since: since, until: today.AddDate(0, 0, 1),
			prevSince: since.AddDate(0, 0, -n), daily: true, bucket: "day", loc: loc}
	}
	switch s {
	case "", "24h":
		return hourly("24h", 24), true
	case "7d":
		return hourly("7d", 7*24), true
	case "30d":
		return daily("30d", 30), true
	case "90d":
		return daily("90d", 90), true
	}
	return rangeSpec{}, false
}

// next advances one bucket.
func (s rangeSpec) next(t time.Time) time.Time {
	if !s.daily {
		return t.Add(time.Hour)
	}
	return t.AddDate(0, 0, 1)
}

// seriesKey identifies a bucket: the UTC hour, or the local calendar date.
func (s rangeSpec) seriesKey(t time.Time) string {
	if !s.daily {
		return t.UTC().Format(time.RFC3339)
	}
	return t.In(s.loc).Format(time.DateOnly)
}

// fillSeries returns every bucket in the window, oldest first, zero-filled.
func fillSeries(s rangeSpec, views, visitors map[string]int64) []Point {
	out := []Point{}
	for b := s.since; b.Before(s.until); b = s.next(b) {
		k := s.seriesKey(b)
		out = append(out, Point{T: b.UTC(), Views: views[k], Visitors: visitors[k]})
	}
	return out
}

// Report builds the analytics report for a range ("24h", "7d", "30d", "90d");
// daily buckets align to calendar days in loc (nil means UTC).
func (r *Recorder) Report(ctx context.Context, rangeName string, loc *time.Location) (Report, error) {
	if loc == nil {
		loc = time.UTC
	}
	spec, ok := parseRange(rangeName, r.now(), loc)
	if !ok {
		return Report{}, ErrBadRange
	}
	if err := r.Flush(ctx); err != nil {
		r.log.WarnContext(ctx, "analytics: flush before report failed", logging.Err(err))
	}
	rep := Report{Range: spec.name, Bucket: spec.bucket, TimeZone: loc.String()}
	var err error
	var visible int64
	if rep.Totals.Views, visible, rep.Totals.Visitors, err = r.totals(ctx, spec.since, spec.until); err != nil {
		return Report{}, err
	}
	if rep.Totals.Views > 0 {
		rep.Totals.AvgVisibleMS = visible / rep.Totals.Views
	}
	if rep.Totals.ViewsPrev, _, rep.Totals.VisitorsPrev, err = r.totals(ctx, spec.prevSince, spec.since); err != nil {
		return Report{}, err
	}
	rep.Totals.LiveNow = r.LiveNow()
	if rep.Timeseries, err = r.series(ctx, spec); err != nil {
		return Report{}, err
	}
	if rep.Paths, err = r.paths(ctx, spec.since, spec.until, 10); err != nil {
		return Report{}, err
	}
	for _, d := range []struct {
		col string
		n   int
		dst *[]Breakdown
	}{{"referrer", 10, &rep.Referrers}, {"device", 10, &rep.Devices},
		{"browser", 10, &rep.Browsers}, {"country", 15, &rep.Countries}} {
		if *d.dst, err = r.dimension(ctx, d.col, spec.since, spec.until, d.n); err != nil {
			return Report{}, err
		}
	}
	return rep, nil
}

// TopPaths returns the most viewed paths since the given time.
func (r *Recorder) TopPaths(ctx context.Context, since time.Time, n int) ([]Breakdown, error) {
	return r.paths(ctx, since, r.now().Add(time.Hour), n)
}

func (r *Recorder) totals(ctx context.Context, since, until time.Time) (views, visible, visitors int64, err error) {
	err = r.db.QueryRow(ctx, `
		SELECT coalesce(sum(views),0)::bigint, coalesce(sum(visible_ms),0)::bigint
		FROM page_view_stats WHERE bucket >= $1 AND bucket < $2`, since, until).Scan(&views, &visible)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("analytics totals: %w", err)
	}
	err = r.db.QueryRow(ctx, `
		SELECT count(DISTINCT visitor) FROM page_visitor_hours WHERE hour >= $1 AND hour < $2`,
		since, until).Scan(&visitors)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("analytics visitors: %w", err)
	}
	return views, visible, visitors, nil
}

// series loads views and visitors per bucket. Day buckets are grouped by
// the calendar date in the report's zone.
func (r *Recorder) series(ctx context.Context, s rangeSpec) ([]Point, error) {
	viewsQ := `SELECT bucket, sum(views)::bigint FROM page_view_stats
		WHERE bucket >= $1 AND bucket < $2 GROUP BY 1`
	visitorsQ := `SELECT hour, count(DISTINCT visitor) FROM page_visitor_hours
		WHERE hour >= $1 AND hour < $2 GROUP BY 1`
	args := []any{s.since, s.until}
	if s.daily {
		viewsQ = `SELECT (bucket AT TIME ZONE $3)::date, sum(views)::bigint FROM page_view_stats
			WHERE bucket >= $1 AND bucket < $2 GROUP BY 1`
		visitorsQ = `SELECT (hour AT TIME ZONE $3)::date, count(DISTINCT visitor) FROM page_visitor_hours
			WHERE hour >= $1 AND hour < $2 GROUP BY 1`
		args = append(args, s.loc.String())
	}
	views, err := r.keyedCounts(ctx, s, viewsQ, args)
	if err != nil {
		return nil, err
	}
	visitors, err := r.keyedCounts(ctx, s, visitorsQ, args)
	if err != nil {
		return nil, err
	}
	return fillSeries(s, views, visitors), nil
}

func (r *Recorder) keyedCounts(ctx context.Context, s rangeSpec, q string, args []any) (map[string]int64, error) {
	rows, err := r.db.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("analytics series: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var t time.Time
		var n int64
		if err := rows.Scan(&t, &n); err != nil {
			return nil, err
		}
		if s.daily {
			out[t.Format(time.DateOnly)] = n // a date scans as UTC midnight
		} else {
			out[s.seriesKey(t)] = n
		}
	}
	return out, rows.Err()
}

func (r *Recorder) paths(ctx context.Context, since, until time.Time, n int) ([]Breakdown, error) {
	rows, err := r.db.Query(ctx, `
		SELECT path, sum(views)::bigint, sum(visible_ms)::bigint FROM page_view_stats
		WHERE bucket >= $1 AND bucket < $2
		GROUP BY path HAVING sum(views) > 0 ORDER BY 2 DESC, 1 LIMIT $3`, since, until, n)
	if err != nil {
		return nil, fmt.Errorf("analytics paths: %w", err)
	}
	out := []Breakdown{}
	var keys []string
	for rows.Next() {
		var b Breakdown
		var visible int64
		if err := rows.Scan(&b.Key, &b.Views, &visible); err != nil {
			rows.Close()
			return nil, err
		}
		avg := visible / b.Views
		b.AvgVisibleMS = &avg
		out = append(out, b)
		keys = append(keys, b.Key)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return out, nil
	}
	// Visitors per path come from the UTC days the window spans.
	first, last := utcDay(since), utcDay(until.Add(-time.Nanosecond))
	vrows, err := r.db.Query(ctx, `
		SELECT path, count(DISTINCT visitor) FROM page_visitors
		WHERE day >= $1::date AND day <= $2::date AND path = ANY($3) GROUP BY path`, first, last, keys)
	if err != nil {
		return nil, fmt.Errorf("analytics path visitors: %w", err)
	}
	defer vrows.Close()
	counts := map[string]int64{}
	for vrows.Next() {
		var p string
		var c int64
		if err := vrows.Scan(&p, &c); err != nil {
			return nil, err
		}
		counts[p] = c
	}
	for i := range out {
		c := counts[out[i].Key]
		out[i].Visitors = &c
	}
	return out, vrows.Err()
}

// dimension columns allowed in breakdown queries (never user input).
var dimensionCols = map[string]bool{"referrer": true, "device": true, "browser": true, "country": true}

func (r *Recorder) dimension(ctx context.Context, col string, since, until time.Time, n int) ([]Breakdown, error) {
	if !dimensionCols[col] {
		return nil, fmt.Errorf("analytics: unknown dimension %q", col)
	}
	rows, err := r.db.Query(ctx, `SELECT `+col+`, sum(views)::bigint FROM page_view_stats
		WHERE bucket >= $1 AND bucket < $2 AND views > 0
		GROUP BY 1 ORDER BY 2 DESC, 1 LIMIT $3`, since, until, n)
	if err != nil {
		return nil, fmt.Errorf("analytics %s: %w", col, err)
	}
	defer rows.Close()
	out := []Breakdown{}
	for rows.Next() {
		var b Breakdown
		if err := rows.Scan(&b.Key, &b.Views); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
