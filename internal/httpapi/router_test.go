package httpapi

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// deadlineModule reports whether each route's request context has a deadline.
type deadlineModule struct{}

func hasDeadline(w http.ResponseWriter, r *http.Request) {
	if _, ok := r.Context().Deadline(); ok {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (deadlineModule) Public(r chi.Router)  { r.Get("/plain", hasDeadline) }
func (deadlineModule) Admin(chi.Router)     {}
func (deadlineModule) Hooks(chi.Router)     {}
func (deadlineModule) Streams(r chi.Router) { r.Get("/stream", hasDeadline) }

// Public routes keep the 20 s request timeout; Streamer routes do not get it.
func TestStreamerRoutesSkipTimeout(t *testing.T) {
	h := New(Deps{Log: slog.New(slog.DiscardHandler), PublicRPM: 100, Modules: []Module{deadlineModule{}}})
	for path, want := range map[string]int{"/api/v1/plain": http.StatusOK, "/api/v1/stream": http.StatusNoContent} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", path, rec.Code, want)
		}
	}
}

// adminDeadlineModule has one Admin route and one AdminStreamer route.
type adminDeadlineModule struct{}

func (adminDeadlineModule) Public(chi.Router)         {}
func (adminDeadlineModule) Admin(r chi.Router)        { r.Get("/plain", hasDeadline) }
func (adminDeadlineModule) Hooks(chi.Router)          {}
func (adminDeadlineModule) AdminStreams(r chi.Router) { r.Get("/stream", hasDeadline) }

// Admin routes keep the 60 s timeout; AdminStreamer routes do not get it, but
// both sit behind AdminAuth.
func TestAdminStreamerRoutes(t *testing.T) {
	authed := false
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer ok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			authed = true
			next.ServeHTTP(w, r)
		})
	}
	h := New(Deps{Log: slog.New(slog.DiscardHandler), AdminAuth: auth, Modules: []Module{adminDeadlineModule{}}})
	for path, want := range map[string]int{"/api/admin/plain": http.StatusOK, "/api/admin/stream": http.StatusNoContent} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without auth: %d, want 401", path, rec.Code)
		}
		authed = false
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer ok")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want || !authed {
			t.Errorf("%s: %d (authed %v), want %d", path, rec.Code, authed, want)
		}
	}

	// Without auth configured the stream is closed like every admin route.
	h = New(Deps{Log: slog.New(slog.DiscardHandler), Modules: []Module{adminDeadlineModule{}}})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/admin/stream", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("stream with admin disabled: %d, want 503", rec.Code)
	}
}
