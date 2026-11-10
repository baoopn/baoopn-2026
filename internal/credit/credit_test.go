package credit

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/brand"
	"github.com/baoopn/baoopn-2026/internal/settings"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// fakeBrand records the slots asked for.
type fakeBrand struct {
	mu    sync.Mutex
	slots []string
	err   error
}

func (b *fakeBrand) AbsoluteAsset(_ context.Context, slot, site string) (brand.Asset, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.slots = append(b.slots, slot)
	if b.err != nil {
		return brand.Asset{}, b.err
	}
	return brand.Asset{Slot: slot, URL: site + "/media/brand/" + slot + ".svg", ContentType: "image/svg+xml",
		Width: len(slot), Height: 10}, nil
}

func (b *fakeBrand) take() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.slots
	b.slots = nil
	return s
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// newTest builds a module whose snapshot is d, so no database is needed.
func newTest(d *Doc) (*Module, *fakeBrand) {
	b := &fakeBrand{}
	m := New(Deps{Log: quiet(), Brand: b, PublicURL: "https://www.baoopn.com/"})
	if d != nil {
		m.snap.Store(d)
	}
	return m, b
}

func TestLogoSlots(t *testing.T) {
	m, b := newTest(nil)
	for kind, want := range map[string]string{
		LogoWordmark: "wordmark_primary wordmark_primary",
		LogoMark:     "monogram_primary monogram_inverse",
	} {
		l, err := m.logo(t.Context(), kind)
		if err != nil || l.Kind != kind {
			t.Fatalf("logo(%s) = %+v %v", kind, l, err)
		}
		if got := strings.Join(b.take(), " "); got != want {
			t.Errorf("%s slots = %q, want %q", kind, got, want)
		}
		if !strings.HasSuffix(l.Light.URL, strings.Fields(want)[0]+".svg") ||
			!strings.HasSuffix(l.Dark.URL, strings.Fields(want)[1]+".svg") || l.Light.ContentType != "image/svg+xml" {
			t.Errorf("%s = %+v", kind, l)
		}
	}
	a, err := m.admin(t.Context(), m.defaults())
	if err != nil || len(a.Logos) != 2 || a.Logos[LogoMark].Dark.Width != len("monogram_inverse") {
		t.Errorf("admin logos = %+v %v", a.Logos, err)
	}
	if a.Endpoint != "https://www.baoopn.com/api/v1/credit" || a.Limits != (Limits{80, 500, 50}) {
		t.Errorf("admin = %+v", a)
	}
}

func TestDefaultsAndLenientParse(t *testing.T) {
	m, _ := newTest(nil)
	def := m.defaults()
	if def.Text != "Designed & built by" || def.URL != "https://www.baoopn.com" || def.Logo != LogoWordmark ||
		def.Origins == nil || len(def.Origins) != 0 || def.UpdatedAt != nil {
		t.Fatalf("defaults = %+v", def)
	}
	if m.alt() != "baoopn.com" {
		t.Errorf("alt = %q", m.alt())
	}
	for _, raw := range []string{"", "null", "[]", "not json"} {
		if d := parseDoc([]byte(raw), def); d.Text != def.Text || d.URL != def.URL || d.Origins == nil {
			t.Errorf("parseDoc(%q) = %+v", raw, d)
		}
	}
	d := parseDoc([]byte(`{"text": 7, "url": "http://x.com", "logo": "big",
		"origins": ["HTTPS://A.com", 3, "http://b.com", "https://a.com/"], "updated_at": "nope"}`), def)
	if d.Text != def.Text || d.URL != def.URL || d.Logo != LogoWordmark ||
		strings.Join(d.Origins, ",") != "https://a.com" || d.UpdatedAt != nil {
		t.Errorf("broken doc = %+v", d)
	}
	d = parseDoc([]byte(`{"text": "Made by", "url": "https://x.com/a", "logo": "mark", "origins": "x",
		"updated_at": "2026-11-09T17:00:00+07:00"}`), def)
	if d.Text != "Made by" || d.URL != "https://x.com/a" || d.Logo != LogoMark || len(d.Origins) != 0 ||
		d.UpdatedAt == nil || !d.UpdatedAt.Equal(time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("good doc = %+v", d)
	}
}

func serve(m *Module, method, origin string) *httptest.ResponseRecorder {
	r := chi.NewRouter()
	m.Public(r)
	req := httptest.NewRequest(method, "/credit", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCORS(t *testing.T) {
	m, _ := newTest(&Doc{Text: "x", URL: "https://baoopn.com", Logo: LogoMark,
		Origins: []string{"https://tlportraitz.com", "http://localhost:5173"}})
	for _, c := range []struct{ origin, want string }{
		{"https://tlportraitz.com", "https://tlportraitz.com"},
		{"HTTPS://TLPortraitz.com", "https://tlportraitz.com"},
		{"http://localhost:5173", "http://localhost:5173"},
		{"https://evil.com", ""},
		{"https://tlportraitz.com.evil.com", ""},
		{"null", ""},
		{"", ""},
	} {
		w := serve(m, "GET", c.origin)
		h := w.Header()
		if w.Code != 200 || h.Get("Access-Control-Allow-Origin") != c.want || h.Get("Vary") != "Origin" ||
			h.Get("Cache-Control") != "public, max-age=300" || !strings.HasPrefix(h.Get("Content-Type"), "application/json") ||
			h.Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("GET origin %q = %d %v", c.origin, w.Code, h)
		}
		var env struct{ Data Credit }
		if json.Unmarshal(w.Body.Bytes(), &env) != nil || env.Data.Logo.Kind != LogoMark || env.Data.Alt != "baoopn.com" {
			t.Errorf("GET origin %q body = %s", c.origin, w.Body)
		}

		w = serve(m, "OPTIONS", c.origin)
		h = w.Header()
		if w.Code != 204 || w.Body.Len() != 0 ||
			h.Get("Vary") != "Origin, Access-Control-Request-Method, Access-Control-Request-Headers" ||
			h.Get("Access-Control-Allow-Origin") != c.want || h.Get("Access-Control-Allow-Credentials") != "" {
			t.Errorf("OPTIONS origin %q = %d %v", c.origin, w.Code, h)
		}
		if c.want != "" && (h.Get("Access-Control-Allow-Methods") != "GET, OPTIONS" ||
			h.Get("Access-Control-Allow-Headers") != "Content-Type" || h.Get("Access-Control-Max-Age") != "86400") {
			t.Errorf("OPTIONS origin %q = %v", c.origin, h)
		}
		if c.want == "" {
			for k := range h {
				if strings.HasPrefix(k, "Access-Control-") {
					t.Errorf("OPTIONS origin %q has %s", c.origin, k)
				}
			}
		}
	}
}

func TestPublicBrandError(t *testing.T) {
	m, b := newTest(&Doc{Text: "x", URL: "https://baoopn.com", Logo: LogoWordmark, Origins: []string{}})
	b.err = errors.New("bad site URL")
	if w := serve(m, "GET", ""); w.Code != 500 || strings.Contains(w.Header().Get("Cache-Control"), "public") {
		t.Errorf("brand error = %d %v", w.Code, w.Header())
	}
}

func TestLoadAndSave(t *testing.T) {
	db := testdb.New(t)
	ctx := t.Context()
	b := &fakeBrand{}
	m := New(Deps{DB: db, Log: quiet(), Brand: b, PublicURL: "https://baoopn.com"})
	d, err := m.load(ctx)
	if err != nil || d.Text != defaultText || d.URL != "https://baoopn.com" || len(d.Origins) != 0 {
		t.Fatalf("load missing = %+v %v", d, err)
	}
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	want := Doc{Text: "Made by", URL: "https://x.com", Logo: LogoMark, Origins: []string{"https://a.com"}, UpdatedAt: &at}
	if err := m.save(ctx, want); err != nil {
		t.Fatal(err)
	}
	if d, _ := m.load(ctx); d.Text != "Made by" {
		t.Errorf("snapshot after save = %+v", d)
	}
	// A new module reads the stored document.
	m2 := New(Deps{DB: db, Log: quiet(), Brand: b, PublicURL: "https://baoopn.com"})
	d, err = m2.load(ctx)
	if err != nil || d.Logo != LogoMark || strings.Join(d.Origins, ",") != "https://a.com" || !d.UpdatedAt.Equal(at) {
		t.Errorf("reload = %+v %v", d, err)
	}
	// A broken hand edit falls back per field.
	if err := settings.Put(ctx, db, Key, map[string]any{"text": "", "logo": "mark"}); err != nil {
		t.Fatal(err)
	}
	m3 := New(Deps{DB: db, Log: quiet(), Brand: b, PublicURL: "https://baoopn.com"})
	if d, _ := m3.load(ctx); d.Text != defaultText || d.Logo != LogoMark {
		t.Errorf("hand edit = %+v", d)
	}
}
