package profile

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Public mounts GET /profile.
func (m *Module) Public(r chi.Router) {
	r.Get("/profile", m.get)
}

// Hooks implements httpapi.Module (no hooks).
func (m *Module) Hooks(chi.Router) {}

// Admin mounts GET/PUT /profile and POST /profile/github.
func (m *Module) Admin(r chi.Router) {
	r.Get("/profile", m.get)
	r.Put("/profile", m.put)
	r.Post("/profile/github", m.github)
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	p, err := m.Get(r.Context())
	if err != nil {
		m.internal(w, r, "profile: get", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p)
}

func (m *Module) put(w http.ResponseWriter, r *http.Request) {
	var in Profile
	if !httpapi.Decode(w, r, &in) {
		return
	}
	p, err := m.save(r.Context(), in)
	var ve *ValidationError
	switch {
	case err == nil:
		httpapi.JSON(w, http.StatusOK, p)
	case errors.As(err, &ve):
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", ve.Msg)
	default:
		m.internal(w, r, "profile: save", err)
	}
}

func (m *Module) github(w http.ResponseWriter, r *http.Request) {
	if m.ghUser == "" {
		httpapi.Error(w, http.StatusServiceUnavailable, "github_disabled", "GITHUB_USER is not configured")
		return
	}
	u, err := m.fetchGitHub(r.Context())
	var rl *rateLimitError
	switch {
	case err == nil:
		httpapi.JSON(w, http.StatusOK, partialFromGitHub(u))
	case errors.As(err, &rl):
		httpapi.Error(w, http.StatusServiceUnavailable, "github_rate_limited",
			"GitHub rate limit reached; resets at "+rl.reset.UTC().Format(time.RFC3339))
	case errors.Is(err, errUpstream):
		m.log.WarnContext(r.Context(), "profile: github", logging.Err(err))
		httpapi.Error(w, http.StatusBadGateway, "github_unavailable", "GitHub is unavailable, try again later")
	default:
		m.internal(w, r, "profile: github", err)
	}
}

func (m *Module) internal(w http.ResponseWriter, r *http.Request, msg string, err error) {
	m.log.ErrorContext(r.Context(), msg, logging.Err(err))
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
}
