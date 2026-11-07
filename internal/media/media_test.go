package media

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

var (
	pngHead  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")
	jpegHead = []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	avifHead = []byte("\x00\x00\x00\x1cftypavif\x00\x00\x00\x00avifmif1miaf")
	pdfHead  = []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\n")
	svgDoc   = []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>`)
)

func TestSniff(t *testing.T) {
	tests := []struct {
		name, original string
		head           []byte
		wantType       string
		wantExt        string
		ok             bool
	}{
		{"png", "a.png", pngHead, "image/png", ".png", true},
		{"jpeg", "a.jpeg", jpegHead, "image/jpeg", ".jpg", true},
		{"gif", "a.gif", []byte("GIF89a\x01\x00\x01\x00"), "image/gif", ".gif", true},
		{"avif", "a.avif", avifHead, "image/avif", ".avif", true},
		{"avis", "a.avif", []byte("\x00\x00\x00\x1cftypavis\x00\x00\x00\x00"), "image/avif", ".avif", true},
		{"pdf", "doc.pdf", pdfHead, "application/pdf", ".pdf", true},
		{"svg xml", "logo.svg", svgDoc, "image/svg+xml", ".svg", true},
		{"svg bare", "logo.SVG", []byte(`<svg viewBox="0 0 1 1"></svg>`), "image/svg+xml", ".svg", true},
		{"svg wrong name", "logo.txt", svgDoc, "", "", false},
		{"xml without svg", "x.svg", []byte(`<?xml version="1.0"?><note/>`), "", "", false},
		{"html", "x.svg", []byte(`<!DOCTYPE html><html><svg></svg></html>`), "", "", false},
		{"exe", "x.png", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00\xff\xff"), "", "", false},
		{"text", "x.png", []byte("hello world"), "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ct, ext, ok := sniff(tc.head, tc.original)
			if ok != tc.ok || ct != tc.wantType || ext != tc.wantExt {
				t.Fatalf("sniff = %q %q %v, want %q %q %v", ct, ext, ok, tc.wantType, tc.wantExt, tc.ok)
			}
		})
	}
}

func TestFilenameRegex(t *testing.T) {
	good := []string{"0123456789abcdef01234567.png", "0123456789abcdef01234567.svg", "0123456789abcdef01234567.pdf"}
	bad := []string{"0123456789ABCDEF01234567.png", "0123456789abcdef0123456.png", "0123456789abcdef01234567.html",
		"../0123456789abcdef01234567.png", "0123456789abcdef01234567.png/x", ".up-123", ""}
	for _, g := range good {
		if !fileRe.MatchString(g) {
			t.Errorf("%q should match", g)
		}
	}
	for _, b := range bad {
		if fileRe.MatchString(b) {
			t.Errorf("%q should not match", b)
		}
	}
}

func TestCleanOriginal(t *testing.T) {
	if got := cleanOriginal("C:\\Temp\\x\\pic\x00.png"); got != "pic.png" {
		t.Fatalf("got %q", got)
	}
	if got := cleanOriginal("../../etc/passwd"); got != "passwd" {
		t.Fatalf("got %q", got)
	}
	if got := cleanOriginal(strings.Repeat("é", 300)); len([]rune(got)) != 200 {
		t.Fatalf("len %d", len([]rune(got)))
	}
}

func newServeStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(Deps{Dir: t.TempDir(), Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestServeHeaders(t *testing.T) {
	s := newServeStore(t)
	svgName := "0123456789abcdef01234567.svg"
	pdfName := "0123456789abcdef01234567.pdf"
	if err := os.WriteFile(filepath.Join(s.dir, svgName), svgDoc, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dir, pdfName), []byte("%PDF-1.7 0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+svgName, nil))
	if rec.Code != 200 {
		t.Fatalf("svg status %d", rec.Code)
	}
	hd := rec.Header()
	if hd.Get("Content-Type") != "image/svg+xml" ||
		hd.Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; sandbox" ||
		hd.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
		hd.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("svg headers %v", hd)
	}

	req := httptest.NewRequest(http.MethodGet, "/media/"+pdfName, nil)
	req.Header.Set("Range", "bytes=0-3")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "%PDF" {
		t.Fatalf("range: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Disposition") != "inline" || rec.Header().Get("Content-Security-Policy") != "" {
		t.Fatalf("pdf headers %v", rec.Header())
	}

	for _, p := range []string{"/media/nope.png", "/media/0123456789abcdef01234567.png", "/media/..%2fsecret"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != 404 || !strings.Contains(rec.Body.String(), "not_found") {
			t.Fatalf("%s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/media/"+svgName, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post: %d", rec.Code)
	}
}

func multipartBody(t *testing.T, name string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func TestUploadDedupeListDelete(t *testing.T) {
	pool := testdb.New(t)
	s, err := New(Deps{DB: pool, Log: slog.New(slog.DiscardHandler), Dir: t.TempDir(), MaxBytes: 1 << 10, UploadWait: -1})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	s.Admin(r)

	upload := func(name string, data []byte) *httptest.ResponseRecorder {
		body, ct := multipartBody(t, name, data)
		req := httptest.NewRequest(http.MethodPost, "/media", body)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	decode := func(rec *httptest.ResponseRecorder) Media {
		var out struct{ Data Media }
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Data
	}

	pic := testimg.PNG(testimg.Halves(2, 2))
	rec := upload("pic.png", pic)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
	first := decode(rec)
	if !fileRe.MatchString(first.Filename) || first.URL != "/media/"+first.Filename ||
		first.ContentType != "image/webp" || first.Original != "pic.png" ||
		first.Status != StatusProcessing || !strings.HasSuffix(first.Filename, ".webp") {
		t.Fatalf("media %+v", first)
	}
	if rec := upload("again.png", pic); rec.Code != http.StatusAccepted || decode(rec).ID != first.ID {
		t.Fatalf("dedupe: %d %s", rec.Code, rec.Body)
	}
	if rec := upload("x.html", []byte("<html><body>hi</body></html>")); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("html: %d", rec.Code)
	}
	if rec := upload("big.png", append(append([]byte{}, pngHead...), make([]byte, 2<<10)...)); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big: %d", rec.Code)
	}
	ctx := context.Background()
	m2, err := s.Save(ctx, "doc.pdf", bytes.NewReader(pdfHead))
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.dir)
	if len(entries) != 2 {
		t.Fatalf("files on disk: %d (temp files left?)", len(entries))
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media?limit=1", nil))
	var list struct {
		Data []Media
		Meta struct{ Total, Page, Limit int }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if rec.Code != 200 || len(list.Data) != 1 || list.Data[0].ID != m2.ID || list.Meta.Total != 2 || list.Meta.Limit != 1 {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media?limit=101", nil))
	if rec.Code != 400 {
		t.Fatalf("limit 101: %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/media/"+itoa(first.ID), nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if _, err := os.Stat(s.originalPath(stemOf(first.Filename))); !os.IsNotExist(err) {
		t.Fatalf("original still there: %v", err)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/media/"+itoa(first.ID), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete again: %d", rec.Code)
	}
	_, _ = io.Copy(io.Discard, rec.Body)
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
