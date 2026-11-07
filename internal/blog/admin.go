package blog

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/markdown"
)

// Admin mounts the post editor routes.
func (m *Module) Admin(r chi.Router) {
	r.Get("/posts", m.adminList)
	r.Post("/posts", m.adminCreate)
	r.Post("/posts/preview", m.adminPreview)
	r.Get("/posts/{id}", m.adminGet)
	r.Put("/posts/{id}", m.adminUpdate)
	r.Delete("/posts/{id}", m.adminDelete)
}

func (m *Module) adminList(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	if _, ok := stateSQL[state]; !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "state must be draft, scheduled or published")
		return
	}
	category, ok := categoryParam(r.URL.Query().Get("category"))
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "category must be tech or life")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if runes(q) > 200 {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "q is too long")
		return
	}
	posts, err := m.listAdmin(r.Context(), state, category, q)
	if err != nil {
		m.internal(w, r, "blog: admin list", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, posts)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func (m *Module) adminGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := m.getAdmin(r.Context(), m.db, id)
	m.respond(w, r, http.StatusOK, p, err)
}

func (m *Module) adminCreate(w http.ResponseWriter, r *http.Request) {
	var in PostInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	p, err := m.create(r.Context(), in)
	m.respond(w, r, http.StatusCreated, p, err)
}

func (m *Module) adminUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in PostInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	p, err := m.update(r.Context(), id, in)
	m.respond(w, r, http.StatusOK, p, err)
}

func (m *Module) adminDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	found, err := m.delete(r.Context(), id)
	if err != nil {
		m.internal(w, r, "blog: delete", err)
		return
	}
	if !found {
		httpapi.Error(w, http.StatusNotFound, "not_found", "post not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) adminPreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Markdown string `json:"markdown"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if runes(in.Markdown) > 200_000 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", "markdown must be at most 200000 characters")
		return
	}
	res, err := markdown.Render(in.Markdown, markdown.Options{Trusted: true})
	if err != nil {
		m.internal(w, r, "blog: preview", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, res)
}

// respond maps store errors to the shared error codes.
func (m *Module) respond(w http.ResponseWriter, r *http.Request, status int, p AdminPost, err error) {
	var ve *ValidationError
	switch {
	case err == nil:
		httpapi.JSON(w, status, p)
	case errors.As(err, &ve):
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", ve.Msg)
	case errors.Is(err, ErrSlugTaken):
		httpapi.Error(w, http.StatusConflict, "slug_taken", "that slug is already used by another post")
	case errors.Is(err, ErrNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "post not found")
	default:
		m.internal(w, r, "blog: save", err)
	}
}
