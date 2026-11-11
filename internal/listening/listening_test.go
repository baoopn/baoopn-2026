package listening

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type upstream struct {
	mu    sync.Mutex
	fail  bool
	calls map[string]int
	np    string

	lastLimit string
}

func (u *upstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.calls[r.URL.Path]++
	if u.fail {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/api/v1/now-playing":
		_, _ = w.Write([]byte(u.np))
	case "/api/v1/history":
		if r.URL.Query().Get("limit") != "3" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"h1","played_at":"2026-11-01T10:00:00+07:00","updated_at":"2026-11-01T03:00:00Z",
			"replay_count":2,"track":{"id":"t1","name":"Song","uri":"spotify:track:t1","isrc":"US123","duration_ms":200000,
			"explicit":true,"album":{"id":"a1","name":"Album","release_date":"2020","cover_image_url":"https://i/c.jpg","uri":"x"},
			"artists":[{"id":"ar1","name":"Artist","uri":"spotify:artist:ar1"}]}}],"next_cursor":"abc"}`))
	case "/api/v1/stats/top-artists":
		_, _ = w.Write([]byte(`{"data":[{"artist":{"id":"ar1","name":"Artist","uri":"u","image_url":"https://i/a.jpg"},"plays":42,"last_played":"2026-11-01T00:00:00Z"}]}`))
	case "/api/v1/playlists/top":
		u.lastLimit = r.URL.Query().Get("limit")
		_, _ = w.Write([]byte(playlistsUpstream))
	default:
		w.WriteHeader(404)
	}
}

// playlistsUpstream has extra fields to trim, a +07:00 timestamp, a bad top-level
// time_zone and entries that must be dropped.
const playlistsUpstream = `{"data":{"time_zone":"Bad Zone!","generated_by":"x","periods":{
	"week":[
		{"period":"week","period_start":"2026-11-02T00:00:00+07:00","period_end":"2026-11-09T00:00:00+07:00",
		 "time_zone":"UTC","label":"Week of Nov 2, 2026","name":"Top 50 · Week of Nov 2, 2026","spotify_id":"pl1",
		 "spotify_url":"https://open.spotify.com/playlist/pl1","track_count":1,"total_plays":9,
		 "published_at":"2026-11-03T01:00:00+07:00","snapshot_id":"snap","owner":"me",
		 "tracks":[{"position":1,"plays":9,"added_at":"x","track":{"id":"t1","name":"Song","uri":"spotify:track:t1",
		  "duration_ms":1,"explicit":false,"artists":null}},null,{"position":2,"plays":1,"track":null}]},
		{"period":"week","period_start":"2026-10-26T00:00:00Z","period_end":"2026-11-02T00:00:00Z","time_zone":"Not a zone",
		 "label":"Week of Oct 26, 2026","name":"Old","spotify_id":"pl2","spotify_url":"https://open.spotify.com/playlist/pl2",
		 "track_count":50,"total_plays":100,"published_at":"2026-10-27T00:00:00Z"},
		{"period":"week","period_start":"2026-10-19T00:00:00Z","period_end":"2026-10-26T00:00:00Z","time_zone":"UTC",
		 "label":"Bad id","name":"Bad id","spotify_id":"ab-c","spotify_url":"https://open.spotify.com/playlist/abc",
		 "track_count":1,"total_plays":1,"published_at":"2026-10-20T00:00:00Z","tracks":[]},
		{"period":"week","period_start":"2026-10-12T00:00:00Z","period_end":"2026-10-19T00:00:00Z","time_zone":"UTC",
		 "label":"Slash id","name":"Slash id","spotify_id":"x/y","spotify_url":"https://open.spotify.com/playlist/xy",
		 "track_count":1,"total_plays":1,"published_at":"2026-10-13T00:00:00Z","tracks":[]},
		{"period":"week","period_start":"2026-10-05T00:00:00Z","period_end":"2026-10-12T00:00:00Z","time_zone":"UTC",
		 "label":"Bad url","name":"Bad url","spotify_id":"pl5","spotify_url":"https://evil.example/playlist/pl5",
		 "track_count":1,"total_plays":1,"published_at":"2026-10-06T00:00:00Z","tracks":[]},
		{"period":"week","period_start":"2026-09-28T00:00:00Z","period_end":"2026-10-05T00:00:00Z","time_zone":"UTC",
		 "label":"Empty id","name":"Empty id","spotify_id":"","spotify_url":"https://open.spotify.com/playlist/",
		 "track_count":1,"total_plays":1,"published_at":"2026-09-29T00:00:00Z","tracks":[]},
		{"period":"week","period_start":"2026-09-21","period_end":"2026-09-28T00:00:00Z","time_zone":"UTC",
		 "label":"Bad time","name":"Bad time","spotify_id":"pl7","spotify_url":"https://open.spotify.com/playlist/pl7",
		 "track_count":1,"total_plays":1,"published_at":"2026-09-22T00:00:00Z","tracks":[]},
		{"period":"week","label":"No bounds","name":"No bounds","spotify_id":"pl8","spotify_url":"https://open.spotify.com/playlist/pl8",
		 "track_count":1,"total_plays":1,"tracks":[]},
		{"period":"week","period_start":"2026-09-14T00:00:00Z","period_end":"2026-09-14T00:00:00Z","time_zone":"UTC",
		 "label":"Empty range","name":"Empty range","spotify_id":"pl9","spotify_url":"https://open.spotify.com/playlist/pl9",
		 "track_count":1,"total_plays":1,"published_at":"2026-09-15T00:00:00Z","tracks":[]},
		{"period":"week","period_start":"2026-10-26T00:00:00Z","period_end":"2026-11-02T00:00:00Z","time_zone":"UTC",
		 "label":"Duplicate","name":"Duplicate","spotify_id":"pl2","spotify_url":"https://open.spotify.com/playlist/pl2",
		 "track_count":1,"total_plays":1,"published_at":"2026-10-27T00:00:00Z","tracks":[]}
	],
	"month":[
		{"period":"week","period_start":"2026-11-01T00:00:00Z","period_end":"2026-12-01T00:00:00Z","time_zone":"UTC",
		 "label":"Mismatch","name":"Mismatch","spotify_id":"pl6","spotify_url":"https://open.spotify.com/playlist/pl6",
		 "track_count":1,"total_plays":1,"published_at":"2026-11-02T00:00:00Z","tracks":[]}
	],
	"decade":[]
}}}`

func (u *upstream) set(fn func(*upstream)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	fn(u)
}

func (u *upstream) count(p string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls[p]
}

func setup(t *testing.T) (*upstream, chi.Router, *time.Time) {
	u := &upstream{calls: map[string]int{}, np: `{"data":null}`}
	srv := httptest.NewServer(u)
	t.Cleanup(srv.Close)
	m := New(Deps{BaseURL: srv.URL + "/", Log: slog.New(slog.DiscardHandler)})
	now := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	m.cache.now = func() time.Time { return now }
	r := chi.NewRouter()
	m.Public(r)
	return u, r, &now
}

func get(r chi.Router, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestTrimming(t *testing.T) {
	_, r, _ := setup(t)
	rec := get(r, "/listening/recent?limit=3")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("recent %d %v", rec.Code, rec.Header())
	}
	body := rec.Body.String()
	for _, gone := range []string{"uri", "isrc", "release_date", "replay_count", "updated_at", "next_cursor"} {
		if strings.Contains(body, gone) {
			t.Errorf("%s not trimmed: %s", gone, body)
		}
	}
	for _, kept := range []string{`"played_at":"2026-11-01T03:00:00Z"`, `"cover_image_url":"https://i/c.jpg"`, `"explicit":true`, `"duration_ms":200000`} {
		if !strings.Contains(body, kept) {
			t.Errorf("missing %s: %s", kept, body)
		}
	}
	rec = get(r, "/listening/now-playing")
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != `{"data":null}` || rec.Header().Get("Cache-Control") != "public, max-age=5" {
		t.Fatalf("now-playing null: %d %s", rec.Code, rec.Body)
	}
	rec = get(r, "/listening/top-artists")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"plays":42`) || strings.Contains(rec.Body.String(), `"uri"`) {
		t.Fatalf("top %d %s", rec.Code, rec.Body)
	}
}

func TestTopPlaylists(t *testing.T) {
	u, r, _ := setup(t)
	rec := get(r, "/listening/top-playlists?limit=4")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=300" {
		t.Fatalf("top-playlists %d %v", rec.Code, rec.Header())
	}
	u.set(func(u *upstream) {
		if u.lastLimit != "4" {
			t.Errorf("limit forwarded as %q", u.lastLimit)
		}
	})
	var env struct{ Data TopPlaylists }
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	v := env.Data
	if v.TimeZone != "UTC" || len(v.Periods.Week) != 2 || len(v.Periods.Month) != 0 || v.Periods.Year == nil {
		t.Fatalf("periods: %s", rec.Body)
	}
	newest, old := v.Periods.Week[0], v.Periods.Week[1]
	if newest.SpotifyID != "pl1" || old.SpotifyID != "pl2" || old.TimeZone != "UTC" || old.Tracks == nil || len(old.Tracks) != 0 {
		t.Errorf("entries: %+v / %+v", newest, old)
	}
	body := rec.Body.String()
	for _, gone := range []string{"generated_by", "snapshot_id", "owner", "added_at", "uri", "decade", "ab-c", "x/y", "evil.example", "Mismatch",
		"Empty id", "Bad time", "No bounds", "Empty range", "Duplicate", `"position":2`} {
		if strings.Contains(body, gone) {
			t.Errorf("%s not trimmed: %s", gone, body)
		}
	}
	for _, kept := range []string{`"period_start":"2026-11-01T17:00:00Z"`, `"published_at":"2026-11-02T18:00:00Z"`,
		`"tracks":[{"position":1,"plays":9,"track":{`, `"artists":[]`, `"month":[]`, `"year":[]`} {
		if !strings.Contains(body, kept) {
			t.Errorf("missing %s: %s", kept, body)
		}
	}
	if rec := get(r, "/listening/top-playlists"); rec.Code != 200 {
		t.Errorf("default limit: %d", rec.Code)
	}
	u.set(func(u *upstream) {
		if u.lastLimit != "6" {
			t.Errorf("default limit forwarded as %q", u.lastLimit)
		}
	})
}

func TestTopPlaylistsEdges(t *testing.T) {
	for _, l := range []string{"1", "24"} {
		u, r, _ := setup(t)
		if rec := get(r, "/listening/top-playlists?limit="+l); rec.Code != 200 {
			t.Errorf("limit=%s: %d", l, rec.Code)
		}
		u.set(func(u *upstream) {
			if u.lastLimit != l {
				t.Errorf("limit=%s forwarded as %q", l, u.lastLimit)
			}
		})
	}
	// Empty or null data still yields three arrays.
	for _, body := range []string{`{"data":{}}`, `{"data":null}`, `{"data":{"periods":null}}`} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
		t.Cleanup(srv.Close)
		m := New(Deps{BaseURL: srv.URL + "/", Log: slog.New(slog.DiscardHandler)})
		r := chi.NewRouter()
		m.Public(r)
		rec := get(r, "/listening/top-playlists")
		if want := `{"data":{"time_zone":"UTC","periods":{"week":[],"month":[],"year":[]}}}`; rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != want {
			t.Errorf("%s -> %d %s", body, rec.Code, rec.Body)
		}
	}
	for tz, want := range map[string]string{"UTC": "UTC", "Etc/GMT+5": "Etc/GMT+5", strings.Repeat("A", 65): "UTC", "": "UTC", "../x": "UTC"} {
		if got := cleanTimeZone(tz); got != want {
			t.Errorf("cleanTimeZone(%q) = %q", tz, got)
		}
	}
}

func TestTopPlaylistsStale(t *testing.T) {
	u, r, now := setup(t)
	if rec := get(r, "/listening/top-playlists"); rec.Code != 200 {
		t.Fatalf("warm %d", rec.Code)
	}
	u.set(func(u *upstream) { u.fail = true })
	*now = now.Add(ttlTop + time.Second)
	if rec := get(r, "/listening/top-playlists"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"pl1"`) {
		t.Fatalf("stale %d %s", rec.Code, rec.Body)
	}
	*now = now.Add(10 * ttlTop)
	if rec := get(r, "/listening/top-playlists"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expired %d", rec.Code)
	}
}

func TestValidation(t *testing.T) {
	_, r, _ := setup(t)
	for _, p := range []string{"/listening/recent?limit=0", "/listening/recent?limit=21", "/listening/recent?limit=x",
		"/listening/top-artists?range=1y", "/listening/top-artists?limit=50",
		"/listening/top-playlists?limit=0", "/listening/top-playlists?limit=25", "/listening/top-playlists?limit=x"} {
		if rec := get(r, p); rec.Code != 400 || !strings.Contains(rec.Body.String(), "bad_request") {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
}

func TestTTLAndStale(t *testing.T) {
	u, r, now := setup(t)
	u.set(func(u *upstream) {
		u.np = `{"data":{"is_playing":true,"progress_ms":1000,"fetched_at":"2026-11-06T12:00:00Z","track":{"id":"t","name":"N","duration_ms":1,"explicit":false,"artists":null}}}`
	})
	if rec := get(r, "/listening/now-playing"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"artists":[]`) {
		t.Fatalf("first %d %s", rec.Code, rec.Body)
	}
	get(r, "/listening/now-playing")
	if u.count("/api/v1/now-playing") != 1 {
		t.Fatalf("cache miss within TTL: %d calls", u.count("/api/v1/now-playing"))
	}
	*now = now.Add(6 * time.Second)
	get(r, "/listening/now-playing")
	if u.count("/api/v1/now-playing") != 2 {
		t.Fatalf("not refreshed after TTL: %d calls", u.count("/api/v1/now-playing"))
	}

	// Upstream down: stale within 10× TTL, then 503.
	u.set(func(u *upstream) { u.fail = true })
	*now = now.Add(30 * time.Second)
	if rec := get(r, "/listening/now-playing"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"is_playing":true`) {
		t.Fatalf("stale %d %s", rec.Code, rec.Body)
	}
	*now = now.Add(30 * time.Second)
	if rec := get(r, "/listening/now-playing"); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "upstream_unavailable") {
		t.Fatalf("expired stale %d %s", rec.Code, rec.Body)
	}
	if rec := get(r, "/listening/top-artists"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("cold failure %d", rec.Code)
	}
}

func TestNoBaseURL(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	r := chi.NewRouter()
	m.Public(r)
	// Unconfigured (SPOTIFY_API_URL empty): empty results, not errors.
	for path, want := range map[string]string{
		"/listening/now-playing":   `{"data":null}`,
		"/listening/recent":        `{"data":[]}`,
		"/listening/top-artists":   `{"data":[]}`,
		"/listening/top-playlists": `{"data":{"time_zone":"UTC","periods":{"week":[],"month":[],"year":[]}}}`,
	} {
		rec := get(r, path)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
}
