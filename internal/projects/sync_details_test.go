package projects

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/settings"
)

// One repo whose details fail must not block the projects after it.
func TestDetailsContinuePastFailingRepo(t *testing.T) {
	e := newEnv(t)
	e.sync()
	for _, slug := range []string{"alpha", "beta"} {
		p := e.bySlug(slug)
		if code := e.do(e.admin, "PATCH", "/projects/"+strconv.FormatInt(p.ID, 10), map[string]any{"visible": true}, nil); code != 200 {
			t.Fatalf("show %s: %d", slug, code)
		}
	}
	e.gh.set(func(f *fakeGitHub) { f.failPaths = map[string]int{"/repos/bao/alpha/languages": 500} })
	e.sync() // still 200: the details pass only reports

	if e.gh.count("/repos/bao/beta/languages") != 1 {
		t.Fatal("beta details skipped after alpha failed")
	}
	var detail struct{ Data ProjectDetail }
	e.do(e.pub, "GET", "/projects/beta", nil, &detail)
	if detail.Data.Languages["Shell"] != 30 {
		t.Fatalf("beta detail %+v", detail.Data)
	}
	var st syncState
	if _, err := settings.Get(context.Background(), e.m.db, stateKey, &st); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(st.LastError, "bao/alpha") || !strings.Contains(st.LastError, "1 of 2") {
		t.Fatalf("last error %q", st.LastError)
	}

	// Alpha is retried on the next run and clears the error.
	e.gh.set(func(f *fakeGitHub) { f.failPaths = nil })
	e.sync()
	if _, err := settings.Get(context.Background(), e.m.db, stateKey, &st); err != nil || st.LastError != "" {
		t.Fatalf("last error after recovery %q %v", st.LastError, err)
	}
}

// An admin sync keeps running when the request is cancelled (client gone,
// request timeout), so the upsert is not rolled back mid-way: the handler
// answers 202 and the sync finishes in the background, holding the lock.
func TestAdminSyncIgnoresRequestCancel(t *testing.T) {
	e := newEnv(t)
	release := make(chan struct{})
	e.gh.set(func(f *fakeGitHub) { f.block = release }) // repo listing waits
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	e.admin.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, "POST", "/projects/sync", nil))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"running"`) {
		t.Fatalf("sync with cancelled request: %d %s", rec.Code, rec.Body)
	}
	// Still running: a second sync is refused.
	if code := e.do(e.admin, "POST", "/projects/sync", nil, nil); code != http.StatusConflict {
		t.Fatalf("overlapping sync: %d", code)
	}
	close(release)
	e.s.mu.Lock() // waits for the background sync
	e.s.mu.Unlock()
	var n int
	if err := e.m.db.QueryRow(context.Background(), `SELECT count(*) FROM projects`).Scan(&n); err != nil || n != 3 {
		t.Fatalf("projects after background sync: %d %v", n, err)
	}
}

// A sync slower than the wait answers 202; a fast one answers 200 as before.
func TestAdminSyncSlowAnswers202(t *testing.T) {
	e := newEnv(t)
	e.m.syncWait = 20 * time.Millisecond
	release := make(chan struct{})
	e.gh.set(func(f *fakeGitHub) { f.block = release })
	var out struct{ Data map[string]string }
	if code := e.do(e.admin, "POST", "/projects/sync", nil, &out); code != http.StatusAccepted || out.Data["status"] != "running" {
		t.Fatalf("slow sync: %d %+v", code, out.Data)
	}
	close(release)
	e.s.mu.Lock()
	e.s.mu.Unlock()
	e.gh.set(func(f *fakeGitHub) { f.block = nil })
	if res := e.sync(); res.Total != 3 {
		t.Fatalf("fast sync %+v", res)
	}
}
