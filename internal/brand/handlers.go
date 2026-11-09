package brand

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Asset is BrandAsset in types.ts.
type Asset struct {
	Slot        string `json:"slot"`
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Custom      bool   `json:"custom"`
}

// AdminAsset is AdminBrandAsset in types.ts.
type AdminAsset struct {
	Asset
	Label      string     `json:"label"`
	Kind       string     `json:"kind"`
	SizeBytes  int64      `json:"size_bytes"`
	Original   string     `json:"original"`
	UpdatedAt  *time.Time `json:"updated_at"`
	DefaultURL string     `json:"default_url"`
	Accept     []string   `json:"accept"`
	Rule       string     `json:"rule"`
	Preview    string     `json:"preview"`
}

// Brand is Brand in types.ts.
type Brand struct {
	Assets map[string]Asset `json:"assets"`
}

func (s *Store) asset(st State, def slotDef) Asset {
	if c, ok := st.Custom[def.Slot]; ok {
		return Asset{Slot: def.Slot, URL: s.files.URL(c.Key), ContentType: def.Type, Width: c.Width, Height: c.Height, Custom: true}
	}
	d := s.defaults[def.Slot]
	return Asset{Slot: def.Slot, URL: s.files.URL(d.key), ContentType: def.Type, Width: d.info.Width, Height: d.info.Height}
}

func (s *Store) admin(st State, def slotDef) AdminAsset {
	a := AdminAsset{Asset: s.asset(st, def), Label: def.Label, Kind: def.Kind, DefaultURL: s.files.URL(s.defaults[def.Slot].key),
		Accept: def.Accept, Rule: def.Rule, Preview: def.Preview, SizeBytes: int64(len(s.defaults[def.Slot].data))}
	if c, ok := st.Custom[def.Slot]; ok {
		at := c.UpdatedAt.UTC()
		a.SizeBytes, a.Original, a.UpdatedAt = c.SizeBytes, c.Original, &at
	}
	return a
}

// AbsoluteAsset returns slot's current file with an absolute http(s) URL,
// for places outside the site such as emails. A media URL that is already
// absolute (MEDIA_PUBLIC_URL) is used as is; a site-relative one ("/media/…")
// gets siteURL in front. On a database error it falls back to the default.
func (s *Store) AbsoluteAsset(ctx context.Context, slot, siteURL string) (Asset, error) {
	def, ok := slotByName(slot)
	if !ok {
		return Asset{}, ErrUnknownSlot
	}
	st, err := s.state(ctx)
	if err != nil {
		s.log.WarnContext(ctx, "brand: read state, using the default", logging.Err(err))
	}
	a := s.asset(st, def)
	if strings.HasPrefix(a.URL, "/") && !strings.HasPrefix(a.URL, "//") {
		// The site's origin (any path, query or fragment dropped) + the path.
		site, err := url.Parse(strings.TrimSpace(siteURL))
		if err != nil || !absoluteHTTP(site.String()) {
			return Asset{}, fmt.Errorf("brand: %s: the site URL is not an absolute http(s) URL", slot)
		}
		ref, err := url.Parse(a.URL)
		if err != nil {
			return Asset{}, fmt.Errorf("brand: %s: %w", slot, err)
		}
		a.URL = site.ResolveReference(ref).String()
	}
	if !absoluteHTTP(a.URL) {
		return Asset{}, fmt.Errorf("brand: %s has no absolute http(s) URL", slot)
	}
	return a, nil
}

// absoluteHTTP reports whether u is an absolute http(s) URL with a host and
// no credentials.
func absoluteHTTP(u string) bool {
	p, err := url.Parse(u)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != "" && p.User == nil
}

// Brand returns the public view of every slot.
func (s *Store) Brand(st State) Brand {
	b := Brand{Assets: make(map[string]Asset, len(slots))}
	for _, def := range slots {
		b.Assets[def.Slot] = s.asset(st, def)
	}
	return b
}

// Hooks mounts nothing.
func (s *Store) Hooks(chi.Router) {}

// Public mounts GET /brand and GET /brand/{slot}.
func (s *Store) Public(r chi.Router) {
	r.Get("/brand", s.handlePublic)
	r.Get("/brand/{slot}", s.handleRedirect)
}

// Admin mounts GET /brand, PUT and DELETE /brand/{slot}.
func (s *Store) Admin(r chi.Router) {
	r.Get("/brand", s.handleAdminList)
	r.Put("/brand/{slot}", s.handlePut)
	r.Delete("/brand/{slot}", s.handleReset)
}

// publicState never fails: on a database error the defaults are served.
func (s *Store) publicState(r *http.Request) State {
	st, err := s.state(r.Context())
	if err != nil {
		s.log.WarnContext(r.Context(), "brand: read state, serving defaults", logging.Err(err))
	}
	return st
}

func (s *Store) handlePublic(w http.ResponseWriter, r *http.Request) {
	b := s.Brand(s.publicState(r))
	w.Header().Set("Cache-Control", "public, max-age=60")
	httpapi.JSON(w, http.StatusOK, b)
}

func (s *Store) handleRedirect(w http.ResponseWriter, r *http.Request) {
	def, ok := slotByName(chi.URLParam(r, "slot"))
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "unknown brand slot")
		return
	}
	a := s.asset(s.publicState(r), def)
	w.Header().Set("Cache-Control", "public, max-age=300")
	http.Redirect(w, r, a.URL, http.StatusFound)
}

func (s *Store) handleAdminList(w http.ResponseWriter, r *http.Request) {
	st, err := s.state(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]AdminAsset, 0, len(slots))
	for _, def := range slots {
		out = append(out, s.admin(st, def))
	}
	httpapi.JSON(w, http.StatusOK, out)
}

const maxRequest = 1 << 20

func (s *Store) handlePut(w http.ResponseWriter, r *http.Request) {
	def, ok := slotByName(chi.URLParam(r, "slot"))
	if !ok {
		httpapi.Error(w, http.StatusNotFound, "not_found", "unknown brand slot")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequest)
	mr, err := r.MultipartReader()
	if err != nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "expected a multipart/form-data body")
		return
	}
	var data []byte
	var name string
	found := false
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
		if part.FormName() != "file" || found {
			_ = part.Close()
			continue
		}
		found, name = true, part.FileName()
		data, err = io.ReadAll(io.LimitReader(part, int64(def.Max)+1))
		_ = part.Close()
		if tooLarge(err) || len(data) > def.Max {
			s.fail(w, r, &Problem{Status: http.StatusRequestEntityTooLarge, Code: "too_large",
				Message: def.Slot + " must be at most " + strconv.Itoa(def.Max>>10) + " KiB"})
			return
		}
		if err != nil {
			httpapi.Error(w, http.StatusBadRequest, "bad_request", "could not read the file")
			return
		}
	}
	if !found {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "missing multipart field \"file\"")
		return
	}
	a, err := s.Replace(r.Context(), def.Slot, name, data)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, a)
}

func (s *Store) handleReset(w http.ResponseWriter, r *http.Request) {
	a, err := s.Reset(r.Context(), chi.URLParam(r, "slot"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, a)
}

func (s *Store) fail(w http.ResponseWriter, r *http.Request, err error) {
	var p *Problem
	switch {
	case errors.As(err, &p):
		httpapi.Error(w, p.Status, p.Code, p.Message)
	case errors.Is(err, ErrUnknownSlot):
		httpapi.Error(w, http.StatusNotFound, "not_found", "unknown brand slot")
	case errors.Is(err, ErrStorage):
		s.log.ErrorContext(r.Context(), "brand: storage", logging.Err(err))
		httpapi.Error(w, http.StatusServiceUnavailable, "storage_unavailable", "media storage is unavailable, try again later")
	default:
		s.log.ErrorContext(r.Context(), "brand: internal", logging.Err(err))
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func tooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}

// cleanOriginal keeps the base name, without control characters, ≤ 200 runes.
func cleanOriginal(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	return strings.TrimSpace(name)
}
