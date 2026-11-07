package listening

import (
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
	default:
		w.WriteHeader(404)
	}
}

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

func TestValidation(t *testing.T) {
	_, r, _ := setup(t)
	for _, p := range []string{"/listening/recent?limit=0", "/listening/recent?limit=21", "/listening/recent?limit=x",
		"/listening/top-artists?range=1y", "/listening/top-artists?limit=50"} {
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

	// Upstream down: stale within 10× TTL, then 502.
	u.set(func(u *upstream) { u.fail = true })
	*now = now.Add(30 * time.Second)
	if rec := get(r, "/listening/now-playing"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"is_playing":true`) {
		t.Fatalf("stale %d %s", rec.Code, rec.Body)
	}
	*now = now.Add(30 * time.Second)
	if rec := get(r, "/listening/now-playing"); rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "upstream_unavailable") {
		t.Fatalf("expired stale %d %s", rec.Code, rec.Body)
	}
	if rec := get(r, "/listening/top-artists"); rec.Code != http.StatusBadGateway {
		t.Fatalf("cold failure %d", rec.Code)
	}
}

func TestNoBaseURL(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	r := chi.NewRouter()
	m.Public(r)
	// Unconfigured (SPOTIFY_API_URL empty): empty results, not errors.
	for path, want := range map[string]string{
		"/listening/now-playing": `{"data":null}`,
		"/listening/recent":      `{"data":[]}`,
		"/listening/top-artists": `{"data":[]}`,
	} {
		rec := get(r, path)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
			t.Errorf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
}
