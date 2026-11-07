// Package listening proxies the spotify-tracker public API (now playing,
// recent plays, top artists, top playlists), trimmed to the types.ts shapes and cached.
package listening

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

// Deps are the listening module's dependencies.
type Deps struct {
	BaseURL string
	Log     *slog.Logger
	HTTP    *http.Client
}

// Module implements httpapi.Module for /listening/*.
type Module struct {
	base  string
	log   *slog.Logger
	http  *http.Client
	cache *cache
}

// New returns the listening module. An empty BaseURL serves empty results.
func New(d Deps) *Module {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 5 * time.Second}
	}
	return &Module{base: strings.TrimRight(d.BaseURL, "/"), log: d.Log, http: d.HTTP, cache: newCache(time.Now)}
}

// Artist is types.ts SpotifyArtist.
type Artist struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ExternalURL string `json:"external_url,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
}

// Album is the album embedded in SpotifyTrack.
type Album struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	CoverImageURL string `json:"cover_image_url,omitempty"`
	ExternalURL   string `json:"external_url,omitempty"`
}

// Track is types.ts SpotifyTrack.
type Track struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	DurationMS  int64    `json:"duration_ms"`
	Explicit    bool     `json:"explicit"`
	ExternalURL string   `json:"external_url,omitempty"`
	Album       *Album   `json:"album,omitempty"`
	Artists     []Artist `json:"artists"`
}

func (t *Track) normalize() {
	if t != nil && t.Artists == nil {
		t.Artists = []Artist{}
	}
}

// NowPlaying is types.ts NowPlaying.
type NowPlaying struct {
	IsPlaying  bool      `json:"is_playing"`
	ProgressMS *int64    `json:"progress_ms"`
	FetchedAt  time.Time `json:"fetched_at"`
	Track      *Track    `json:"track"`
}

// RecentPlay is types.ts RecentPlay.
type RecentPlay struct {
	ID       string    `json:"id"`
	PlayedAt time.Time `json:"played_at"`
	Track    Track     `json:"track"`
}

// TopArtist is types.ts TopArtist.
type TopArtist struct {
	Artist     Artist    `json:"artist"`
	Plays      int64     `json:"plays"`
	LastPlayed time.Time `json:"last_played"`
}

// Public mounts the listening routes.
func (m *Module) Public(r chi.Router) {
	if m.base == "" {
		// SPOTIFY_API_URL unset: the widgets are simply empty, not errors.
		r.Get("/listening/now-playing", func(w http.ResponseWriter, _ *http.Request) { httpapi.JSON(w, http.StatusOK, nil) })
		empty := func(w http.ResponseWriter, _ *http.Request) { httpapi.JSON(w, http.StatusOK, []any{}) }
		r.Get("/listening/recent", empty)
		r.Get("/listening/top-artists", empty)
		r.Get("/listening/top-playlists", func(w http.ResponseWriter, _ *http.Request) {
			httpapi.JSON(w, http.StatusOK, emptyTopPlaylists())
		})
		return
	}
	r.Get("/listening/now-playing", m.nowPlaying)
	r.Get("/listening/recent", m.recent)
	r.Get("/listening/top-artists", m.topArtists)
	r.Get("/listening/top-playlists", m.topPlaylists)
}

// Admin implements httpapi.Module (no admin routes).
func (m *Module) Admin(chi.Router) {}

// Hooks implements httpapi.Module (no hooks).
func (m *Module) Hooks(chi.Router) {}

const (
	ttlNow    = 5 * time.Second
	ttlRecent = 60 * time.Second
	ttlTop    = 5 * time.Minute
)

func (m *Module) nowPlaying(w http.ResponseWriter, r *http.Request) {
	m.serve(w, r, "/api/v1/now-playing", ttlNow, func(raw []byte) (any, error) {
		var v *NowPlaying
		if err := decodeData(raw, &v); err != nil {
			return nil, err
		}
		if v != nil {
			v.FetchedAt = v.FetchedAt.UTC()
			v.Track.normalize()
		}
		return v, nil
	})
}

func (m *Module) recent(w http.ResponseWriter, r *http.Request) {
	limit, ok := intParam(r.URL.Query().Get("limit"), 6, 1, 20)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 20")
		return
	}
	m.serve(w, r, "/api/v1/history?limit="+strconv.Itoa(limit), ttlRecent, func(raw []byte) (any, error) {
		var v []RecentPlay
		if err := decodeData(raw, &v); err != nil {
			return nil, err
		}
		if v == nil {
			v = []RecentPlay{}
		}
		for i := range v {
			v[i].PlayedAt = v[i].PlayedAt.UTC()
			v[i].Track.normalize()
		}
		return v, nil
	})
}

var ranges = map[string]bool{"7d": true, "30d": true, "90d": true}

func (m *Module) topArtists(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rng := q.Get("range")
	if rng == "" {
		rng = "30d"
	}
	if !ranges[rng] {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "range must be 7d, 30d or 90d")
		return
	}
	limit, ok := intParam(q.Get("limit"), 10, 1, 20)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 20")
		return
	}
	path := "/api/v1/stats/top-artists?range=" + url.QueryEscape(rng) + "&limit=" + strconv.Itoa(limit)
	m.serve(w, r, path, ttlTop, func(raw []byte) (any, error) {
		var v []TopArtist
		if err := decodeData(raw, &v); err != nil {
			return nil, err
		}
		if v == nil {
			v = []TopArtist{}
		}
		for i := range v {
			v[i].LastPlayed = v[i].LastPlayed.UTC()
		}
		return v, nil
	})
}

func intParam(v string, def, lo, hi int) (int, bool) {
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, false
	}
	return n, true
}
