package blog

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Public mounts /posts, /posts/legacy, /posts/{slug} and /tags.
func (m *Module) Public(r chi.Router) {
	r.Get("/posts", m.publicList)
	r.Get("/posts/legacy", m.publicLegacy)
	r.Get("/posts/{slug}", m.publicDetail)
	r.Get("/tags", m.publicTags)
}

// Hooks implements httpapi.Module (no hooks).
func (m *Module) Hooks(chi.Router) {}

func (m *Module) publicList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, ok := intParam(q.Get("page"), 1, 1, 1_000_000)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "page must be a positive integer")
		return
	}
	limit, ok := intParam(q.Get("limit"), 10, 1, 50)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 50")
		return
	}
	f := publicFilter{
		Tag:    strings.ToLower(strings.TrimSpace(q.Get("tag"))),
		Series: strings.TrimSpace(q.Get("series")),
		Q:      strings.TrimSpace(q.Get("q")),
		Page:   page, Limit: limit,
	}
	if len(f.Tag) > 30 || len(f.Series) > 80 || runes(f.Q) > 200 {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "filter value too long")
		return
	}
	posts, total, err := m.listPublic(r.Context(), f)
	if err != nil {
		m.internal(w, r, "blog: list", err)
		return
	}
	httpapi.JSONMeta(w, http.StatusOK, posts, httpapi.PageMeta{Total: total, Page: page, Limit: limit})
}

func (m *Module) publicDetail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if len(slug) > 80 || !slugRe.MatchString(slug) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "post not found")
		return
	}
	d, err := m.detail(r.Context(), slug)
	if errors.Is(err, ErrNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "post not found")
		return
	}
	if err != nil {
		m.internal(w, r, "blog: detail", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, d)
}

// normalizeLegacy lowercases a v3 path and strips a trailing slash.
func normalizeLegacy(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p
}

func (m *Module) publicLegacy(w http.ResponseWriter, r *http.Request) {
	p := normalizeLegacy(r.URL.Query().Get("path"))
	if p == "" || len(p) > 200 || !strings.HasPrefix(p, "/") {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "path must be an absolute path")
		return
	}
	slug, err := m.legacySlug(r.Context(), p)
	if errors.Is(err, ErrNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no post for that path")
		return
	}
	if err != nil {
		m.internal(w, r, "blog: legacy", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]string{"slug": slug})
}

func (m *Module) publicTags(w http.ResponseWriter, r *http.Request) {
	tags, err := m.tags(r.Context())
	if err != nil {
		m.internal(w, r, "blog: tags", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, tags)
}

func (m *Module) internal(w http.ResponseWriter, r *http.Request, msg string, err error) {
	m.log.ErrorContext(r.Context(), msg, logging.Err(err))
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
}

func intParam(v string, def, lo, hi int) (int, bool) {
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, false
	}
	return n, true
}
