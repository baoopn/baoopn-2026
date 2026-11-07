package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// MEDIA_ORPHAN_GRACE=0 must turn the cleanup off, not fall back to the
// store's 7-day default.
func TestOrphanGraceZeroDisables(t *testing.T) {
	if g := orphanGrace(0); g >= 0 {
		t.Fatalf("orphanGrace(0) = %v, want negative (disabled)", g)
	}
	if g := orphanGrace(48 * time.Hour); g != 48*time.Hour {
		t.Fatalf("orphanGrace(48h) = %v", g)
	}
	s, err := media.New(media.Deps{DB: testdb.New(t), Log: slog.New(slog.DiscardHandler), Dir: t.TempDir(),
		OrphanGrace: orphanGrace(0)})
	if err != nil {
		t.Fatal(err)
	}
	if s.OrphansEnabled() {
		t.Fatal("cleanup enabled with MEDIA_ORPHAN_GRACE=0")
	}
	r := chi.NewRouter()
	s.Admin(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("POST", "/media/orphans", strings.NewReader(`{}`)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"disabled"`) {
		t.Fatalf("POST = %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/media/orphans", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"enabled":false`) {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
}
