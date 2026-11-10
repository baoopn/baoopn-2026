package media

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	brandICO = "brand/favicon.0123456789abcdef.ico"
	brandSVG = "brand/monogram-primary.0123456789abcdef.svg"
	// brandListening is a listening sub-brand key (one subfolder).
	brandListening = "brand/listening/icon-192.0123456789abcdef.png"
)

var brandSVGDoc = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"/>`)

func TestBrandKeyRe(t *testing.T) {
	for _, k := range []string{brandICO, brandSVG, "brand/apple-touch-icon.0123456789abcdef.png", brandListening,
		"brand/listening/icon-now-playing-navy.0123456789abcdef.svg"} {
		if !BrandKeyRe.MatchString(k) {
			t.Errorf("%s rejected", k)
		}
	}
	for _, k := range []string{"brand/../x.0123456789abcdef.png", "brand/x.png", "brand/a/b.0123456789abcdef.png",
		"brand/favicon.0123456789ABCDEF.ico", "brand/favicon.0123456789abcdef.webp", "favicon.0123456789abcdef.ico",
		"brand/Favicon.0123456789abcdef.ico", "brand/favicon.0123456789abcdef0.ico", "/brand/favicon.0123456789abcdef.ico",
		"brand/Listening/icon-192.0123456789abcdef.png", "brand/listening/../x.0123456789abcdef.png",
		"brand/listening/a/icon-192.0123456789abcdef.png", "brand/listening/listening/x.0123456789abcdef.png",
		"brand/other/icon-192.0123456789abcdef.png", "brand/listening/.0123456789abcdef.png", "brand/listening/"} {
		if BrandKeyRe.MatchString(k) {
			t.Errorf("%s accepted", k)
		}
	}
}

func checkBrandFiles(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	if err := s.PutBrandFile(ctx, "brand/x.png", "image/png", []byte("x")); !errors.Is(err, ErrBadBrandKey) {
		t.Fatalf("bad key put: %v", err)
	}
	if ok, err := s.BrandFileExists(ctx, brandICO); ok || err != nil {
		t.Fatalf("exists before put: %v %v", ok, err)
	}
	if err := s.PutBrandFile(ctx, brandICO, "image/x-icon", []byte{0, 0, 1, 0, 9}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutBrandFile(ctx, brandICO, "image/x-icon", []byte{0, 0, 1, 0, 9}); err != nil {
		t.Fatalf("idempotent put: %v", err)
	}
	if ok, err := s.BrandFileExists(ctx, brandICO); !ok || err != nil {
		t.Fatalf("exists after put: %v %v", ok, err)
	}
	if err := s.RemoveBrandFile(ctx, brandICO); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.BrandFileExists(ctx, brandICO); ok {
		t.Fatal("still there after remove")
	}
	if err := s.RemoveBrandFile(ctx, brandICO); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestBrandFilesDisk(t *testing.T) {
	s := newServeStore(t)
	checkBrandFiles(t, s)
	ctx := context.Background()
	if err := s.PutBrandFile(ctx, brandSVG, "image/svg+xml", brandSVGDoc); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(s.dir, "brand", "monogram-primary.0123456789abcdef.svg")); err != nil || string(b) != string(brandSVGDoc) {
		t.Fatalf("on disk: %q %v", b, err)
	}
	if err := s.PutBrandFile(ctx, brandICO, "image/x-icon", []byte{0, 0, 1, 0}); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	for key, ctype := range map[string]string{brandSVG: "image/svg+xml", brandICO: "image/x-icon"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+key, nil))
		hd := rec.Header()
		if rec.Code != 200 || hd.Get("Content-Type") != ctype || hd.Get("Cache-Control") != immutableCache ||
			hd.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d %v", key, rec.Code, hd)
		}
		wantCSP := ""
		if ctype == "image/svg+xml" {
			wantCSP = "default-src 'none'; style-src 'unsafe-inline'; sandbox"
		}
		if hd.Get("Content-Security-Policy") != wantCSP {
			t.Fatalf("%s: CSP %q", key, hd.Get("Content-Security-Policy"))
		}
	}
	for _, p := range []string{"/media/brand/../x.0123456789abcdef.png", "/media/brand/x.png",
		"/media/brand/a/b.0123456789abcdef.png", "/media/brand/..%2fsecret", "/media/brand/apple-touch-icon.0123456789abcdef.png"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != 404 {
			t.Fatalf("%s: %d", p, rec.Code)
		}
	}
}

// TestBrandFilesDiskListening: a brand/listening/ key gets its own folder
// on disk (created on the first put), and is served, found and removed.
func TestBrandFilesDiskListening(t *testing.T) {
	s := newServeStore(t)
	ctx := context.Background()
	if ok, err := s.BrandFileExists(ctx, brandListening); ok || err != nil {
		t.Fatalf("exists before put: %v %v", ok, err)
	}
	png := []byte("PNG listening")
	if err := s.PutBrandFile(ctx, brandListening, "image/png", png); err != nil {
		t.Fatal(err)
	}
	onDisk := filepath.Join(s.dir, "brand", "listening", "icon-192.0123456789abcdef.png")
	if b, err := os.ReadFile(onDisk); err != nil || string(b) != string(png) {
		t.Fatalf("on disk: %q %v", b, err)
	}
	if left, _ := filepath.Glob(filepath.Join(s.dir, "brand", "listening", ".tmp-*")); len(left) != 0 {
		t.Fatalf("temp files left: %v", left)
	}
	if ok, err := s.BrandFileExists(ctx, brandListening); !ok || err != nil {
		t.Fatalf("exists after put: %v %v", ok, err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+brandListening, nil))
	if rec.Code != 200 || rec.Body.String() != string(png) || rec.Header().Get("Content-Type") != "image/png" ||
		rec.Header().Get("Cache-Control") != immutableCache {
		t.Fatalf("GET: %d %v", rec.Code, rec.Header())
	}
	for _, p := range []string{"/media/brand/listening/../icon-192.0123456789abcdef.png",
		"/media/brand/Listening/icon-192.0123456789abcdef.png", "/media/brand/listening/a/icon-192.0123456789abcdef.png"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != 404 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if err := s.RemoveBrandFile(ctx, brandListening); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.BrandFileExists(ctx, brandListening); ok {
		t.Fatal("still there after remove")
	}
	if err := s.RemoveBrandFile(ctx, brandListening); err != nil {
		t.Fatalf("remove missing: %v", err)
	}
}

func TestBrandFilesBucket(t *testing.T) {
	f, srv := newFakeS3(t, "media-bucket")
	s := newS3Store(t, s3Cfg(srv, ""), false)
	checkBrandFiles(t, s)
	ctx := context.Background()
	if err := s.PutBrandFile(ctx, brandSVG, "image/svg+xml", brandSVGDoc); err != nil {
		t.Fatal(err)
	}
	o, ok := f.get(brandSVG)
	if !ok || string(o.body) != string(brandSVGDoc) || o.header.Get("Content-Type") != "image/svg+xml" ||
		o.header.Get("Cache-Control") != immutableCache || o.header.Get("Content-Disposition") != "attachment" {
		t.Fatalf("object = %v %+v", ok, o.header)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "brand")); !os.IsNotExist(err) {
		t.Fatalf("bucket store wrote MEDIA_DIR/brand: %v", err)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+brandSVG, nil))
	if rec.Code != 200 || rec.Body.String() != string(brandSVGDoc) || rec.Header().Get("Content-Type") != "image/svg+xml" ||
		rec.Header().Get("Content-Security-Policy") == "" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("stream: %d %v", rec.Code, rec.Header())
	}

	pub := newS3Store(t, s3Cfg(srv, "https://media.example.com"), false)
	if got := pub.URL(brandSVG); got != "https://media.example.com/"+brandSVG {
		t.Fatalf("URL = %q", got)
	}
	rec = httptest.NewRecorder()
	pub.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+brandSVG, nil))
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "https://media.example.com/"+brandSVG {
		t.Fatalf("redirect: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec = httptest.NewRecorder()
	pub.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/brand/..%2f..%2fevil", nil))
	if rec.Code != 404 || rec.Header().Get("Location") != "" {
		t.Fatalf("bad key redirect: %d", rec.Code)
	}
}

// Brand files (no media row, under brand/) are foreign to the orphan
// cleanup: never candidates, never deleted, never "missing".
func TestOrphanIgnoresBrandBucket(t *testing.T) {
	s, f := newOrphanS3Store(t)
	ctx := context.Background()
	old := time.Now().Add(-90 * day)
	f.putObj(brandICO, []byte("0123456789"), old)
	f.putObj(brandSVG, []byte("0123456789"), old)
	stray := stemN(1) + ".webp"
	f.putObj(stray, []byte("0123456789"), old)
	exec(t, s, `INSERT INTO settings (key, value) VALUES ('brand', $1)`,
		`{"custom":{"favicon_ico":{"key":"`+brandICO+`"}},"retired":[{"key":"`+brandSVG+`","retired_at":"2026-01-01T00:00:00Z"}]}`)

	p := mustPlan(t, s)
	if len(p.Candidates) != 1 || candidate(p, "object:"+stray) == nil || p.Kept.Foreign != 2 || len(p.Missing) != 0 {
		t.Fatalf("plan: candidates %v kept %+v missing %v", keys(p), p.Kept, p.Missing)
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil || res.Deleted.Objects != 1 {
		t.Fatalf("clean = %+v %v", res, err)
	}
	if !f.has(brandICO) || !f.has(brandSVG) || f.has(stray) {
		t.Fatal("cleanup touched a brand file (or kept the stray)")
	}
}

func TestOrphanIgnoresBrandDisk(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	writeAged(t, filepath.Join(s.dir, "brand", "favicon.0123456789abcdef.ico"), 90*day)
	writeAged(t, filepath.Join(s.dir, "brand", "monogram-primary.0123456789abcdef.svg"), 90*day)
	stray := stemN(1) + ".png"
	writeAged(t, filepath.Join(s.dir, stray), 30*day)

	p := mustPlan(t, s)
	if len(p.Candidates) != 1 || candidate(p, "local:"+stray) == nil || p.Kept.Foreign != 1 || len(p.Missing) != 0 {
		t.Fatalf("plan: candidates %v kept %+v", keys(p), p.Kept)
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil || res.Deleted.Local != 1 {
		t.Fatalf("clean = %+v %v", res, err)
	}
	for _, name := range []string{"favicon.0123456789abcdef.ico", "monogram-primary.0123456789abcdef.svg"} {
		if _, err := os.Stat(filepath.Join(s.dir, "brand", name)); err != nil {
			t.Errorf("brand/%s: %v", name, err)
		}
	}
}
