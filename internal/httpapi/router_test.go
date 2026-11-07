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
