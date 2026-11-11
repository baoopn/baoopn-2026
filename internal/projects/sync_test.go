package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// fakeGitHub serves two pages of repos, an etag, READMEs and languages.
type fakeGitHub struct {
	mu        sync.Mutex
	srv       *httptest.Server
	etag      string
	pushedA   time.Time
	remaining int
	fail      int // status to fail repo listing with (0 = none)
	failPaths map[string]int
	block     chan struct{} // when set, repo listing waits for it to close
	calls     map[string]int
	authSeen  string
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{etag: `"v1"`, pushedA: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), remaining: 100, calls: map[string]int{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) set(fn func(*fakeGitHub)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeGitHub) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	block := f.block
	f.mu.Unlock()
	if block != nil && r.URL.Path == "/users/bao/repos" {
		<-block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[r.URL.Path]++
	f.authSeen = r.Header.Get("Authorization")
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(f.remaining))
	w.Header().Set("X-RateLimit-Reset", "1900000000")
	if st := f.failPaths[r.URL.Path]; st != 0 {
		w.WriteHeader(st)
		return
	}
	repo := func(id int, name string, pushed time.Time, private bool) map[string]any {
		return map[string]any{"id": id, "name": name, "full_name": "bao/" + name, "html_url": "https://github.com/bao/" + name,
			"homepage": nil, "description": name + " desc", "language": "Go", "topics": []string{"go"},
			"stargazers_count": id, "forks_count": 1, "archived": false, "fork": false, "private": private,
			"pushed_at": pushed, "created_at": "2025-01-01T00:00:00Z", "default_branch": "main"}
	}
	switch {
	case r.URL.Path == "/users/bao/repos":
		if f.fail != 0 {
			if f.fail == http.StatusForbidden {
				w.Header().Set("X-RateLimit-Remaining", "0")
			}
			w.WriteHeader(f.fail)
			return
		}
		if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") == "" {
			w.WriteHeader(400)
			return
		}
		if r.URL.Query().Get("page") == "1" && r.Header.Get("If-None-Match") == f.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		old := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
		var body []map[string]any
		if r.URL.Query().Get("page") == "1" {
			w.Header().Set("ETag", f.etag)
			w.Header().Set("Link", fmt.Sprintf(`<%s/users/bao/repos?page=2>; rel="next", <%s/users/bao/repos?page=2>; rel="last"`, f.srv.URL, f.srv.URL))
			body = []map[string]any{repo(1, "alpha", f.pushedA, false), repo(2, "beta", old, false), repo(3, "secret", old, true)}
		} else {
			body = []map[string]any{repo(4, "gamma", old, false)}
		}
		_ = json.NewEncoder(w).Encode(body)
	case r.URL.Path == "/repos/bao/alpha/readme":
		if r.Header.Get("Accept") != "application/vnd.github.raw" {
			w.WriteHeader(400)
			return
		}
		_, _ = w.Write([]byte("# Alpha\n\n![shot](docs/shot.png)\n\n<script>alert(1)</script>"))
	case strings.HasSuffix(r.URL.Path, "/readme"):
		w.WriteHeader(http.StatusNotFound)
	case strings.HasSuffix(r.URL.Path, "/languages"):
		_, _ = w.Write([]byte(`{"Go": 1200, "Shell": 30}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type env struct {
	t     *testing.T
	m     *Module
	s     *Syncer
	gh    *fakeGitHub
	admin chi.Router
	pub   chi.Router
}

func newEnv(t *testing.T) *env {
	pool := testdb.New(t)
	gh := newFakeGitHub(t)
	log := slog.New(slog.DiscardHandler)
	s := NewSyncer(SyncDeps{DB: pool, Log: log, User: "bao", Token: "tok", APIBase: gh.srv.URL})
	m := New(Deps{DB: pool, Log: log, Syncer: s})
	e := &env{t: t, m: m, s: s, gh: gh, admin: chi.NewRouter(), pub: chi.NewRouter()}
	m.Admin(e.admin)
	m.Public(e.pub)
	return e
}

func (e *env) do(r chi.Router, method, path string, body, out any) int {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("decode: %v %s", err, rec.Body)
		}
	}
	return rec.Code
}

func (e *env) sync() SyncResult {
	e.t.Helper()
	var out struct{ Data SyncResult }
	if code := e.do(e.admin, "POST", "/projects/sync", nil, &out); code != 200 {
		e.t.Fatalf("sync: %d", code)
	}
	return out.Data
}

func (e *env) bySlug(slug string) AdminProject {
	e.t.Helper()
	var out struct{ Data []AdminProject }
	e.do(e.admin, "GET", "/projects", nil, &out)
	for _, p := range out.Data {
		if p.Slug == slug {
			return p
		}
	}
	e.t.Fatalf("no project %s", slug)
	return AdminProject{}
}

func TestSyncAgainstFakeGitHub(t *testing.T) {
	e := newEnv(t)
	res := e.sync()
	if res.Added != 3 || res.Updated != 0 || res.Total != 3 {
		t.Fatalf("first sync %+v", res)
	}
	e.gh.set(func(f *fakeGitHub) {
		if f.authSeen != "Bearer tok" {
			t.Errorf("auth header %q", f.authSeen)
		}
	})
	alpha := e.bySlug("alpha")
	if alpha.Visible || alpha.GitHubID == nil || *alpha.GitHubID != 1 || alpha.Title != "alpha" || alpha.GHHomepage != "" {
		t.Fatalf("alpha %+v", alpha)
	}
	if e.gh.count("/repos/bao/alpha/readme") != 0 {
		t.Fatal("details fetched for hidden repo")
	}

	// Curate alpha; the next sync (304) still runs the details pass.
	id := strconv.FormatInt(alpha.ID, 10)
	var patched struct{ Data AdminProject }
	code := e.do(e.admin, "PATCH", "/projects/"+id, map[string]any{"visible": true, "title": "Alpha Custom",
		"summary": "Mine", "featured": true, "tech": []string{"Go"}}, &patched)
	if code != 200 || !patched.Data.Visible || patched.Data.Title != "Alpha Custom" {
		t.Fatalf("patch %d %+v", code, patched.Data)
	}
	select {
	case kid := <-e.s.kick:
		if kid != alpha.ID {
			t.Fatalf("kick id %d", kid)
		}
	default:
		t.Fatal("visible flip did not kick the syncer")
	}

	res = e.sync()
	if res.Added != 0 || res.Updated != 0 || res.Total != 3 {
		t.Fatalf("304 sync %+v", res)
	}
	if e.gh.count("/repos/bao/alpha/readme") != 1 || e.gh.count("/repos/bao/alpha/languages") != 1 {
		t.Fatal("details pass did not run")
	}
	var detail struct{ Data ProjectDetail }
	if code := e.do(e.pub, "GET", "/projects/alpha", nil, &detail); code != 200 {
		t.Fatalf("public detail %d", code)
	}
	if detail.Data.BodySource != "readme" ||
		!strings.Contains(detail.Data.BodyHTML, "https://raw.githubusercontent.com/bao/alpha/main/docs/shot.png") ||
		strings.Contains(detail.Data.BodyHTML, "<script>") || detail.Data.Languages["Go"] != 1200 {
		t.Fatalf("readme detail %+v", detail.Data)
	}

	// Unchanged pushed_at: no second details fetch.
	e.sync()
	if e.gh.count("/repos/bao/alpha/readme") != 1 {
		t.Fatal("details refetched although pushed_at is unchanged")
	}

	// A push changes pushed_at and the etag: upsert + details again; admin fields survive.
	e.gh.set(func(f *fakeGitHub) { f.etag = `"v2"`; f.pushedA = f.pushedA.Add(time.Hour) })
	res = e.sync()
	if res.Updated != 3 || res.Added != 0 {
		t.Fatalf("re-sync %+v", res)
	}
	if e.gh.count("/repos/bao/alpha/readme") != 2 {
		t.Fatal("details not refetched after push")
	}
	alpha = e.bySlug("alpha")
	if alpha.Title != "Alpha Custom" || alpha.Summary != "Mine" || !alpha.Visible || !alpha.Featured ||
		strings.Join(alpha.Tech, ",") != "Go" {
		t.Fatalf("admin fields lost %+v", alpha)
	}

	// Low quota defers the details pass; README 404 means no body.
	beta := e.bySlug("beta")
	e.do(e.admin, "PATCH", "/projects/"+strconv.FormatInt(beta.ID, 10), map[string]any{"visible": true}, nil)
	e.gh.set(func(f *fakeGitHub) { f.remaining = 2 })
	e.sync()
	if e.gh.count("/repos/bao/beta/readme") != 0 {
		t.Fatal("details ran with low quota")
	}
	e.gh.set(func(f *fakeGitHub) { f.remaining = 100 })
	e.sync()
	if e.gh.count("/repos/bao/beta/readme") != 1 {
		t.Fatal("beta details not fetched")
	}
	e.do(e.pub, "GET", "/projects/beta", nil, &detail)
	if detail.Data.BodySource != "none" || detail.Data.Languages["Shell"] != 30 {
		t.Fatalf("beta detail %+v", detail.Data)
	}

	// Public list: visible only, featured first.
	var list struct{ Data []ProjectSummary }
	e.do(e.pub, "GET", "/projects", nil, &list)
	if len(list.Data) != 2 || list.Data[0].Slug != "alpha" {
		t.Fatalf("public list %+v", list.Data)
	}
	e.do(e.pub, "GET", "/projects?featured=1", nil, &list)
	if len(list.Data) != 1 {
		t.Fatalf("featured list %+v", list.Data)
	}
	if code := e.do(e.pub, "GET", "/projects/gamma", nil, nil); code != 404 {
		t.Fatalf("hidden detail %d", code)
	}

	// Errors.
	e.gh.set(func(f *fakeGitHub) { f.fail = http.StatusForbidden })
	if code := e.do(e.admin, "POST", "/projects/sync", nil, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("rate limited: %d", code)
	}
	e.gh.set(func(f *fakeGitHub) { f.fail = http.StatusInternalServerError })
	if code := e.do(e.admin, "POST", "/projects/sync", nil, nil); code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable: %d", code)
	}
	e.s.mu.Lock()
	code = e.do(e.admin, "POST", "/projects/sync", nil, nil)
	e.s.mu.Unlock()
	if code != http.StatusConflict {
		t.Fatalf("concurrent sync: %d", code)
	}
}

func TestManualReorderViews(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	var a, b, c struct{ Data AdminProject }
	e.do(e.admin, "POST", "/projects", map[string]string{"title": "Side Project"}, &a)
	e.do(e.admin, "POST", "/projects", map[string]string{"title": "Side Project"}, &b)
	if code := e.do(e.admin, "POST", "/projects", map[string]string{"title": "X", "slug": "third"}, &c); code != 201 {
		t.Fatalf("create %d", code)
	}
	if a.Data.Slug != "side-project" || b.Data.Slug != "side-project-2" || a.Data.Visible || a.Data.GitHubID != nil {
		t.Fatalf("manual %+v %+v", a.Data, b.Data)
	}
	if code := e.do(e.admin, "POST", "/projects", map[string]string{"title": "X", "slug": "third"}, nil); code != http.StatusConflict {
		t.Fatalf("taken %d", code)
	}
	if code := e.do(e.admin, "PATCH", "/projects/"+strconv.FormatInt(b.Data.ID, 10), map[string]any{"slug": "third"}, nil); code != http.StatusConflict {
		t.Fatalf("patch taken %d", code)
	}
	if code := e.do(e.admin, "PATCH", "/projects/"+strconv.FormatInt(b.Data.ID, 10), map[string]any{"body_md": "## Case"}, nil); code != 200 {
		t.Fatalf("patch body %d", code)
	}

	ids := []int64{c.Data.ID, a.Data.ID, b.Data.ID}
	if code := e.do(e.admin, "POST", "/projects/reorder", map[string]any{"ids": ids}, nil); code != 204 {
		t.Fatalf("reorder %d", code)
	}
	var list struct{ Data []AdminProject }
	e.do(e.admin, "GET", "/projects", nil, &list)
	for i, p := range list.Data {
		if p.ID != ids[i] || p.SortOrder != i {
			t.Fatalf("order %d: %+v", i, p)
		}
	}
	if _, err := e.m.db.Exec(ctx, `INSERT INTO page_view_stats (bucket, path, device, browser, country, referrer, views)
		VALUES (now(), '/projects/third', 'desktop', 'chrome', '', '', 4), (now() - interval '40 days', '/projects/third', 'desktop', 'chrome', '', '', 9)`); err != nil {
		t.Fatal(err)
	}
	var one struct{ Data AdminProject }
	e.do(e.admin, "GET", "/projects/"+strconv.FormatInt(c.Data.ID, 10), nil, &one)
	if one.Data.Views30d != 4 {
		t.Fatalf("views %d", one.Data.Views30d)
	}
	if code := e.do(e.admin, "DELETE", "/projects/"+strconv.FormatInt(c.Data.ID, 10), nil, nil); code != 204 {
		t.Fatalf("delete %d", code)
	}
	if code := e.do(e.admin, "DELETE", "/projects/"+strconv.FormatInt(c.Data.ID, 10), nil, nil); code != 404 {
		t.Fatalf("delete again %d", code)
	}
	if code := e.do(e.admin, "POST", "/projects/reorder", map[string]any{"ids": make([]int64, 501)}, nil); code != 422 {
		t.Fatalf("reorder cap %d", code)
	}
}
