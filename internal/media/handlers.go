package media

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

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
	r.Get("/media/{id}", s.get)
	r.Post("/media/{id}/retry", s.retry)
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

var cropFields = [4]string{"crop_x", "crop_y", "crop_w", "crop_h"}

// upload handles POST /media: multipart "file" plus optional crop_x,
// crop_y, crop_w, crop_h (all four or none), in any order. The file is
// staged first, so crop fields may follow it.
func (s *Store) upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "expected a multipart/form-data body")
		return
	}
	var staged *os.File
	var name string
	defer func() {
		if staged != nil {
			_ = staged.Close()
			_ = os.Remove(staged.Name())
		}
	}()
	vals := map[string]string{}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if tooLarge(err) {
				httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "file is too large")
				return
			}
			httpapi.Error(w, http.StatusBadRequest, "bad_request", "malformed multipart body")
			return
		}
		field := part.FormName()
		switch {
		case field == "file" && staged == nil:
			f, err := os.CreateTemp(s.dir, ".rq-*")
			if err != nil {
				_ = part.Close()
				s.internal(w, r, "media: temp file", err)
				return
			}
			staged, name = f, part.FileName()
			n, err := io.Copy(f, io.LimitReader(part, s.maxBytes+1))
			_ = part.Close()
			if tooLarge(err) || n > s.maxBytes {
				httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "file is too large")
				return
			}
			if err != nil {
				httpapi.Error(w, http.StatusBadRequest, "bad_request", "could not read the file")
				return
			}
		case strings.HasPrefix(field, "crop_"):
			b, err := io.ReadAll(io.LimitReader(part, 32))
			_ = part.Close()
			if err != nil {
				httpapi.Error(w, http.StatusBadRequest, "bad_request", "malformed multipart body")
				return
			}
			vals[field] = strings.TrimSpace(string(b))
		default:
			_ = part.Close()
		}
	}
	if staged == nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "missing multipart field \"file\"")
		return
	}
	crop, msg := parseCrop(vals)
	if msg != "" {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", msg)
		return
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		s.internal(w, r, "media: rewind", err)
		return
	}
	m, err := s.SaveCrop(r.Context(), name, staged, crop)
	if err != nil {
		s.saveError(w, r, err)
		return
	}
	if m.Status == StatusProcessing && s.uploadWait > 0 {
		if m, err = s.WaitDone(r.Context(), m.ID, s.uploadWait); err != nil {
			s.internal(w, r, "media: wait", err)
			return
		}
	}
	if m.Status == StatusReady {
		httpapi.JSON(w, http.StatusCreated, m)
		return
	}
	httpapi.JSON(w, http.StatusAccepted, m)
}

// parseCrop reads the four crop fields; all or none must be present.
func parseCrop(v map[string]string) (*Crop, string) {
	present := 0
	var n [4]int
	for i, k := range cropFields {
		s, ok := v[k]
		if !ok || s == "" {
			continue
		}
		present++
		x, err := strconv.Atoi(s)
		if err != nil || x < 0 || x > 1_000_000 {
			return nil, k + " must be a non-negative integer"
		}
		n[i] = x
	}
	switch present {
	case 0:
		return nil, ""
	case 4:
		if n[2] == 0 || n[3] == 0 {
			return nil, "crop_w and crop_h must be at least 1"
		}
		return &Crop{X: n[0], Y: n[1], W: n[2], H: n[3]}, ""
	}
	return nil, "crop needs all of crop_x, crop_y, crop_w and crop_h"
}

func (s *Store) saveError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTooLarge) || tooLarge(err):
		httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "file is too large")
	case errors.Is(err, ErrTooManyPixels):
		httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large",
			"the image has too many pixels (limit "+strconv.FormatInt(s.maxPixels, 10)+")")
	case errors.Is(err, ErrUnsupported):
		httpapi.Error(w, http.StatusUnsupportedMediaType, "unsupported_type",
			"only JPEG, PNG, GIF, WebP, HEIC, BMP, TIFF, AVIF, SVG and PDF files are allowed")
	case errors.Is(err, ErrUndecodable):
		httpapi.Error(w, http.StatusUnsupportedMediaType, "unsupported_type",
			"the image could not be decoded (corrupt or unsupported variant)")
	case errors.Is(err, ErrBadCrop):
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "the crop rectangle is outside the image")
	case errors.Is(err, ErrOriginalsUnavailable):
		s.log.ErrorContext(r.Context(), "media: originals store", logging.Err(err))
		httpapi.Error(w, http.StatusServiceUnavailable, "storage_unavailable", "media storage is unavailable, try again later")
	default:
		s.internal(w, r, "media: save", err)
	}
}

func tooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Store) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	m, err := s.Get(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "media not found")
		return
	}
	if err != nil {
		s.internal(w, r, "media: get", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, m)
}

func (s *Store) retry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid id")
		return
	}
	m, err := s.Retry(r.Context(), id)
	switch {
	case err == nil:
		httpapi.JSON(w, http.StatusAccepted, m)
	case errors.Is(err, pgx.ErrNoRows):
		httpapi.Error(w, http.StatusNotFound, "not_found", "media not found")
	case errors.Is(err, ErrNotFailed):
		httpapi.Error(w, http.StatusConflict, "conflict", "only failed media can be retried")
	case errors.Is(err, ErrGone):
		httpapi.Error(w, http.StatusGone, "gone", "the original is no longer kept; upload the file again")
	default:
		s.internal(w, r, "media: retry", err)
	}
}

func (s *Store) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
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

// Handler serves GET/HEAD /media/{name}: from disk, streamed from the
// bucket, or (bucket with a public domain) as a 301 to that domain so old
// /media links keep working. Only valid media filenames are accepted, so the
// redirect can never point anywhere else.
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
		switch {
		case s.s3 != nil && s.public != "":
			w.Header().Set("Cache-Control", "public, max-age=86400")
			http.Redirect(w, r, s.URL(name), http.StatusMovedPermanently)
		case s.s3 != nil:
			s.serveObject(w, r, name)
		default:
			s.serveFile(w, r, name)
		}
	})
}

// setFileHeaders sets the headers every served media file gets.
func setFileHeaders(h http.Header, ext string) {
	h.Set("Content-Type", typeByExt[ext])
	h.Set("Cache-Control", immutableCache)
	h.Set("X-Content-Type-Options", "nosniff")
	switch ext {
	case ".svg":
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	case ".pdf":
		h.Set("Content-Disposition", "inline")
	}
}

func (s *Store) serveFile(w http.ResponseWriter, r *http.Request, name string) {
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
	setFileHeaders(w.Header(), filepath.Ext(name))
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// serveObject streams name from the bucket (no public domain configured).
// Range and If-None-Match are passed through to the bucket.
func (s *Store) serveObject(w http.ResponseWriter, r *http.Request, name string) {
	head := r.Method == http.MethodHead
	obj, err := s.s3.get(r.Context(), name, r.Header.Get("Range"), r.Header.Get("If-None-Match"), head)
	if err != nil {
		switch code := httpStatus(err); {
		case code == http.StatusNotModified:
			w.Header().Set("Cache-Control", immutableCache)
			w.WriteHeader(http.StatusNotModified)
		case isNotFound(err):
			httpapi.Error(w, http.StatusNotFound, "not_found", "no such file")
		case code == http.StatusRequestedRangeNotSatisfiable:
			httpapi.Error(w, code, "bad_range", "range not satisfiable")
		default:
			s.log.ErrorContext(r.Context(), "media: get object", slog.String("filename", name), logging.Err(err))
			httpapi.Error(w, http.StatusBadGateway, "storage_unavailable", "storage unavailable")
		}
		return
	}
	if obj.Body != nil {
		defer func() { _ = obj.Body.Close() }()
	}
	h := w.Header()
	setFileHeaders(h, filepath.Ext(name))
	h.Set("Accept-Ranges", "bytes")
	h.Set("Content-Length", strconv.FormatInt(obj.ContentLength, 10))
	if obj.ETag != "" {
		h.Set("ETag", obj.ETag)
	}
	if !obj.LastModified.IsZero() {
		h.Set("Last-Modified", obj.LastModified.UTC().Format(http.TimeFormat))
	}
	status := http.StatusOK
	if obj.ContentRange != "" {
		h.Set("Content-Range", obj.ContentRange)
		status = http.StatusPartialContent
	}
	w.WriteHeader(status)
	if obj.Body != nil {
		if _, err := io.Copy(w, obj.Body); err != nil {
			s.log.DebugContext(r.Context(), "media: stream object", logging.Err(err))
		}
	}
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
