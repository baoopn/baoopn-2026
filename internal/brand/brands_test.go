package brand

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// Baoopn shorthands for the older tests.
func slotByName(name string) (slotDef, bool)    { return baoopnBrand.slot(name) }
func keyFor(d slotDef, data []byte) string      { return baoopnBrand.keyFor(d, data) }
func defaultFile(d slotDef) ([]byte, error)     { return baoopnBrand.defaultFile(d) }
func listeningSlot(name string) (slotDef, bool) { return listeningBrand.slot(name) }

// newPair is a baoopn and a listening store sharing one file storage, each
// with its own settings document.
func newPair(t *testing.T) (main, lis *Store, f *fakeFiles, mainDoc, lisDoc *memDoc, c *clock) {
	t.Helper()
	f = newFakeFiles()
	c = &clock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	m := NewModule(Deps{Log: slog.New(slog.DiscardHandler), Files: f, Now: c.now})
	return m.Main, m.Listening, f, attachDoc(m.Main), attachDoc(m.Listening), c
}

func TestBrandTables(t *testing.T) {
	for _, b := range brands {
		if err := b.check(); err != nil {
			t.Errorf("%s: %v", b.Name, err)
		}
	}
	if _, ok := baoopnBrand.slot(Listening); ok {
		t.Error("baoopn has a slot named listening")
	}
	if len(slots) != 12 || len(listeningSlots) != 31 {
		t.Errorf("slots = %d and %d, want 12 and 31", len(slots), len(listeningSlots))
	}
	want := map[string]int{"favicons": 9, "logos": 8, "icons": 12, "social": 1, "email": 1}
	got := map[string]int{}
	for _, d := range listeningSlots {
		got[d.Group]++
		if !slices.Contains([]string{"icon", "logo", "image"}, d.Kind) {
			t.Errorf("%s: kind %q", d.Slot, d.Kind)
		}
	}
	for g, n := range want {
		if got[g] != n {
			t.Errorf("group %s = %d slots, want %d", g, got[g], n)
		}
	}
	// Slots in both brands share the rule text, except the email logo's
	// display note.
	for _, d := range listeningSlots {
		if o, ok := baoopnBrand.slot(d.Slot); ok && d.Slot != EmailLogo && o.Rule != d.Rule {
			t.Errorf("%s: rule %q differs from baoopn %q", d.Slot, d.Rule, o.Rule)
		}
	}
	d, _ := listeningSlot("icon_now_playing_navy")
	if d.File != "icon-now-playing-navy.svg" || d.Stem != "icon-now-playing-navy" || d.Label != "Now playing (animated) icon (navy)" ||
		d.Preview != "light" || !d.Square {
		t.Errorf("icon_now_playing_navy = %+v", d)
	}
}

func TestCheckRejectsBadTables(t *testing.T) {
	ok := listeningSlots[0]
	for name, b := range map[string]*brandDef{
		"listening slot": {Name: "x", KeyPrefix: "brand/", Slots: []slotDef{{Slot: Listening, Stem: "a", Ext: "png", Group: "logos"}}},
		"dup stem":       {Name: "x", KeyPrefix: "brand/", Slots: []slotDef{ok, {Slot: "other", Stem: ok.Stem, Ext: ok.Ext, Group: "email"}}},
		"dup slot":       {Name: "x", KeyPrefix: "brand/", Slots: []slotDef{ok, ok}},
		"group order":    {Name: "x", KeyPrefix: "brand/", Slots: []slotDef{{Slot: "a", Stem: "a", Ext: "png", Group: "email"}, {Slot: "b", Stem: "b", Ext: "png", Group: "logos"}}},
		"bad group":      {Name: "x", KeyPrefix: "brand/", Slots: []slotDef{{Slot: "a", Stem: "a", Ext: "png", Group: "misc"}}},
		"bad stem":       {Name: "x", KeyPrefix: "brand/", Slots: []slotDef{{Slot: "a", Stem: "A_b", Ext: "png", Group: "logos"}}},
	} {
		if name == "listening slot" {
			// Only the baoopn table may not use the name.
			saved := baoopnBrand.Slots
			baoopnBrand.Slots = b.Slots
			err := baoopnBrand.check()
			baoopnBrand.Slots = saved
			if err == nil {
				t.Errorf("%s: accepted", name)
			}
			continue
		}
		if err := b.check(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOwnsKey(t *testing.T) {
	const h = ".0123456789abcdef."
	for _, tc := range []struct {
		key             string
		main, listening bool
	}{
		{"brand/favicon" + h + "ico", true, false},
		{"brand/listening/favicon" + h + "ico", false, true},
		{"brand/listening/listening/favicon" + h + "ico", false, false},
		{"brand/other/favicon" + h + "ico", false, false},
		{"brand/listening" + h + "png", true, false}, // a root key whose stem is "listening"
		{"brand/listening/../favicon" + h + "ico", false, false},
		{"favicon" + h + "ico", false, false},
	} {
		if got := baoopnBrand.owns(tc.key); got != tc.main {
			t.Errorf("baoopn owns %s = %v", tc.key, got)
		}
		if got := listeningBrand.owns(tc.key); got != tc.listening {
			t.Errorf("listening owns %s = %v", tc.key, got)
		}
	}
}

func TestListeningStore(t *testing.T) {
	ctx := context.Background()
	main, lis, f, mainDoc, lisDoc, _ := newPair(t)
	lis.Ensure(ctx)
	if f.puts != len(listeningSlots) {
		t.Fatalf("puts = %d, want %d", f.puts, len(listeningSlots))
	}
	for _, d := range listeningSlots {
		k := lis.defaults[d.Slot].key
		if !strings.HasPrefix(k, "brand/listening/") || !f.has(k) {
			t.Errorf("%s: default %q not uploaded under brand/listening/", d.Slot, k)
		}
	}
	main.Ensure(ctx)
	if f.puts != len(listeningSlots)+len(slots) {
		t.Fatalf("puts after baoopn = %d", f.puts)
	}

	a, err := lis.Replace(ctx, "icon_192", "app.png", pngN(192))
	if err != nil || !a.Custom || a.Group != "favicons" || !strings.HasPrefix(a.URL, "https://media.example.com/brand/listening/icon-192.") {
		t.Fatalf("replace = %+v %v", a, err)
	}
	if lisDoc.saves != 1 || mainDoc.saves != 0 || len(mainDoc.st.Custom) != 0 {
		t.Fatalf("saves: listening %d baoopn %d", lisDoc.saves, mainDoc.saves)
	}
	if _, err := lis.Replace(ctx, MonogramPrimary, "x.svg", nil); err != ErrUnknownSlot {
		t.Errorf("baoopn-only slot on listening: %v", err)
	}
	if _, err := main.Replace(ctx, "icon_192", "x.png", pngN(192)); err != ErrUnknownSlot {
		t.Errorf("listening-only slot on baoopn: %v", err)
	}
	if a, err := lis.Reset(ctx, "icon_192"); err != nil || a.Custom {
		t.Fatalf("reset = %+v %v", a, err)
	}
	if lisDoc.saves != 2 || mainDoc.saves != 0 || len(lisDoc.st.Retired) != 1 {
		t.Fatalf("after reset: listening saves %d retired %v, baoopn saves %d", lisDoc.saves, lisDoc.st.Retired, mainDoc.saves)
	}
	b := lis.Brand(State{})
	if len(b.Assets) != len(listeningSlots) {
		t.Errorf("listening brand = %d assets", len(b.Assets))
	}
}

// TestBrandIsolation: a store drops the other brand's keys from its
// document and never deletes the other brand's files.
func TestBrandIsolation(t *testing.T) {
	ctx := context.Background()
	main, lis, f, mainDoc, lisDoc, c := newPair(t)
	main.Ensure(ctx)
	lis.Ensure(ctx)
	lisKey := lis.defaults[FaviconICO].key
	mainKey := main.defaults[FaviconICO].key
	old := c.t.Add(-30 * 24 * time.Hour)

	mainDoc.st = State{Custom: map[string]Custom{FaviconICO: {Key: lisKey, ContentType: typeICO}},
		Retired: []Retired{{Key: lisKey, RetiredAt: old}}}
	lisDoc.st = State{Custom: map[string]Custom{FaviconICO: {Key: mainKey, ContentType: typeICO}},
		Retired: []Retired{{Key: mainKey, RetiredAt: old}}}
	main.snap.Store(nil)
	lis.snap.Store(nil)
	main.prune(ctx)
	lis.prune(ctx)
	if !f.has(lisKey) || !f.has(mainKey) || len(f.deleted) != 0 {
		t.Fatalf("deleted another brand's default: %v", f.deleted)
	}
	if len(mainDoc.st.Retired) != 0 || len(lisDoc.st.Retired) != 0 {
		t.Errorf("foreign keys left in retired: %v %v", mainDoc.st.Retired, lisDoc.st.Retired)
	}
	if a := main.asset(main.publicState(httptest.NewRequest("GET", "/", nil)), mustSlot(t, FaviconICO)); a.Custom {
		t.Errorf("baoopn took a listening key as its override: %+v", a)
	}
	ld, _ := listeningSlot(FaviconICO)
	if a := lis.asset(lis.publicState(httptest.NewRequest("GET", "/", nil)), ld); a.Custom {
		t.Errorf("listening took a baoopn key as its override: %+v", a)
	}
}

func TestModuleRoutes(t *testing.T) {
	main, lis, _, _, _, _ := newPair(t)
	m := &Module{Main: main, Listening: lis}
	r := chi.NewRouter()
	r.Route("/v1", m.Public)
	r.Route("/admin", m.Admin)
	do := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	w := do("GET", "/v1/brand/listening")
	var b struct{ Data Brand }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &b) != nil || len(b.Data.Assets) != len(listeningSlots) ||
		w.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("GET listening = %d %s", w.Code, w.Body)
	}
	if w := do("GET", "/v1/brand"); w.Code != 200 || !strings.Contains(w.Body.String(), `"monogram_primary"`) ||
		strings.Contains(w.Body.String(), `"icon_192"`) {
		t.Errorf("GET baoopn = %d", w.Code)
	}
	if w := do("GET", "/v1/brand/listening/icon_192"); w.Code != http.StatusFound ||
		!strings.Contains(w.Header().Get("Location"), "/brand/listening/icon-192.") {
		t.Errorf("redirect = %d %v", w.Code, w.Header())
	}
	for _, p := range []string{"/v1/brand/listening/monogram_primary", "/v1/brand/listening/", "/v1/brand/icon_192"} {
		if w := do("GET", p); w.Code != 404 {
			t.Errorf("GET %s = %d", p, w.Code)
		}
	}
	if w := do("GET", "/admin/brand/listening"); w.Code != 200 || !strings.Contains(w.Body.String(), `"group":"icons"`) {
		t.Errorf("admin list = %d", w.Code)
	}
	for _, mp := range [][2]string{{"PUT", "/admin/brand/listening"}, {"DELETE", "/admin/brand/listening"},
		{"DELETE", "/admin/brand/listening/monogram_primary"}} {
		if w := do(mp[0], mp[1]); w.Code != 404 {
			t.Errorf("%s %s = %d %s", mp[0], mp[1], w.Code, w.Body)
		}
	}
}
