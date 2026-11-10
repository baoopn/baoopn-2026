package brand

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"testing"
	"time"
)

// fakeFiles is an in-memory Files.
type fakeFiles struct {
	mu      sync.Mutex
	objs    map[string][]byte
	puts    int
	failPut error
	failHas map[string]error
	failDel map[string]error
	deleted []string
	// allMissing makes every Exists answer "not found" (a wrong bucket).
	allMissing bool
	// putHook runs at the start of every put, outside the lock.
	putHook func()
	// base prefixes URL; "" means the absolute media domain.
	base string
}

func newFakeFiles() *fakeFiles {
	return &fakeFiles{objs: map[string][]byte{}, failHas: map[string]error{}, failDel: map[string]error{}}
}

func (f *fakeFiles) PutBrandFile(_ context.Context, key, _ string, data []byte) error {
	if f.putHook != nil {
		f.putHook()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failPut != nil {
		return f.failPut
	}
	f.puts++
	f.objs[key] = append([]byte(nil), data...)
	return nil
}

func (f *fakeFiles) BrandFileExists(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failHas[key]; err != nil {
		return false, err
	}
	if f.allMissing {
		return false, nil
	}
	_, ok := f.objs[key]
	return ok, nil
}

func (f *fakeFiles) RemoveBrandFile(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.failDel[key]; err != nil {
		return err
	}
	delete(f.objs, key)
	f.deleted = append(f.deleted, key)
	return nil
}

func (f *fakeFiles) URL(key string) string {
	if f.base != "" {
		return f.base + key
	}
	return "https://media.example.com/" + key
}

func (f *fakeFiles) has(key string) bool {
	ok, _ := f.BrandFileExists(context.Background(), key)
	return ok
}

// memDoc is the settings document in memory.
type memDoc struct {
	st       State
	saves    int
	failSave error
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTestStore(t *testing.T) (*Store, *fakeFiles, *memDoc, *clock) {
	t.Helper()
	f := newFakeFiles()
	c := &clock{t: time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)}
	s := New(Deps{Log: slog.New(slog.DiscardHandler), Files: f, Now: c.now})
	doc := &memDoc{}
	s.load = func(context.Context) (State, error) { return doc.st.clone(), nil }
	s.save = func(_ context.Context, st State) error {
		if doc.failSave != nil {
			return doc.failSave
		}
		doc.saves++
		doc.st = st.clone()
		return nil
	}
	return s, f, doc, c
}

func TestEnsureUploadsDefaultsOnce(t *testing.T) {
	s, f, doc, _ := newTestStore(t)
	ctx := context.Background()
	s.Ensure(ctx)
	if f.puts != len(slots) {
		t.Fatalf("puts = %d, want %d", f.puts, len(slots))
	}
	for _, d := range slots {
		data, _ := defaultFile(d)
		if string(f.objs[s.defaults[d.Slot].key]) != string(data) {
			t.Errorf("%s: default not stored byte-exact", d.Slot)
		}
	}
	s.Ensure(ctx)
	if f.puts != len(slots) || doc.saves != 0 {
		t.Fatalf("second run: puts %d saves %d", f.puts, doc.saves)
	}
}

func TestEnsureCustomMissingOrError(t *testing.T) {
	s, f, doc, c := newTestStore(t)
	ctx := context.Background()
	gone := "brand/apple-touch-icon.0123456789abcdef.png"
	flaky := "brand/monogram-primary.0123456789abcdef.svg"
	doc.st = State{Custom: map[string]Custom{
		AppleTouchIcon:  {Key: gone, ContentType: typePNG, UpdatedAt: c.t},
		MonogramPrimary: {Key: flaky, ContentType: typeSVG, UpdatedAt: c.t},
	}}
	f.failHas[flaky] = errors.New("timeout")
	s.Ensure(ctx)
	if _, ok := doc.st.Custom[AppleTouchIcon]; ok {
		t.Error("override whose file is gone was kept")
	}
	if doc.st.Custom[MonogramPrimary].Key != flaky {
		t.Error("override dropped on a storage error")
	}
	if len(doc.st.Retired) != 1 || doc.st.Retired[0].Key != gone {
		t.Errorf("dropped key not retired: %+v", doc.st.Retired)
	}
}

// A bucket that answers "not found" for everything (wrong bucket, broken
// credentials behind a 404) must not wipe the overrides.
func TestEnsureKeepsOverridesWhenStorageUnconfirmed(t *testing.T) {
	custom := map[string]Custom{AppleTouchIcon: {Key: "brand/apple-touch-icon.0123456789abcdef.png", ContentType: typePNG}}
	for name, edit := range map[string]func(*fakeFiles, *Store){
		"404 and put fails": func(f *fakeFiles, _ *Store) { f.allMissing, f.failPut = true, errors.New("403") },
		"default check fails": func(f *fakeFiles, s *Store) {
			f.failHas[s.defaults[FaviconSVG].key] = errors.New("timeout")
		},
	} {
		s, f, doc, _ := newTestStore(t)
		doc.st = State{Custom: maps.Clone(custom)}
		edit(f, s)
		s.Ensure(context.Background())
		if doc.st.Custom[AppleTouchIcon].Key != custom[AppleTouchIcon].Key || doc.saves != 0 {
			t.Errorf("%s: overrides %+v, saves %d", name, doc.st.Custom, doc.saves)
		}
	}
}

func TestReplaceWithDefaultBytesResets(t *testing.T) {
	s, f, doc, _ := newTestStore(t)
	ctx := context.Background()
	s.Ensure(ctx)
	if _, err := s.Replace(ctx, AppleTouchIcon, "a.png", pngN(180)); err != nil {
		t.Fatal(err)
	}
	k := doc.st.Custom[AppleTouchIcon].Key
	data, _ := defaultFile(mustSlot(t, AppleTouchIcon))
	a, err := s.Replace(ctx, AppleTouchIcon, "default.png", data)
	if err != nil || a.Custom || a.URL != f.URL(s.defaults[AppleTouchIcon].key) || a.UpdatedAt != nil {
		t.Fatalf("replace with default bytes = %+v %v", a, err)
	}
	if _, ok := doc.st.Custom[AppleTouchIcon]; ok || len(doc.st.Retired) != 1 || doc.st.Retired[0].Key != k {
		t.Fatalf("state = %+v", doc.st)
	}
}

// Public reads use the snapshot and never wait for a writer.
func TestReadsDoNotWaitForWriters(t *testing.T) {
	s, f, _, _ := newTestStore(t)
	ctx := context.Background()
	s.Ensure(ctx)
	entered, release := make(chan struct{}), make(chan struct{})
	f.putHook = func() { close(entered); <-release }
	done := make(chan error, 1)
	go func() {
		_, err := s.Replace(ctx, AppleTouchIcon, "a.png", pngN(180))
		done <- err
	}()
	<-entered
	read := make(chan Brand, 1)
	go func() {
		st, _ := s.state(ctx)
		read <- s.Brand(st)
	}()
	select {
	case b := <-read:
		if b.Assets[AppleTouchIcon].Custom {
			t.Error("read saw the unsaved upload")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("public read waited for the writer")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if st, _ := s.state(ctx); !s.Brand(st).Assets[AppleTouchIcon].Custom {
		t.Error("snapshot not published after the write")
	}
}

func TestReplaceResetRetire(t *testing.T) {
	s, f, doc, c := newTestStore(t)
	ctx := context.Background()
	s.Ensure(ctx)
	def := s.defaults[AppleTouchIcon].key

	a, err := s.Replace(ctx, AppleTouchIcon, `C:\Users\me\icon.png`, pngN(180))
	if err != nil {
		t.Fatal(err)
	}
	k1 := doc.st.Custom[AppleTouchIcon].Key
	if !a.Custom || a.URL != f.URL(k1) || a.DefaultURL != f.URL(def) || a.Original != "icon.png" ||
		a.UpdatedAt == nil || a.Width != 180 || !f.has(k1) {
		t.Fatalf("replace = %+v", a)
	}
	saves := doc.saves
	if _, err := s.Replace(ctx, AppleTouchIcon, "again.png", pngN(180)); err != nil || doc.saves != saves {
		t.Fatalf("same bytes: saves %d -> %d, %v", saves, doc.saves, err)
	}

	img2 := pngN(180)
	img2 = append(img2[:len(img2):len(img2)], 0) // trailing byte: other hash, still decodes
	if _, err := s.Replace(ctx, AppleTouchIcon, "two.png", img2); err != nil {
		t.Fatal(err)
	}
	k2 := doc.st.Custom[AppleTouchIcon].Key
	if k2 == k1 || len(doc.st.Retired) != 1 || doc.st.Retired[0].Key != k1 {
		t.Fatalf("retired = %+v", doc.st.Retired)
	}

	r, err := s.Reset(ctx, AppleTouchIcon)
	if err != nil || r.Custom || r.URL != f.URL(def) || r.UpdatedAt != nil {
		t.Fatalf("reset = %+v %v", r, err)
	}
	if len(doc.st.Retired) != 2 || !f.has(k1) || !f.has(k2) {
		t.Fatalf("after reset retired %+v; files must stay", doc.st.Retired)
	}
	if _, err := s.Reset(ctx, AppleTouchIcon); err != nil {
		t.Fatalf("second reset: %v", err)
	}
	if _, err := s.Reset(ctx, "nope"); !errors.Is(err, ErrUnknownSlot) {
		t.Fatalf("unknown slot: %v", err)
	}

	// Six days on nothing goes; after seven the retired files are deleted.
	c.t = c.t.Add(6 * 24 * time.Hour)
	s.prune(ctx)
	if !f.has(k1) || !f.has(k2) {
		t.Fatal("deleted inside the 7 days")
	}
	c.t = c.t.Add(25 * time.Hour)
	f.failDel[k2] = errors.New("503")
	s.prune(ctx)
	if f.has(k1) || !f.has(k2) || len(doc.st.Retired) != 1 || doc.st.Retired[0].Key != k2 {
		t.Fatalf("prune: k1 %v k2 %v retired %+v", f.has(k1), f.has(k2), doc.st.Retired)
	}
	delete(f.failDel, k2)
	s.prune(ctx)
	if f.has(k2) || len(doc.st.Retired) != 0 || !f.has(def) {
		t.Fatalf("second prune: retired %+v", doc.st.Retired)
	}
}

func TestPruneNeverDeletesCurrentOrDefault(t *testing.T) {
	s, f, doc, c := newTestStore(t)
	ctx := context.Background()
	s.Ensure(ctx)
	defKey := s.defaults[MonogramPrimary].key
	defData, _ := defaultFile(mustSlot(t, MonogramPrimary))

	// A custom upload byte-identical to the default, then replaced: the
	// default key is retired but must never be deleted.
	if _, err := s.Replace(ctx, AppleTouchIcon, "a.png", pngN(180)); err != nil {
		t.Fatal(err)
	}
	cur := doc.st.Custom[AppleTouchIcon].Key
	s.mu.Lock()
	st := doc.st.clone()
	st.Custom[MonogramPrimary] = Custom{Key: defKey, ContentType: typeSVG, SizeBytes: int64(len(defData)), UpdatedAt: c.t}
	st.Retired = append(st.Retired, Retired{Key: cur, RetiredAt: c.t.Add(-30 * 24 * time.Hour)})
	doc.st = st
	s.snap.Store(nil)
	s.mu.Unlock()
	if _, err := s.Reset(ctx, MonogramPrimary); err != nil {
		t.Fatal(err)
	}
	c.t = c.t.Add(30 * 24 * time.Hour)
	s.prune(ctx)
	if !f.has(defKey) || !f.has(cur) {
		t.Fatalf("deleted a protected file: default %v current %v (deleted %v)", f.has(defKey), f.has(cur), f.deleted)
	}
	if len(doc.st.Retired) != 0 {
		t.Fatalf("protected keys left in retired: %+v", doc.st.Retired)
	}
}

func TestReplaceDBFailureRemovesUpload(t *testing.T) {
	s, f, doc, _ := newTestStore(t)
	ctx := context.Background()
	s.Ensure(ctx)
	doc.failSave = errors.New("db down")
	if _, err := s.Replace(ctx, AppleTouchIcon, "a.png", pngN(180)); err == nil {
		t.Fatal("want an error")
	}
	if len(f.deleted) != 1 || f.has(f.deleted[0]) {
		t.Fatalf("new object not removed: deleted %v", f.deleted)
	}
	if len(doc.st.Custom) != 0 {
		t.Fatalf("state changed: %+v", doc.st)
	}
	// Uploading the default bytes while the DB is down must not delete the default.
	data, _ := defaultFile(mustSlot(t, MonogramInverse))
	s.mu.Lock()
	doc.st.Custom = map[string]Custom{MonogramInverse: {Key: "brand/monogram-inverse.0123456789abcdef.svg", ContentType: typeSVG}}
	s.snap.Store(nil)
	s.mu.Unlock()
	f.deleted = nil
	if _, err := s.Replace(ctx, MonogramInverse, "d.svg", data); err == nil {
		t.Fatal("want an error")
	}
	if len(f.deleted) != 0 || !f.has(s.defaults[MonogramInverse].key) {
		t.Fatalf("default removed: %v", f.deleted)
	}
}

func TestReplaceStorageAndValidation(t *testing.T) {
	s, f, doc, _ := newTestStore(t)
	ctx := context.Background()
	f.failPut = errors.New("r2 down")
	if _, err := s.Replace(ctx, AppleTouchIcon, "a.png", pngN(180)); !errors.Is(err, ErrStorage) {
		t.Fatalf("storage: %v", err)
	}
	f.failPut = nil
	if _, err := s.Replace(ctx, AppleTouchIcon, "a.png", pngN(192)); problemOf(t, err).Status != 422 {
		t.Fatal(err)
	}
	if _, err := s.Replace(ctx, "nope", "a.png", pngN(180)); !errors.Is(err, ErrUnknownSlot) {
		t.Fatal(err)
	}
	if doc.saves != 0 {
		t.Fatalf("saves = %d", doc.saves)
	}
}

func TestBrandView(t *testing.T) {
	s, f, _, _ := newTestStore(t)
	b := s.Brand(State{})
	if len(b.Assets) != len(slots) {
		t.Fatalf("assets = %d", len(b.Assets))
	}
	for _, d := range slots {
		a := b.Assets[d.Slot]
		if a.Slot != d.Slot || a.URL != f.URL(s.defaults[d.Slot].key) || a.ContentType != d.Type || a.Custom || a.Width < 1 {
			t.Errorf("%s: %+v", d.Slot, a)
		}
	}
	if a := b.Assets[FaviconICO]; a.Width != 48 || a.Height != 48 {
		t.Errorf("favicon_ico size %dx%d", a.Width, a.Height)
	}
	for slot, want := range map[string]string{
		MonogramInverse: "dark", WordmarkInverse: "dark", WordmarkNavy: "light", WordmarkCream: "dark",
		MonogramNavy: "light", MonogramCream: "dark", WordmarkPrimary: "both",
	} {
		if a := s.admin(State{}, mustSlot(t, slot)); a.Preview != want || a.Kind != "logo" {
			t.Errorf("%s = %+v, want preview %s", slot, a, want)
		}
	}
}

// TestDefaultsValid: every slot's default passes its own rules, and the
// glyph-only SVGs (viewBox with a non-zero origin) get their size from the
// viewBox width and height.
func TestDefaultsValid(t *testing.T) {
	sizes := map[string][2]int{
		WordmarkPrimary: {320, 132}, WordmarkInverse: {320, 132}, WordmarkNavy: {254, 109}, WordmarkCream: {254, 109},
		MonogramNavy: {160, 78}, MonogramCream: {160, 78},
	}
	for _, d := range slots {
		data, err := defaultFile(d)
		if err != nil {
			t.Fatalf("%s: %v", d.Slot, err)
		}
		info, err := validate(d, data)
		if err != nil {
			t.Errorf("%s: %v", d.Slot, err)
			continue
		}
		if want, ok := sizes[d.Slot]; ok && (info.Width != want[0] || info.Height != want[1]) {
			t.Errorf("%s: %dx%d, want %dx%d", d.Slot, info.Width, info.Height, want[0], want[1])
		}
	}
	if len(slots) != 12 {
		t.Errorf("slots = %d, want 12", len(slots))
	}
}

func TestAbsoluteAsset(t *testing.T) {
	ctx := context.Background()
	s, f, _, _ := newTestStore(t)
	key := s.defaults[EmailLogo].key
	a, err := s.AbsoluteAsset(ctx, EmailLogo, "https://site.example.com/")
	if err != nil || a.URL != "https://media.example.com/"+key || a.Width != 640 || a.Height != 264 {
		t.Fatalf("absolute media URL: %+v %v", a, err)
	}
	f.base = "/media/" // no MEDIA_PUBLIC_URL: the site serves /media/*
	if a, err = s.AbsoluteAsset(ctx, EmailLogo, "https://site.example.com/"); err != nil || a.URL != "https://site.example.com/media/"+key {
		t.Fatalf("relative media URL: %+v %v", a, err)
	}
	// Only the origin of the site URL counts: path, query and fragment are dropped.
	for _, site := range []string{"https://site.example.com/sub/page?x=1#frag", "https://site.example.com?q=/x",
		"https://site.example.com#/x", " https://site.example.com "} {
		if a, err = s.AbsoluteAsset(ctx, EmailLogo, site); err != nil || a.URL != "https://site.example.com/media/"+key {
			t.Errorf("site %q: %+v %v", site, a, err)
		}
	}
	for _, site := range []string{"", "javascript:alert(1)", "//evil.example", "https://u@site.example.com", "/relative"} {
		if a, err := s.AbsoluteAsset(ctx, EmailLogo, site); err == nil {
			t.Errorf("site %q: got %q", site, a.URL)
		}
	}
	f.base = "javascript:alert(1)//"
	if _, err := s.AbsoluteAsset(ctx, EmailLogo, "https://site.example.com"); err == nil {
		t.Error("a non-http media URL was accepted")
	}
	if _, err := s.AbsoluteAsset(ctx, "nope", "https://site.example.com"); !errors.Is(err, ErrUnknownSlot) {
		t.Errorf("unknown slot: %v", err)
	}
	if a := s.admin(State{}, mustSlot(t, EmailLogo)); a.Kind != "logo" || a.Preview != "both" || a.Label != "Email logo" ||
		len(a.Accept) != 1 || a.Accept[0] != "image/png" {
		t.Errorf("email logo = %+v", a)
	}
}
