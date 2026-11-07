package media

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Public implements httpapi.Module (no public API routes).
func (s *Store) Public(chi.Router) {}

// Hooks implements httpapi.Module (no hooks).
func (s *Store) Hooks(chi.Router) {}

// Admin mounts the media library routes.
func (s *Store) Admin(r chi.Router) {
	r.Get("/media", s.list)
	r.Post("/media", s.upload)
	r.Delete("/media/{id}", s.remove)
}

func (s *Store) list(w http.ResponseWriter, r *http.Request) {
	page, ok := intParam(r, "page", 1, 1, 1_000_000)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "page must be a positive integer")
		return
	}
	limit, ok := intParam(r, "limit", 40, 1, 100)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "limit must be between 1 and 100")
		return
	}
	items, total, err := s.List(r.Context(), page, limit)
	if err != nil {
		s.internal(w, r, "media: list", err)
		return
	}
	httpapi.JSONMeta(w, http.StatusOK, items, httpapi.PageMeta{Total: total, Page: page, Limit: limit})
}

func (s *Store) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "expected a multipart/form-data body")
		return
	}
	for {
		part, err := mr.NextPart()
		if err != nil {
			if tooLarge(err) {
				httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "file is too large")
				return
			}
			httpapi.Error(w, http.StatusBadRequest, "bad_request", "missing multipart field \"file\"")
			return
		}
		if part.FormName() != "file" {
			_ = part.Close()
			continue
		}
		m, err := s.Save(r.Context(), part.FileName(), part)
		_ = part.Close()
		switch {
		case err == nil:
			httpapi.JSON(w, http.StatusCreated, m)
		case errors.Is(err, ErrTooLarge) || tooLarge(err):
			httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "file is too large")
		case errors.Is(err, ErrUnsupported):
			httpapi.Error(w, http.StatusUnsupportedMediaType, "unsupported_type",
				"only PNG, JPEG, GIF, WebP, AVIF, SVG and PDF files are allowed")
		default:
			s.internal(w, r, "media: save", err)
		}
		return
	}
}

func tooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func (s *Store) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	found, err := s.Delete(r.Context(), id)
	if err != nil {
		s.internal(w, r, "media: delete", err)
		return
	}
	if !found {
		httpapi.Error(w, http.StatusNotFound, "not_found", "media not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Store) internal(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.log.ErrorContext(r.Context(), msg, logging.Err(err))
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
}

var fileRe = regexp.MustCompile(`^[0-9a-f]{24}\.(png|jpg|gif|webp|avif|svg|pdf)$`)

// Handler serves GET/HEAD /media/{name} straight from disk.
func (s *Store) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			httpapi.Error(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/media/")
		if !fileRe.MatchString(name) {
			httpapi.Error(w, http.StatusNotFound, "not_found", "no such file")
			return
		}
		f, err := os.Open(filepath.Join(s.dir, name))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				s.log.ErrorContext(r.Context(), "media: open", logging.Err(err))
			}
			httpapi.Error(w, http.StatusNotFound, "not_found", "no such file")
			return
		}
		defer func() { _ = f.Close() }()
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			httpapi.Error(w, http.StatusNotFound, "not_found", "no such file")
			return
		}
		ext := filepath.Ext(name)
		h := w.Header()
		h.Set("Content-Type", typeByExt[ext])
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("X-Content-Type-Options", "nosniff")
		switch ext {
		case ".svg":
			h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
		case ".pdf":
			h.Set("Content-Disposition", "inline")
		}
		http.ServeContent(w, r, name, st.ModTime(), f)
	})
}

// intParam parses an optional integer query parameter within [lo, hi].
func intParam(r *http.Request, key string, def, lo, hi int) (int, bool) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, true
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, false
	}
	return n, true
}
