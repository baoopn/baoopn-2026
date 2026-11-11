package projects

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
// request timeout), so the upsert is not rolled back mid-way.
func TestAdminSyncIgnoresRequestCancel(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	e.admin.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, "POST", "/projects/sync", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("sync with cancelled request: %d %s", rec.Code, rec.Body)
	}
	var out struct{ Data SyncResult }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Data.Total != 3 {
		t.Fatalf("result %+v %v", out.Data, err)
	}
}
