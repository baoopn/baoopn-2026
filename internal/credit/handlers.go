package credit

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Public mounts GET and OPTIONS /credit.
func (m *Module) Public(r chi.Router) {
	r.Get("/credit", m.handlePublic)
	r.Options("/credit", m.handlePreflight)
}

// Admin mounts GET and PUT /credit.
func (m *Module) Admin(r chi.Router) {
	r.Get("/credit", m.handleAdminGet)
	r.Put("/credit", m.handlePut)
}

// Hooks mounts nothing.
func (m *Module) Hooks(chi.Router) {}

func (m *Module) internal(w http.ResponseWriter, r *http.Request, what string, err error) {
	m.d.Log.ErrorContext(r.Context(), "credit: "+what, logging.Err(err))
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
}

// allowedOrigin returns the request's Origin when it is in the list, else "".
// Only exact matches count; "null" never does.
func allowedOrigin(r *http.Request, origins []string) string {
	o := strings.ToLower(strings.TrimSpace(r.Header.Get("Origin")))
	if o == "" || o == "null" {
		return ""
	}
	for _, s := range origins {
		if s == o {
			return o
		}
	}
	return ""
}

// publicDoc never fails: on a database error it serves the defaults, which
// allow no origin, and a shorter cache time.
func (m *Module) publicDoc(r *http.Request) (Doc, string) {
	d, err := m.load(r.Context())
	if err != nil {
		m.d.Log.WarnContext(r.Context(), "credit: read settings, serving defaults", logging.Err(err))
		return m.defaults(), "public, max-age=60"
	}
	return d, "public, max-age=300"
}

func (m *Module) handlePublic(w http.ResponseWriter, r *http.Request) {
	d, cache := m.publicDoc(r)
	h := w.Header()
	h.Set("Vary", "Origin")
	c, err := m.public(r.Context(), d)
	if err != nil {
		// No CORS header and no caching: a client page sees a failed fetch
		// and keeps its fallback text.
		h.Set("Cache-Control", "no-store")
		m.internal(w, r, "brand logo", err)
		return
	}
	if o := allowedOrigin(r, d.Origins); o != "" {
		h.Set("Access-Control-Allow-Origin", o)
	}
	h.Set("Cache-Control", cache)
	httpapi.JSON(w, http.StatusOK, c)
}

func (m *Module) handlePreflight(w http.ResponseWriter, r *http.Request) {
	d, _ := m.publicDoc(r)
	h := w.Header()
	h.Set("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
	if o := allowedOrigin(r, d.Origins); o != "" {
		h.Set("Access-Control-Allow-Origin", o)
		h.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Content-Type")
		h.Set("Access-Control-Max-Age", "86400")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) respondAdmin(w http.ResponseWriter, r *http.Request, d Doc) {
	a, err := m.admin(r.Context(), d)
	if err != nil {
		m.internal(w, r, "brand logo", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, a)
}

func (m *Module) handleAdminGet(w http.ResponseWriter, r *http.Request) {
	d, err := m.load(r.Context())
	if err != nil {
		m.internal(w, r, "read settings", err)
		return
	}
	m.respondAdmin(w, r, d)
}

func (m *Module) handlePut(w http.ResponseWriter, r *http.Request) {
	var in Input
	if !httpapi.Decode(w, r, &in) {
		return
	}
	d, problem := in.validate()
	if problem != "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", problem)
		return
	}
	at := m.d.Now().UTC().Truncate(time.Second)
	d.UpdatedAt = &at
	if err := m.save(r.Context(), d); err != nil {
		m.internal(w, r, "save settings", err)
		return
	}
	m.respondAdmin(w, r, d)
}
