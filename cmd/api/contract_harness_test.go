package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/analytics"
	"github.com/baoopn/baoopn-2026/internal/blog"
	"github.com/baoopn/baoopn-2026/internal/config"
	"github.com/baoopn/baoopn-2026/internal/contact"
	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/listening"
	"github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/overview"
	"github.com/baoopn/baoopn-2026/internal/profile"
	"github.com/baoopn/baoopn-2026/internal/projects"
	"github.com/baoopn/baoopn-2026/internal/seedimport"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/seed"
)

const (
	ctInboundDomain = "reply.baoopn.com"
	ctInboundSecret = "contract-secret"
	ctNotify        = "owner@example.com"
	ctUA            = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/141.0 Safari/537.36"
)

// recMailer is an enabled mailer that records every message.
type recMailer struct {
	mu       sync.Mutex
	sent     []mail.Message
	failNext bool
}

func (m *recMailer) Enabled() bool       { return true }
func (m *recMailer) FromName() string    { return "Bao Nguyen" }
func (m *recMailer) FromAddress() string { return "bao@mail.baoopn.com" }
func (m *recMailer) Send(_ context.Context, msg mail.Message) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	if m.failNext {
		m.failNext = false
		return "", mail.ErrRefused
	}
	return fmt.Sprintf("<out-%d@mail.baoopn.com>", len(m.sent)), nil
}

func (m *recMailer) failOnce() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failNext = true
}

func (m *recMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sent)
}

// fakeGitHubAPI serves one user and two public repos (plus a private one).
func fakeGitHubAPI(t *testing.T) *httptest.Server {
	repo := func(id int, name string, archived, private bool) map[string]any {
		return map[string]any{"id": id, "name": name, "full_name": "baoopn/" + name,
			"html_url": "https://github.com/baoopn/" + name, "homepage": "https://" + name + ".baoopn.com",
			"description": name + " description", "language": "Go", "topics": []string{"go", "api"},
			"stargazers_count": 7 * id, "forks_count": id, "archived": archived, "fork": false,
			"private": private, "pushed_at": "2026-10-01T08:00:00Z", "created_at": "2025-03-01T00:00:00Z",
			"default_branch": "main"}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4999")
		w.Header().Set("X-RateLimit-Reset", "1900000000")
		switch {
		case r.URL.Path == "/users/baoopn/repos":
			_ = json.NewEncoder(w).Encode([]map[string]any{repo(101, "spotify-tracker", false, false),
				repo(102, "old-site", true, false), repo(103, "secret-stuff", false, true)})
		case r.URL.Path == "/users/baoopn":
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "baoopn", "name": "Bao Nguyen",
				"bio": "Software engineer", "location": "Ho Chi Minh City", "avatar_url": "https://avatars.example/u/1",
				"email": nil, "blog": "baoopn.com", "html_url": "https://github.com/baoopn"})
		case r.URL.Path == "/repos/baoopn/spotify-tracker/readme":
			_, _ = w.Write([]byte("# spotify-tracker\n\n## Setup\n\nRun it.\n"))
		case strings.HasSuffix(r.URL.Path, "/readme"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/languages"):
			_, _ = w.Write([]byte(`{"Go": 52000, "TypeScript": 1200}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeSpotifyTracker serves the spotify-tracker API with extra upstream fields
// the proxy must trim.
func fakeSpotifyTracker(t *testing.T) *httptest.Server {
	track := `{"id":"t1","name":"Song","uri":"spotify:track:t1","isrc":"US1","duration_ms":201000,"explicit":false,
		"external_url":"https://open.spotify.com/track/t1",
		"album":{"id":"a1","name":"Album","release_date":"2020","cover_image_url":"https://i.scdn.co/c.jpg","uri":"x"},
		"artists":[{"id":"ar1","name":"Artist","uri":"spotify:artist:ar1","external_url":"https://open.spotify.com/artist/ar1"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/now-playing":
			_, _ = w.Write([]byte(`{"data":{"is_playing":true,"progress_ms":42000,"fetched_at":"2026-11-07T10:00:00+07:00","device":"x","track":` + track + `}}`))
		case "/api/v1/history":
			_, _ = w.Write([]byte(`{"data":[{"id":"h1","played_at":"2026-11-07T09:00:00+07:00","replay_count":1,"track":` + track + `}],"next_cursor":"c"}`))
		case "/api/v1/stats/top-artists":
			_, _ = w.Write([]byte(`{"data":[{"artist":{"id":"ar1","name":"Artist","uri":"u","image_url":"https://i.scdn.co/a.jpg"},"plays":42,"last_played":"2026-11-06T00:00:00Z"}]}`))
		case "/api/v1/playlists/top":
			entry := func(period, id, start, end, label, tracks string) string {
				return `{"period":"` + period + `","period_start":"` + start + `","period_end":"` + end + `","time_zone":"UTC",
					"label":"` + label + `","name":"Top 50 · ` + label + `","spotify_id":"` + id + `",
					"spotify_url":"https://open.spotify.com/playlist/` + id + `","track_count":50,"total_plays":120,
					"published_at":"2026-11-03T08:00:00+07:00","snapshot_id":"snap","tracks":` + tracks + `}`
			}
			week := entry("week", "plWeek1", "2026-11-02T00:00:00Z", "2026-11-09T00:00:00Z", "Week of Nov 2, 2026",
				`[{"position":1,"plays":12,"added_at":"x","track":`+track+`}]`)
			older := entry("week", "plWeek0", "2026-10-26T00:00:00Z", "2026-11-02T00:00:00Z", "Week of Oct 26, 2026", `[]`)
			invalid := entry("week", "bad-id", "2026-10-19T00:00:00Z", "2026-10-26T00:00:00Z", "Week of Oct 19, 2026", `[]`)
			month := entry("month", "plMonth1", "2026-11-01T00:00:00Z", "2026-12-01T00:00:00Z", "November 2026", `[]`)
			_, _ = w.Write([]byte(`{"data":{"time_zone":"UTC","extra":1,"periods":{"week":[` + week + `,` + older + `,` + invalid +
				`],"month":[` + month + `],"year":[]}}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type harness struct {
	t       *testing.T
	db      *pgxpool.Pool
	h       http.Handler
	rec     *analytics.Recorder
	contact *contact.Module
	mailer  *recMailer
	ip      string
}

// newHarness builds every module like buildModules, but with the GitHub and
// spotify-tracker base URLs pointed at fakes (buildModules cannot inject them).
func newHarness(t *testing.T, opts ...func(*httpapi.Deps)) *harness {
	t.Helper()
	ctx := t.Context()
	pool := testdb.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	gh := fakeGitHubAPI(t)
	sp := fakeSpotifyTracker(t)
	cfg := config.Config{PublicURL: "https://baoopn.com", GitHubUser: "baoopn",
		InboundDomain: ctInboundDomain, InboundSecret: ctInboundSecret}
	mailer := &recMailer{}
	httpClient := &http.Client{Timeout: 5 * time.Second}

	site := sitesettings.New(sitesettings.Deps{DB: pool, Log: log, DefaultNotify: ctNotify, Mailer: mailer})
	mediaStore, err := media.New(media.Deps{DB: pool, Log: log, Dir: t.TempDir(), MaxBytes: 1 << 20, UploadWait: -1})
	if err != nil {
		t.Fatal(err)
	}
	profileMod := profile.New(profile.Deps{DB: pool, Log: log, GitHubUser: "baoopn", HTTP: httpClient,
		Seed: seed.FS, APIBase: gh.URL})
	if err := profileMod.EnsureSeeded(ctx); err != nil {
		t.Fatal(err)
	}
	blogMod := blog.New(blog.Deps{DB: pool, Log: log})
	if err := seedimport.Run(ctx, seedimport.Deps{DB: pool, Log: log, Media: mediaStore, Blog: blogMod, FS: seed.FS}); err != nil {
		t.Fatal(err)
	}
	syncer := projects.NewSyncer(projects.SyncDeps{DB: pool, Log: log, User: "baoopn", HTTP: httpClient, APIBase: gh.URL})
	projectsMod := projects.New(projects.Deps{DB: pool, Log: log, Syncer: syncer})
	listeningMod := listening.New(listening.Deps{BaseURL: sp.URL, Log: log, HTTP: httpClient})
	contactMod := contact.New(contact.Deps{DB: pool, Log: log, Mailer: mailer, Settings: site,
		PublicURL: cfg.PublicURL, InboundDomain: ctInboundDomain, InboundSecret: ctInboundSecret, HTTP: httpClient})
	rec := analytics.New(analytics.Deps{DB: pool, Log: log, Salt: analytics.DeriveSalt("k"),
		SiteHost: analytics.HostOf(cfg.PublicURL)})
	ov := overview.New(overview.Deps{DB: pool, Log: log, Analytics: rec})

	pass := func(next http.Handler) http.Handler { return next }
	deps := httpapi.Deps{Log: log, DB: pool, AdminAuth: pass, Media: mediaStore.Handler(),
		ClientIP:    httpapi.TrustedClientIP(""),
		Modules:     []httpapi.Module{profileMod, projectsMod, blogMod, mediaStore, listeningMod, contactMod, site, rec, ov},
		AdminConfig: adminConfigHandler(cfg)}
	for _, o := range opts {
		o(&deps)
	}
	h := httpapi.New(deps)
	return &harness{t: t, db: pool, h: h, rec: rec, contact: contactMod, mailer: mailer, ip: "203.0.113.7"}
}

// raw sends a request and returns the recorder.
func (c *harness) raw(method, path string, body io.Reader, hdr map[string]string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("User-Agent", ctUA)
	req.Header.Set("X-Client-IP", c.ip)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, req)
	return w
}

func jsonBody(v any) io.Reader {
	if v == nil {
		return nil
	}
	if s, ok := v.(string); ok {
		return strings.NewReader(s)
	}
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// ok calls an endpoint, checks the status, the success envelope (exactly
// data, plus meta when withMeta) and the data shape, and returns data.
func (c *harness) ok(method, path string, body any, want int, s shape, withMeta bool) any {
	c.t.Helper()
	w := c.raw(method, path, jsonBody(body), nil)
	if w.Code != want {
		c.t.Errorf("%s %s = %d, want %d: %s", method, path, w.Code, want, w.Body)
		return nil
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		c.t.Errorf("%s %s: Content-Type %q", method, path, ct)
	}
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		c.t.Errorf("%s %s: not JSON: %v", method, path, err)
		return nil
	}
	envShape := fields{"data": s}
	if withMeta {
		envShape["meta"] = pageMeta
	}
	for _, p := range obj(envShape)("$", env) {
		c.t.Errorf("%s %s: %s", method, path, p)
	}
	return env["data"]
}

// decodeData checks a success envelope already received and returns data.
func decodeData(t *testing.T, w *httptest.ResponseRecorder, s shape) any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Errorf("not JSON: %q", w.Body)
		return nil
	}
	for _, p := range obj(fields{"data": s})("$", env) {
		t.Error(p)
	}
	return env["data"]
}

// empty calls an endpoint that answers with no body.
func (c *harness) empty(method, path string, body any, want int) {
	c.t.Helper()
	w := c.raw(method, path, jsonBody(body), nil)
	if w.Code != want || w.Body.Len() != 0 {
		c.t.Errorf("%s %s = %d %q, want %d and no body", method, path, w.Code, w.Body, want)
	}
}

// fail checks an error response: status, exactly {error:{code,message}}, code.
func (c *harness) fail(method, path string, body any, want int, code string) {
	c.t.Helper()
	c.failRaw(c.raw(method, path, jsonBody(body), nil), method+" "+path, want, code)
}

func (c *harness) failRaw(w *httptest.ResponseRecorder, what string, want int, code string) {
	c.t.Helper()
	if w.Code != want {
		c.t.Errorf("%s = %d, want %d: %s", what, w.Code, want, w.Body)
		return
	}
	var env map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		c.t.Errorf("%s: error body not JSON: %q", what, w.Body)
		return
	}
	for _, p := range obj(fields{"error": apiError})("$", env) {
		c.t.Errorf("%s: %s", what, p)
	}
	if e, _ := env["error"].(map[string]any); e != nil && e["code"] != code {
		c.t.Errorf("%s: error.code = %v, want %s", what, e["code"], code)
	}
}

// hook posts a raw RFC 822 message to the inbound hook, signed per docs/api.md.
func (c *harness) hook(raw, envTo string, at time.Time, secret string) *httptest.ResponseRecorder {
	c.t.Helper()
	tsv := strconv.FormatInt(at.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(tsv + "." + envTo + "\n" + raw)) // docs/api.md: the envelope recipient is signed
	return c.raw("POST", "/api/hooks/inbound-email", strings.NewReader(raw), map[string]string{
		"Content-Type": "message/rfc822", "X-Envelope-To": envTo, "X-Envelope-From": "bounce@mail.example",
		"X-Timestamp": tsv, "X-Signature": hex.EncodeToString(mac.Sum(nil)),
	})
}

// wait lets background sends finish.
func (c *harness) wait() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.contact.Shutdown(ctx)
}

func (c *harness) scalar(sql string, args ...any) any {
	c.t.Helper()
	var v any
	if err := c.db.QueryRow(context.Background(), sql, args...).Scan(&v); err != nil {
		c.t.Fatalf("%s: %v", sql, err)
	}
	return v
}

func idOf(v any) string {
	m, _ := v.(map[string]any)
	if f, ok := m["id"].(float64); ok {
		return strconv.FormatInt(int64(f), 10)
	}
	return "0"
}

func field(v any, k string) any {
	m, _ := v.(map[string]any)
	return m[k]
}

func list(v any) []any {
	a, _ := v.([]any)
	return a
}
