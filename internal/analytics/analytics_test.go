package analytics

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

const (
	uaChrome   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36"
	uaSafari   = "Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1"
	uaEdge     = uaChrome + " Edg/141.0.0.0"
	uaFirefox  = "Mozilla/5.0 (X11; Linux x86_64; rv:145.0) Gecko/20100101 Firefox/145.0"
	uaOpera    = uaChrome + " OPR/120.0.0.0"
	uaBot      = "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	uaHeadless = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) HeadlessChrome/141.0.0.0 Safari/537.36"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// testRecorder has no database and a settable clock.
func testRecorder(now *time.Time) *Recorder {
	r := New(Deps{Log: quietLog(), Salt: "salt", SiteHost: "baoopn.com"})
	r.now = func() time.Time { return *now }
	return r
}

func TestBrowserAndDevice(t *testing.T) {
	for ua, want := range map[string]string{uaChrome: "Chrome", uaSafari: "Safari", uaEdge: "Edge",
		uaFirefox: "Firefox", uaOpera: "Opera", "SomethingElse/1.0": "Other"} {
		if got := browserOf(ua); got != want {
			t.Errorf("browserOf(%q) = %q, want %q", ua, got, want)
		}
	}
	cases := []struct {
		width int
		ua    string
		want  string
	}{{390, uaChrome, "mobile"}, {800, uaChrome, "tablet"}, {1440, uaChrome, "desktop"},
		{0, uaSafari, "mobile"}, {0, uaChrome, "desktop"}}
	for _, c := range cases {
		if got := deviceOf(c.width, c.ua); got != c.want {
			t.Errorf("deviceOf(%d) = %q, want %q", c.width, got, c.want)
		}
	}
}

func TestBotUA(t *testing.T) {
	for ua, bot := range map[string]bool{uaBot: true, uaHeadless: true, "curl/8.0": true,
		"python-requests/2.32": true, uaChrome: false, uaSafari: false} {
		if got := botUA.MatchString(ua); got != bot {
			t.Errorf("botUA(%q) = %v, want %v", ua, got, bot)
		}
	}
}

func TestReferrerHost(t *testing.T) {
	for ref, want := range map[string]string{
		"":                                  "",
		"https://www.google.com/search?q=x": "google.com",
		"https://t.co/abc":                  "t.co",
		"https://baoopn.com/blog":           "", // own site
		"https://WWW.Baoopn.com/x":          "",
		"android-app://com.slack":           "",
		"not a url":                         "",
		"https://" + strings.Repeat("a", 120) + ".com/": "",
	} {
		if got := referrerHost(ref, "baoopn.com"); got != want {
			t.Errorf("referrerHost(%q) = %q, want %q", ref, got, want)
		}
	}
	if got := HostOf("https://baoopn.com/"); got != "baoopn.com" {
		t.Errorf("HostOf = %q", got)
	}
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{"": "/", "/": "/", "/?utm=x#y": "/", "/Blog/": "/blog",
		"/projects/x//": "/projects/x", "relative": "/", "/" + strings.Repeat("a", 150): "/" + strings.Repeat("a", 99)} {
		if got := cleanPath(in); got != want {
			t.Errorf("cleanPath(%q) = %q, want %q", in, got, want)
		}
	}
	for p, ex := range map[string]bool{"/admin": true, "/admin/inbox": true, "/api/v1": true,
		"/media/x.png": true, "/administrator": true, "/blog": false, "/": false, "/about": false,
		"/blog/my-post-1": false, "/projects/spotify-tracker": false, "/blog/a/b": true, "/wp-login.php": true,
		"/blog/" + strings.Repeat("x", 120): true} {
		if excludedPath(p) != ex {
			t.Errorf("excludedPath(%q) = %v", p, !ex)
		}
	}
}

func TestVisitorHash(t *testing.T) {
	now := time.Date(2026, 11, 6, 1, 0, 0, 0, time.UTC)
	r := testRecorder(&now)
	a := r.visitorHash(now, "1.2.3.4", uaChrome)
	if !regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(a) {
		t.Fatalf("hash %q is not 24 hex chars", a)
	}
	if b := r.visitorHash(now.Add(22*time.Hour), "1.2.3.4", uaChrome); b != a {
		t.Error("hash changed within the same UTC day")
	}
	if c := r.visitorHash(now.Add(23*time.Hour), "1.2.3.4", uaChrome); c == a {
		t.Error("hash did not change across UTC days")
	}
	if d := r.visitorHash(now, "1.2.3.5", uaChrome); d == a {
		t.Error("hash ignores the IP")
	}
}

func TestDeriveSalt(t *testing.T) {
	a, b := DeriveSalt("k1"), DeriveSalt("k1")
	if a != b || len(a) != 64 {
		t.Fatalf("DeriveSalt not deterministic: %q %q", a, b)
	}
	if DeriveSalt("k2") == a {
		t.Error("different keys gave the same salt")
	}
	if DeriveSalt("") == DeriveSalt("") {
		t.Error("empty key should give a random salt")
	}
}

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := LoadZone(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestParseRange(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 42, 0, 0, time.UTC)
	for _, c := range []struct {
		name          string
		since, until  time.Time
		prev          time.Time
		bucket        string
		buckets       int
		wantOK        bool
		zone          string
		firstLocalDay string
	}{
		{name: "24h", since: time.Date(2026, 11, 5, 11, 0, 0, 0, time.UTC), until: time.Date(2026, 11, 6, 11, 0, 0, 0, time.UTC),
			prev: time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC), bucket: "hour", buckets: 24, wantOK: true, zone: "UTC"},
		{name: "7d", since: time.Date(2026, 10, 30, 11, 0, 0, 0, time.UTC), until: time.Date(2026, 11, 6, 11, 0, 0, 0, time.UTC),
			prev: time.Date(2026, 10, 23, 11, 0, 0, 0, time.UTC), bucket: "hour", buckets: 168, wantOK: true, zone: "America/New_York"},
		{name: "30d", bucket: "day", buckets: 30, wantOK: true, zone: "America/New_York", firstLocalDay: "2026-10-08"},
		{name: "90d", bucket: "day", buckets: 90, wantOK: true, zone: "Asia/Kolkata", firstLocalDay: "2026-08-09"},
		{name: "1y"},
	} {
		zone := c.zone
		if zone == "" {
			zone = "UTC"
		}
		loc := mustZone(t, zone)
		s, ok := parseRange(c.name, now, loc)
		if ok != c.wantOK {
			t.Fatalf("%s: ok = %v", c.name, ok)
		}
		if !ok {
			continue
		}
		if s.bucket != c.bucket {
			t.Errorf("%s: bucket %q", c.name, s.bucket)
		}
		if !c.since.IsZero() && (!s.since.Equal(c.since) || !s.until.Equal(c.until) || !s.prevSince.Equal(c.prev)) {
			t.Errorf("%s: window [%v, %v) prev %v", c.name, s.since, s.until, s.prevSince)
		}
		pts := fillSeries(s, nil, nil)
		if len(pts) != c.buckets {
			t.Errorf("%s: %d buckets, want %d", c.name, len(pts), c.buckets)
		}
		if c.firstLocalDay != "" {
			if got := s.since.In(loc).Format(time.DateOnly); got != c.firstLocalDay {
				t.Errorf("%s: first day %s, want %s", c.name, got, c.firstLocalDay)
			}
			if h, m := s.since.In(loc).Hour(), s.since.In(loc).Minute(); h != 0 || m != 0 {
				t.Errorf("%s: since is not local midnight: %v", c.name, s.since.In(loc))
			}
		}
	}
}

// America/New_York leaves DST on 2026-11-01: that day lasts 25 hours, and
// every bucket must still start at local midnight.
func TestParseRangeDST(t *testing.T) {
	ny := mustZone(t, "America/New_York")
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	s, _ := parseRange("30d", now, ny)
	pts := fillSeries(s, nil, nil)
	for i, p := range pts {
		l := p.T.In(ny)
		if l.Hour() != 0 || l.Minute() != 0 {
			t.Fatalf("bucket %d starts at %v", i, l)
		}
		if i > 0 {
			gap := p.T.Sub(pts[i-1].T)
			day := pts[i-1].T.In(ny).Format(time.DateOnly)
			want := 24 * time.Hour
			if day == "2026-11-01" {
				want = 25 * time.Hour
			}
			if gap != want {
				t.Errorf("day %s lasts %v, want %v", day, gap, want)
			}
		}
	}
	// The previous period also starts at local midnight, before the DST change.
	if l := s.prevSince.In(ny); l.Hour() != 0 || l.Format(time.DateOnly) != "2026-09-08" {
		t.Errorf("prevSince = %v", l)
	}
	// The window ends at the next local midnight.
	if l := s.until.In(ny); l.Format(time.DateOnly) != "2026-11-07" || l.Hour() != 0 {
		t.Errorf("until = %v", l)
	}
	// Asia/Kolkata midnights fall on half hours in UTC.
	in := mustZone(t, "Asia/Kolkata")
	s, _ = parseRange("30d", now, in)
	if s.since.UTC().Minute() != 30 {
		t.Errorf("Kolkata since = %v, want a :30 UTC time", s.since.UTC())
	}
}

func TestFillSeries(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	s, _ := parseRange("24h", now, time.UTC)
	k := s.seriesKey(now)
	pts := fillSeries(s, map[string]int64{k: 5}, map[string]int64{k: 2})
	last := pts[len(pts)-1]
	if !last.T.Equal(now) || last.Views != 5 || last.Visitors != 2 {
		t.Errorf("last point = %+v", last)
	}
	if pts[0].Views != 0 {
		t.Errorf("gap not zero-filled: %+v", pts[0])
	}
	d, _ := parseRange("30d", now, time.UTC)
	dp := fillSeries(d, map[string]int64{"2026-11-06": 3}, nil)
	if dp[len(dp)-1].Views != 3 {
		t.Errorf("day series last = %+v", dp[len(dp)-1])
	}
}

func TestLoadZone(t *testing.T) {
	for name, ok := range map[string]bool{"": true, "UTC": true, "Asia/Ho_Chi_Minh": true,
		"Local": false, "Mars/Base": false, strings.Repeat("a", 70): false} {
		if _, err := LoadZone(name); (err == nil) != ok {
			t.Errorf("LoadZone(%q) err = %v", name, err)
		}
	}
}

func post(r *Recorder, body, ua string) int {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	w := httptest.NewRecorder()
	r.EventsHandler(w, req)
	return w.Code
}

func TestEventsHandler(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	r := testRecorder(&now)
	for _, c := range []struct {
		body, ua string
		want     int
	}{
		{`{"type":"pageview","path":"/blog"}`, uaChrome, 202},
		{`{"type":"engagement","path":"/blog","visible_ms":5000,"extra":1}`, uaChrome, 202},
		{`{"type":"pageview","path":"/"}`, uaBot, 202},
		{`{"type":"pageview","path":"/admin"}`, uaChrome, 202},
		{`{"type":"pageview","path":"/"}`, "", 202},
		{`{"type":"click","path":"/"}`, uaChrome, 400},
		{`not json`, uaChrome, 400},
		{`{"type":"pageview","path":"/` + strings.Repeat("a", 3000) + `"}`, uaChrome, 413},
	} {
		if got := post(r, c.body, c.ua); got != c.want {
			t.Errorf("POST %.40s (ua %.10q) = %d, want %d", c.body, c.ua, got, c.want)
		}
	}
	// Only the Chrome /blog pageview and its engagement were recorded (one key:
	// the direct view has no referrer either).
	if len(r.pages) != 1 {
		t.Fatalf("pages = %d, want 1: %+v", len(r.pages), r.pages)
	}
	var views, visible int64
	for k, v := range r.pages {
		if k.path != "/blog" {
			t.Errorf("unexpected path %q", k.path)
		}
		views += v.views
		visible += v.visibleMS
	}
	if views != 1 || visible != 5000 {
		t.Errorf("views %d visible %d", views, visible)
	}
	if len(r.days) != 2 || len(r.hours) != 1 {
		t.Errorf("visitor rows: days %d hours %d", len(r.days), len(r.hours))
	}
	// A second view by the same visitor queues nothing new.
	post(r, `{"type":"pageview","path":"/blog"}`, uaChrome)
	if len(r.days) != 2 || len(r.hours) != 1 {
		t.Errorf("duplicate visitor rows queued: days %d hours %d", len(r.days), len(r.hours))
	}
	// A new path adds only its per-path row; a new hour adds an hour row.
	post(r, `{"type":"pageview","path":"/projects"}`, uaChrome)
	now = now.Add(time.Hour)
	post(r, `{"type":"pageview","path":"/projects"}`, uaChrome)
	if len(r.days) != 3 || len(r.hours) != 2 {
		t.Errorf("after new path/hour: days %d hours %d", len(r.days), len(r.hours))
	}
}

func TestEngagementClamp(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	r := testRecorder(&now)
	post(r, `{"type":"engagement","path":"/","visible_ms":99999999}`, uaChrome)
	post(r, `{"type":"engagement","path":"/","visible_ms":-5}`, uaChrome)
	for _, v := range r.pages {
		if v.visibleMS != maxVisibleMS {
			t.Errorf("visible_ms = %d, want %d", v.visibleMS, maxVisibleMS)
		}
	}
}

func TestLiveExpiry(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	r := testRecorder(&now)
	r.markLive("a", now)
	r.markLive("b", now.Add(-4*time.Minute))
	r.markLive("c", now.Add(-6*time.Minute))
	if got := r.LiveNow(); got != 2 {
		t.Errorf("LiveNow = %d, want 2", got)
	}
	now = now.Add(2 * time.Minute)
	if got := r.LiveNow(); got != 1 {
		t.Errorf("LiveNow after 2m = %d, want 1", got)
	}
	_ = r.Flush(t.Context()) // nothing pending: no DB needed; drops expired entries
	if len(r.live) != 1 {
		t.Errorf("live map not pruned: %d", len(r.live))
	}
}

func TestRequeueCap(t *testing.T) {
	now := time.Now()
	r := testRecorder(&now)
	days := make([]dayVisitor, maxPendingRows)
	for i := range days {
		days[i].visitor = "old"
	}
	r.days = []dayVisitor{{visitor: "new"}}
	r.requeueVisitors(days, []hourVisitor{{visitor: "h"}})
	if n := len(r.days) + len(r.hours); n != maxPendingRows {
		t.Fatalf("pending = %d, want %d", n, maxPendingRows)
	}
	if r.days[len(r.days)-1].visitor != "new" || len(r.hours) != 1 {
		t.Error("newest rows were dropped instead of the oldest")
	}
}
