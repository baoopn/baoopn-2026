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
	for _, c := range []struct{ kind, style, want string }{
		{LogoWordmark, StyleColor, "wordmark_primary wordmark_inverse"},
		{LogoWordmark, StyleMono, "wordmark_navy wordmark_cream"},
		{LogoMark, StyleColor, "monogram_primary monogram_inverse"},
		{LogoMark, StyleMono, "monogram_navy monogram_cream"},
	} {
		l, err := m.logo(t.Context(), c.kind, c.style)
		if err != nil || l.Kind != c.kind || l.Style != c.style {
			t.Fatalf("logo(%s, %s) = %+v %v", c.kind, c.style, l, err)
		}
		if got := strings.Join(b.take(), " "); got != c.want {
			t.Errorf("%s/%s slots = %q, want %q", c.kind, c.style, got, c.want)
		}
		if !strings.HasSuffix(l.Light.URL, strings.Fields(c.want)[0]+".svg") ||
			!strings.HasSuffix(l.Dark.URL, strings.Fields(c.want)[1]+".svg") || l.Light.ContentType != "image/svg+xml" {
			t.Errorf("%s/%s = %+v", c.kind, c.style, l)
		}
	}
	a, err := m.admin(t.Context(), m.defaults())
	if err != nil || len(a.Logos) != 2 || a.Logos[LogoMark][StyleColor].Dark.Width != len("monogram_inverse") {
		t.Fatalf("admin logos = %+v %v", a.Logos, err)
	}
	for _, kind := range kinds {
		if len(a.Logos[kind]) != 2 {
			t.Errorf("admin logos[%s] = %+v", kind, a.Logos[kind])
		}
		for _, style := range []string{StyleColor, StyleMono} {
			if l := a.Logos[kind][style]; l.Kind != kind || l.Style != style {
				t.Errorf("admin logos[%s][%s] = %+v", kind, style, l)
			}
		}
		if v := a.Variants[kind]; v != (Variant{Prefix: defaultPrefix, Style: StyleColor}) {
			t.Errorf("admin variants[%s] = %+v", kind, v)
		}
	}
	if a.Endpoint != "https://www.baoopn.com/api/v1/credit" || a.Limits != (Limits{80, 80, 500, 50}) {
		t.Errorf("admin = %+v", a)
	}
}

func TestDefaultsAndLenientParse(t *testing.T) {
	m, _ := newTest(nil)
	def := m.defaults()
	dv := Variant{Prefix: "Designed & built by", Style: StyleColor}
	if def.URL != "https://www.baoopn.com" || def.Logo != LogoWordmark || len(def.Variants) != 2 ||
		def.Variants[LogoWordmark] != dv || def.Variants[LogoMark] != dv ||
		def.Origins == nil || len(def.Origins) != 0 || def.UpdatedAt != nil {
		t.Fatalf("defaults = %+v", def)
	}
	if m.alt() != "baoopn.com" {
		t.Errorf("alt = %q", m.alt())
	}
	for _, raw := range []string{"", "null", "[]", "not json"} {
		if d := parseDoc([]byte(raw), def); d.Variants[LogoMark] != dv || d.URL != def.URL || d.Origins == nil {
			t.Errorf("parseDoc(%q) = %+v", raw, d)
		}
	}
	d := parseDoc([]byte(`{"text": 7, "url": "http://x.com", "logo": "big", "variants": 3,
		"origins": ["HTTPS://A.com", 3, "http://b.com", "https://a.com/"], "updated_at": "nope"}`), def)
	if d.Variants[LogoWordmark] != dv || d.Variants[LogoMark] != dv || d.URL != def.URL || d.Logo != LogoWordmark ||
		strings.Join(d.Origins, ",") != "https://a.com" || d.UpdatedAt != nil {
		t.Errorf("broken doc = %+v", d)
	}
	d = parseDoc([]byte(`{"url": "https://x.com/a", "logo": "mark", "origins": "x",
		"variants": {"wordmark": {"prefix": "Made by", "suffix": " with care ", "style": "mono"},
			"mark": {"prefix": "", "suffix": "", "style": "color"}},
		"updated_at": "2026-11-09T17:00:00+07:00"}`), def)
	if d.Variants[LogoWordmark] != (Variant{"Made by", "with care", StyleMono}) ||
		d.Variants[LogoMark] != (Variant{"", "", StyleColor}) ||
		d.URL != "https://x.com/a" || d.Logo != LogoMark || len(d.Origins) != 0 ||
		d.UpdatedAt == nil || !d.UpdatedAt.Equal(time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("good doc = %+v", d)
	}

	// A document from before the variants: text is the prefix of both.
	d = parseDoc([]byte(`{"text": "by", "logo": "mark"}`), def)
	if want := (Variant{"by", "", StyleColor}); d.Variants[LogoWordmark] != want || d.Variants[LogoMark] != want ||
		d.Logo != LogoMark {
		t.Errorf("legacy doc = %+v", d)
	}
	// Only mark stored: wordmark gets the legacy text, or the default line.
	d = parseDoc([]byte(`{"text": "by", "variants": {"mark": {"prefix": "x", "style": "mono"}}}`), def)
	if d.Variants[LogoWordmark] != (Variant{"by", "", StyleColor}) || d.Variants[LogoMark] != (Variant{"x", "", StyleMono}) {
		t.Errorf("mark only = %+v", d.Variants)
	}
	d = parseDoc([]byte(`{"variants": {"mark": {"prefix": "x"}}}`), def)
	if d.Variants[LogoWordmark] != dv || d.Variants[LogoMark] != (Variant{"x", "", StyleColor}) {
		t.Errorf("mark only, no text = %+v", d.Variants)
	}
	// Each broken field falls back on its own, in its own variant only.
	d = parseDoc([]byte(`{"variants": {
		"wordmark": {"prefix": "a\u202e", "suffix": "a\nb", "style": "big"},
		"mark": {"prefix": "by", "suffix": "baoopn", "style": "mono"}}}`), def)
	if d.Variants[LogoWordmark] != dv || d.Variants[LogoMark] != (Variant{"by", "baoopn", StyleMono}) {
		t.Errorf("broken fields = %+v", d.Variants)
	}
	d = parseDoc([]byte(`{"variants": {"wordmark": {"prefix": 3, "suffix": "x", "style": "MONO"}, "mark": null}}`), def)
	if d.Variants[LogoWordmark] != (Variant{defaultPrefix, "x", StyleColor}) || d.Variants[LogoMark] != dv {
		t.Errorf("wrong types = %+v", d.Variants)
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
	m, _ := newTest(&Doc{URL: "https://baoopn.com", Logo: LogoMark, Variants: defaultVariants("x"),
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
	m, b := newTest(&Doc{URL: "https://baoopn.com", Logo: LogoWordmark, Variants: defaultVariants("x"),
		Origins: []string{"https://a.com"}})
	b.err = errors.New("bad site URL")
	w := serve(m, "GET", "https://a.com")
	if w.Code != 500 || w.Header().Get("Cache-Control") != "no-store" ||
		w.Header().Get("Access-Control-Allow-Origin") != "" || w.Header().Get("Vary") != "Origin" {
		t.Errorf("brand error = %d %v", w.Code, w.Header())
	}
}

// TestPublicReadError: a failed read serves the defaults for 60 s with no
// CORS header, and the next request reads again.
func TestPublicReadError(t *testing.T) {
	m, _ := newTest(nil)
	fail := true
	reads := 0
	m.read = func(context.Context) ([]byte, error) {
		reads++
		if fail {
			return nil, errors.New("db down")
		}
		return []byte(`{"text":"Made by","url":"https://x.com","logo":"mark","origins":["https://a.com"]}`), nil
	}
	w := serve(m, "GET", "https://a.com")
	h := w.Header()
	var env struct{ Data Credit }
	if w.Code != 200 || h.Get("Cache-Control") != "public, max-age=60" || h.Get("Access-Control-Allow-Origin") != "" ||
		h.Get("Vary") != "Origin" || json.Unmarshal(w.Body.Bytes(), &env) != nil ||
		env.Data.Text != defaultPrefix || env.Data.Prefix != defaultPrefix || env.Data.URL != "https://www.baoopn.com" || env.Data.Logo.Kind != LogoWordmark {
		t.Errorf("GET on read error = %d %v %s", w.Code, h, w.Body)
	}
	w = serve(m, "OPTIONS", "https://a.com")
	if w.Code != 204 || w.Body.Len() != 0 {
		t.Errorf("OPTIONS on read error = %d", w.Code)
	}
	for k := range w.Header() {
		if strings.HasPrefix(k, "Access-Control-") {
			t.Errorf("OPTIONS on read error has %s", k)
		}
	}
	if m.snap.Load() != nil || reads != 2 {
		t.Fatalf("snapshot %v after %d failed reads, want nil", m.snap.Load(), reads)
	}
	// The database is back: the next request reads it and lists the origin.
	fail = false
	w = serve(m, "GET", "https://a.com")
	if w.Code != 200 || w.Header().Get("Access-Control-Allow-Origin") != "https://a.com" ||
		w.Header().Get("Cache-Control") != "public, max-age=300" || reads != 3 || m.snap.Load() == nil {
		t.Errorf("GET after recovery = %d %v, reads %d", w.Code, w.Header(), reads)
	}
	serve(m, "GET", "https://a.com")
	if reads != 3 {
		t.Errorf("reads = %d, want the snapshot to serve the 4th request", reads)
	}
}

// TestSaveDuringFirstLoad: a save that finishes while the first read is in
// flight wins over that older read.
func TestSaveDuringFirstLoad(t *testing.T) {
	m, _ := newTest(nil)
	entered, release := make(chan struct{}), make(chan struct{})
	m.read = func(context.Context) ([]byte, error) {
		close(entered)
		<-release
		return []byte(`{"text":"Old","url":"https://old.com","logo":"wordmark","origins":[]}`), nil
	}
	var written []Doc
	m.write = func(_ context.Context, d Doc) error { written = append(written, d); return nil }

	got := make(chan Doc, 1)
	go func() {
		d, err := m.load(context.Background())
		if err != nil {
			t.Error(err)
		}
		got <- d
	}()
	<-entered
	saved := Doc{URL: "https://new.com", Logo: LogoMark, Variants: defaultVariants("New"), Origins: []string{"https://a.com"}}
	if err := m.save(t.Context(), saved); err != nil {
		t.Fatal(err)
	}
	close(release)
	if d := <-got; d.variant(LogoMark).Prefix != "New" || d.Logo != LogoMark {
		t.Errorf("slow load returned %+v, want the saved doc", d)
	}
	if d := m.snap.Load(); d == nil || d.variant(LogoMark).Prefix != "New" || len(written) != 1 {
		t.Errorf("snapshot = %+v, writes %d", d, len(written))
	}
}

func TestLoadAndSave(t *testing.T) {
	db := testdb.New(t)
	ctx := t.Context()
	b := &fakeBrand{}
	m := New(Deps{DB: db, Log: quiet(), Brand: b, PublicURL: "https://baoopn.com"})
	d, err := m.load(ctx)
	if err != nil || d.variant(LogoWordmark).Prefix != defaultPrefix || d.URL != "https://baoopn.com" || len(d.Origins) != 0 {
		t.Fatalf("load missing = %+v %v", d, err)
	}
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	want := Doc{URL: "https://x.com", Logo: LogoMark, Origins: []string{"https://a.com"}, UpdatedAt: &at,
		Variants: map[string]Variant{LogoWordmark: {"Made by", "", StyleColor}, LogoMark: {"by", "baoopn", StyleMono}}}
	if err := m.save(ctx, want); err != nil {
		t.Fatal(err)
	}
	if d, _ := m.load(ctx); d.variant(LogoWordmark).Prefix != "Made by" {
		t.Errorf("snapshot after save = %+v", d)
	}
	// A new module reads the stored document.
	m2 := New(Deps{DB: db, Log: quiet(), Brand: b, PublicURL: "https://baoopn.com"})
	d, err = m2.load(ctx)
	if err != nil || d.Logo != LogoMark || strings.Join(d.Origins, ",") != "https://a.com" || !d.UpdatedAt.Equal(at) ||
		d.Variants[LogoWordmark] != want.Variants[LogoWordmark] || d.Variants[LogoMark] != want.Variants[LogoMark] {
		t.Errorf("reload = %+v %v", d, err)
	}
	// A broken hand edit falls back per field.
	if err := settings.Put(ctx, db, Key, map[string]any{"text": 7, "logo": "mark"}); err != nil {
		t.Fatal(err)
	}
	m3 := New(Deps{DB: db, Log: quiet(), Brand: b, PublicURL: "https://baoopn.com"})
	if d, _ := m3.load(ctx); d.variant(LogoMark).Prefix != defaultPrefix || d.Logo != LogoMark {
		t.Errorf("hand edit = %+v", d)
	}
}

// ?logo= picks the logo kind for one client; a bad value keeps the stored one.
func TestPublicLogoParam(t *testing.T) {
	m, _ := newTest(&Doc{URL: "https://baoopn.com", Logo: LogoWordmark, Variants: defaultVariants("x"), Origins: []string{}})
	r := chi.NewRouter()
	m.Public(r)
	for q, want := range map[string]string{
		"": LogoWordmark, "?logo=mark": LogoMark, "?logo=wordmark": LogoWordmark,
		"?logo=big": LogoWordmark, "?logo=MARK": LogoWordmark,
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/credit"+q, nil))
		var body struct {
			Data Credit `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
			t.Fatalf("%q: %d %v", q, w.Code, err)
		}
		if body.Data.Logo.Kind != want {
			t.Errorf("%q: kind = %q, want %q", q, body.Data.Logo.Kind, want)
		}
	}
	if d := m.snap.Load(); d.Logo != LogoWordmark {
		t.Errorf("stored logo changed to %q", d.Logo)
	}
}

// ?logo= picks the variant: its prefix, suffix and style.
func TestPublicVariants(t *testing.T) {
	m, _ := newTest(&Doc{URL: "https://baoopn.com", Logo: LogoWordmark, Origins: []string{},
		Variants: map[string]Variant{
			LogoWordmark: {Prefix: "Designed & built by", Style: StyleColor},
			LogoMark:     {Prefix: "by", Suffix: "baoopn", Style: StyleMono},
		}})
	r := chi.NewRouter()
	m.Public(r)
	get := func(q string) Credit {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/credit"+q, nil))
		var body struct {
			Data Credit `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
			t.Fatalf("%q: %d %v", q, w.Code, err)
		}
		return body.Data
	}
	for _, q := range []string{"", "?logo=MARK", "?logo=wordmark"} {
		c := get(q)
		if c.Prefix != "Designed & built by" || c.Text != c.Prefix || c.Suffix != "" ||
			c.Logo.Kind != LogoWordmark || c.Logo.Style != StyleColor ||
			!strings.HasSuffix(c.Logo.Dark.URL, "/wordmark_inverse.svg") {
			t.Errorf("%q = %+v", q, c)
		}
	}
	c := get("?logo=mark")
	if c.Prefix != "by" || c.Text != "by" || c.Suffix != "baoopn" || c.Logo.Kind != LogoMark || c.Logo.Style != StyleMono ||
		!strings.HasSuffix(c.Logo.Light.URL, "/monogram_navy.svg") || !strings.HasSuffix(c.Logo.Dark.URL, "/monogram_cream.svg") {
		t.Errorf("?logo=mark = %+v", c)
	}
	if d := m.snap.Load(); d.Logo != LogoWordmark || d.Variants[LogoMark].Style != StyleMono {
		t.Errorf("stored doc changed: %+v", d)
	}
}

// ?color= overrides the chosen variant's style for one request only.
func TestPublicColorParam(t *testing.T) {
	m, _ := newTest(&Doc{URL: "https://baoopn.com", Logo: LogoWordmark, Origins: []string{},
		Variants: map[string]Variant{
			LogoWordmark: {Prefix: "x", Style: StyleColor},
			LogoMark:     {Prefix: "by", Suffix: "baoopn", Style: StyleMono},
		}})
	r := chi.NewRouter()
	m.Public(r)
	for _, c := range []struct{ q, kind, style, light string }{
		{"", LogoWordmark, StyleColor, "wordmark_primary"},
		{"?color=mono", LogoWordmark, StyleMono, "wordmark_navy"},
		{"?color=MONO", LogoWordmark, StyleColor, "wordmark_primary"},
		{"?color=big", LogoWordmark, StyleColor, "wordmark_primary"},
		{"?logo=mark", LogoMark, StyleMono, "monogram_navy"},
		{"?logo=mark&color=color", LogoMark, StyleColor, "monogram_primary"},
		{"?logo=mark&color=mono", LogoMark, StyleMono, "monogram_navy"},
		{"?logo=mark&color=Color", LogoMark, StyleMono, "monogram_navy"},
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/credit"+c.q, nil))
		var body struct {
			Data Credit `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 {
			t.Fatalf("%q: %d %v", c.q, w.Code, err)
		}
		l := body.Data.Logo
		if l.Kind != c.kind || l.Style != c.style || !strings.HasSuffix(l.Light.URL, "/"+c.light+".svg") ||
			w.Header().Get("Cache-Control") != "public, max-age=300" {
			t.Errorf("%q = %+v", c.q, l)
		}
	}
	if d := m.snap.Load(); d.Logo != LogoWordmark || d.Variants[LogoWordmark].Style != StyleColor ||
		d.Variants[LogoMark].Style != StyleMono {
		t.Errorf("stored doc changed: %+v", d)
	}
}
