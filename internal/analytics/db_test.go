package analytics

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

func dbRecorder(t *testing.T, now *time.Time) (*Recorder, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	r := New(Deps{DB: pool, Log: quietLog(), Salt: "salt", SiteHost: "baoopn.com"})
	r.now = func() time.Time { return *now }
	return r, pool
}

func count(t *testing.T, pool *pgxpool.Pool, q string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestFlushAccumulates(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 15, 0, 0, time.UTC)
	r, pool := dbRecorder(t, &now)
	ctx := t.Context()
	post(r, `{"type":"pageview","path":"/blog","referrer":"https://news.ycombinator.com/"}`, uaChrome)
	if err := r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	post(r, `{"type":"pageview","path":"/blog","referrer":"https://news.ycombinator.com/"}`, uaFirefox+" x")
	post(r, `{"type":"pageview","path":"/blog","referrer":"https://news.ycombinator.com/"}`, uaChrome)
	if err := r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if v := count(t, pool, `SELECT views FROM page_view_stats WHERE path='/blog' AND browser='Chrome'`); v != 2 {
		t.Errorf("Chrome views = %d, want 2", v)
	}
	if v := count(t, pool, `SELECT count(*) FROM page_view_stats WHERE referrer = 'news.ycombinator.com'`); v != 2 {
		t.Error("referrer not stored")
	}
	// Visitors are deduplicated: two distinct visitors.
	if v := count(t, pool, `SELECT count(*) FROM page_visitors WHERE path='*'`); v != 2 {
		t.Errorf("site visitors = %d, want 2", v)
	}
	if v := count(t, pool, `SELECT count(*) FROM page_visitor_hours`); v != 2 {
		t.Errorf("hour visitors = %d, want 2", v)
	}
	// Re-flushing an already stored visitor row (new process, same day) is a no-op.
	r.days = append(r.days, dayVisitor{day: utcDay(now), path: "*", visitor: r.visitorHash(now, "192.0.2.1", uaChrome)})
	if err := r.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if v := count(t, pool, `SELECT count(*) FROM page_visitors WHERE path='*'`); v != 2 {
		t.Errorf("site visitors after re-flush = %d, want 2", v)
	}
}

func TestFlushRequeuesOnClosedPool(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	pool := testdb.New(t)
	closed, err := pgxpool.New(t.Context(), pool.Config().ConnString())
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	r := New(Deps{DB: closed, Log: quietLog(), Salt: "s"})
	r.now = func() time.Time { return now }
	post(r, `{"type":"pageview","path":"/"}`, uaChrome)
	if err := r.Flush(t.Context()); err == nil {
		t.Fatal("flush on a closed pool succeeded")
	}
	post(r, `{"type":"pageview","path":"/"}`, uaChrome)
	if len(r.pages) != 1 || len(r.days) != 2 || len(r.hours) != 1 {
		t.Fatalf("not re-queued: pages %d days %d hours %d", len(r.pages), len(r.days), len(r.hours))
	}
	for _, v := range r.pages {
		if v.views != 2 {
			t.Errorf("requeued views = %d, want 2", v.views)
		}
	}
	// The same data then flushes through a working pool.
	r.db = pool
	if err := r.Flush(t.Context()); err != nil {
		t.Fatal(err)
	}
	if v := count(t, pool, `SELECT sum(views)::bigint FROM page_view_stats`); v != 2 {
		t.Errorf("views = %d, want 2", v)
	}
}

func seedStats(t *testing.T, pool *pgxpool.Pool, rows ...[]any) {
	t.Helper()
	for _, a := range rows {
		_, err := pool.Exec(t.Context(), `INSERT INTO page_view_stats
			(bucket, path, device, browser, country, referrer, views, visible_ms) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, a...)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func seedHours(t *testing.T, pool *pgxpool.Pool, hour time.Time, visitors ...string) {
	t.Helper()
	for _, v := range visitors {
		if _, err := pool.Exec(t.Context(), `INSERT INTO page_visitor_hours (hour, visitor) VALUES ($1,$2)`, hour, v); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReportTotalsAndPrevious(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC)
	r, pool := dbRecorder(t, &now)
	h := now.Truncate(time.Hour)
	seedStats(t, pool,
		[]any{h, "/", "desktop", "Chrome", "VN", "google.com", 3, 9000},
		[]any{h.Add(-2 * time.Hour), "/blog/x", "mobile", "Safari", "", "", 1, 1000},
		[]any{h.Add(-30 * time.Hour), "/", "desktop", "Chrome", "VN", "", 4, 0}, // previous 24h
		[]any{h.Add(-50 * time.Hour), "/", "desktop", "Chrome", "VN", "", 7, 0}, // outside both
	)
	seedHours(t, pool, h, "a", "b")
	seedHours(t, pool, h.Add(-2*time.Hour), "a")
	seedHours(t, pool, h.Add(-30*time.Hour), "c")
	for _, d := range [][]any{{utcDay(now), "/", "a"}, {utcDay(now), "/", "b"}, {utcDay(now), "/blog/x", "a"}} {
		if _, err := pool.Exec(t.Context(), `INSERT INTO page_visitors (day, path, visitor) VALUES ($1,$2,$3)`, d...); err != nil {
			t.Fatal(err)
		}
	}
	r.markLive("a", now)

	rep, err := r.Report(t.Context(), "24h", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	tt := rep.Totals
	if tt.Views != 4 || tt.Visitors != 2 || tt.AvgVisibleMS != 2500 || tt.ViewsPrev != 4 || tt.VisitorsPrev != 1 || tt.LiveNow != 1 {
		t.Errorf("totals = %+v", tt)
	}
	if len(rep.Timeseries) != 24 {
		t.Fatalf("timeseries = %d points", len(rep.Timeseries))
	}
	if last := rep.Timeseries[23]; !last.T.Equal(h) || last.Views != 3 || last.Visitors != 2 {
		t.Errorf("last point = %+v", last)
	}
	if len(rep.Paths) != 2 || rep.Paths[0].Key != "/" || *rep.Paths[0].Visitors != 2 || *rep.Paths[0].AvgVisibleMS != 3000 {
		t.Errorf("paths = %+v", rep.Paths)
	}
	if len(rep.Referrers) != 2 || rep.Referrers[0].Key != "google.com" || rep.Referrers[1].Key != "" {
		t.Errorf("referrers = %+v", rep.Referrers)
	}
	if len(rep.Countries) != 2 || len(rep.Devices) != 2 || len(rep.Browsers) != 2 {
		t.Errorf("dims: %+v %+v %+v", rep.Countries, rep.Devices, rep.Browsers)
	}
	if _, err := r.Report(t.Context(), "1y", time.UTC); err != ErrBadRange {
		t.Errorf("bad range err = %v", err)
	}
	top, err := r.TopPaths(t.Context(), now.Add(-7*24*time.Hour), 1)
	if err != nil || len(top) != 1 || top[0].Key != "/" || top[0].Views != 14 {
		t.Errorf("TopPaths = %+v, %v", top, err)
	}
}

// 23:30 UTC on Nov 5 is 06:30 on Nov 6 in Asia/Ho_Chi_Minh (UTC+7).
func TestReportDayBucketTZ(t *testing.T) {
	now := time.Date(2026, 11, 6, 3, 0, 0, 0, time.UTC)
	r, pool := dbRecorder(t, &now)
	hcm := mustZone(t, "Asia/Ho_Chi_Minh")
	late := time.Date(2026, 11, 5, 23, 0, 0, 0, time.UTC)  // bucket of 23:30 UTC
	early := time.Date(2026, 11, 5, 16, 0, 0, 0, time.UTC) // 23:00 Nov 5 local
	seedStats(t, pool,
		[]any{late, "/", "desktop", "Chrome", "", "", 5, 0},
		[]any{early, "/", "desktop", "Chrome", "", "", 2, 0},
	)
	seedHours(t, pool, late, "a", "b")
	seedHours(t, pool, early, "a")

	rep, err := r.Report(t.Context(), "30d", hcm)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Bucket != "day" || rep.TimeZone != "Asia/Ho_Chi_Minh" || len(rep.Timeseries) != 30 {
		t.Fatalf("report = %s %s %d", rep.Bucket, rep.TimeZone, len(rep.Timeseries))
	}
	today, yesterday := rep.Timeseries[29], rep.Timeseries[28]
	if got := today.T.In(hcm).Format(time.DateOnly); got != "2026-11-06" {
		t.Errorf("last bucket day = %s", got)
	}
	if !today.T.Equal(time.Date(2026, 11, 5, 17, 0, 0, 0, time.UTC)) {
		t.Errorf("last bucket t = %v, want local midnight in UTC", today.T)
	}
	if today.Views != 5 || today.Visitors != 2 || yesterday.Views != 2 || yesterday.Visitors != 1 {
		t.Errorf("today %+v yesterday %+v", today, yesterday)
	}
}

func TestPrune(t *testing.T) {
	now := time.Now()
	r, pool := dbRecorder(t, &now)
	old := now.Add(-401 * 24 * time.Hour).UTC().Truncate(time.Hour)
	recent := now.Add(-time.Hour).UTC().Truncate(time.Hour)
	seedStats(t, pool, []any{old, "/", "d", "b", "", "", 1, 0}, []any{recent, "/", "d", "b", "", "", 1, 0})
	seedHours(t, pool, old, "a")
	seedHours(t, pool, recent, "a")
	for _, d := range []time.Time{utcDay(old), utcDay(recent)} {
		if _, err := pool.Exec(t.Context(), `INSERT INTO page_visitors (day, path, visitor) VALUES ($1,'*','a')`, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Prune(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`SELECT count(*) FROM page_view_stats`, `SELECT count(*) FROM page_visitor_hours`,
		`SELECT count(*) FROM page_visitors`} {
		if n := count(t, pool, q); n != 1 {
			t.Errorf("%s = %d, want 1", q, n)
		}
	}
}
