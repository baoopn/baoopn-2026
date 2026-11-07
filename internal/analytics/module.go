package analytics

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Public mounts POST /api/v1/events.
func (r *Recorder) Public(rt chi.Router) { rt.Post("/events", r.EventsHandler) }

// Admin mounts GET /api/admin/analytics.
func (r *Recorder) Admin(rt chi.Router) { rt.Get("/analytics", r.reportHandler) }

// Hooks mounts nothing.
func (r *Recorder) Hooks(chi.Router) {}

func (r *Recorder) reportHandler(w http.ResponseWriter, req *http.Request) {
	q := req.URL.Query()
	loc, err := LoadZone(q.Get("tz"))
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_tz", err.Error())
		return
	}
	rep, err := r.Report(req.Context(), q.Get("range"), loc)
	switch {
	case errors.Is(err, ErrBadRange):
		httpapi.Error(w, http.StatusBadRequest, "bad_range", err.Error())
	case err != nil:
		r.log.ErrorContext(req.Context(), "analytics report", logging.Err(err))
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
	default:
		httpapi.JSON(w, http.StatusOK, rep)
	}
}
