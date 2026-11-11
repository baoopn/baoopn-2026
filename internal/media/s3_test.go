package media

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/xml"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// fakeS3 is an in-process S3 object API (PUT, HEAD, GET, DELETE) for one
// bucket, addressed either virtual-hosted (<bucket>.<host>) or path-style.
type fakeS3 struct {
	t      *testing.T
	bucket string
	mu     sync.Mutex
	objs   map[string]fakeObj
	hosts  []string // Host header of every request
	// failPuts makes the next PUTs answer 500 (storage outage).
	failPuts int
	// pageSize is the ListObjectsV2 page size (default 1000).
	pageSize int
}

type fakeObj struct {
	body   []byte
	header http.Header
}

func newFakeS3(t *testing.T, bucket string) (*fakeS3, *httptest.Server) {
	f := &fakeS3{t: t, bucket: bucket, objs: map[string]fakeObj{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeS3) key(r *http.Request) (string, bool) {
	p := strings.TrimPrefix(r.URL.Path, "/")
	if strings.HasPrefix(r.Host, f.bucket+".") {
		return p, p != ""
	}
	k, ok := strings.CutPrefix(p, f.bucket+"/")
	return k, ok && k != ""
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hosts = append(f.hosts, r.Host)
	if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-key/") {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2" {
		f.list(w, r)
		return
	}
	key, ok := f.key(r)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodPut:
		if f.failPuts > 0 {
			f.failPuts--
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if enc := r.Header.Get("Content-Encoding"); strings.Contains(enc, "aws-chunked") {
			f.t.Errorf("unexpected aws-chunked upload")
		}
		body, _ := io.ReadAll(r.Body)
		h := http.Header{}
		for _, k := range []string{"Content-Type", "Cache-Control", "Content-Disposition"} {
			if v := r.Header.Get(k); v != "" {
				h.Set(k, v)
			}
		}
		sum := md5.Sum(body)
		h.Set("ETag", `"`+hex.EncodeToString(sum[:])+`"`)
		h.Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		f.objs[key] = fakeObj{body: body, header: h}
		w.Header().Set("ETag", h.Get("ETag"))
		w.WriteHeader(http.StatusOK)
	case http.MethodHead, http.MethodGet:
		o, ok := f.objs[key]
		if !ok {
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		for k, v := range o.header {
			w.Header()[k] = v
		}
		if inm := r.Header.Get("If-None-Match"); inm != "" && inm == o.header.Get("ETag") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		body := o.body
		status := http.StatusOK
		if rng := r.Header.Get("Range"); rng != "" {
			a, b, _ := strings.Cut(strings.TrimPrefix(rng, "bytes="), "-")
			lo, _ := strconv.Atoi(a)
			hi, _ := strconv.Atoi(b)
			hi = min(hi, len(body)-1)
			w.Header().Set("Content-Range", "bytes "+a+"-"+strconv.Itoa(hi)+"/"+strconv.Itoa(len(body)))
			body = body[lo : hi+1]
			status = http.StatusPartialContent
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(status)
		if r.Method == http.MethodGet {
			_, _ = w.Write(body)
		}
	case http.MethodDelete:
		delete(f.objs, key)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// list answers ListObjectsV2 in key order, pageSize keys per page; the
// continuation token is the last key of the previous page.
func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	keys := make([]string, 0, len(f.objs))
	after := r.URL.Query().Get("continuation-token")
	for k := range f.objs {
		if k > after {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	size := f.pageSize
	if size <= 0 {
		size = 1000
	}
	truncated := len(keys) > size
	if truncated {
		keys = keys[:size]
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><ListBucketResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/">`)
	b.WriteString("<Name>" + f.bucket + "</Name><KeyCount>" + strconv.Itoa(len(keys)) + "</KeyCount>")
	b.WriteString("<IsTruncated>" + strconv.FormatBool(truncated) + "</IsTruncated>")
	for _, k := range keys {
		o := f.objs[k]
		mod, _ := http.ParseTime(o.header.Get("Last-Modified"))
		var esc strings.Builder
		_ = xml.EscapeText(&esc, []byte(k))
		b.WriteString("<Contents><Key>" + esc.String() + "</Key><LastModified>" + mod.UTC().Format("2006-01-02T15:04:05.000Z") +
			"</LastModified><Size>" + strconv.Itoa(len(o.body)) + "</Size><StorageClass>STANDARD</StorageClass></Contents>")
	}
	if truncated {
		b.WriteString("<NextContinuationToken>" + keys[len(keys)-1] + "</NextContinuationToken>")
	}
	b.WriteString("</ListBucketResult>")
	w.Header().Set("Content-Type", "application/xml")
	_, _ = io.WriteString(w, b.String())
}

// putObj stores an object directly, last modified at mod.
func (f *fakeS3) putObj(key string, body []byte, mod time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := http.Header{}
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Last-Modified", mod.UTC().Format(http.TimeFormat))
	f.objs[key] = fakeObj{body: body, header: h}
}

func (f *fakeS3) has(key string) bool {
	_, ok := f.get(key)
	return ok
}

func (f *fakeS3) get(key string) (fakeObj, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o, ok := f.objs[key]
	return o, ok
}

// s3Cfg points at the fake through a fake hostname, so virtual-hosted
// requests (<bucket>.r2.test) reach it without DNS.
func s3Cfg(srv *httptest.Server, public string) S3Config {
	addr := srv.Listener.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	tr := &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}
	return S3Config{Endpoint: "http://r2.test:" + port, Bucket: "media-bucket", AccessKeyID: "test-key",
		SecretAccessKey: "test-secret", PublicURL: public, HTTPClient: &http.Client{Transport: tr}}
}

func newS3Store(t *testing.T, cfg S3Config, withDB bool) *Store {
	t.Helper()
	d := Deps{Dir: t.TempDir(), Log: slog.New(slog.DiscardHandler), S3: cfg}
	if withDB {
		d.DB = testdb.New(t)
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestS3ConfigEnabled(t *testing.T) {
	full := S3Config{Endpoint: "https://x.r2.cloudflarestorage.com", Bucket: "b", AccessKeyID: "k", SecretAccessKey: "s"}
	if !full.Enabled() {
		t.Fatal("full config should be enabled")
	}
	for _, edit := range []func(*S3Config){
		func(c *S3Config) { c.Endpoint = "" }, func(c *S3Config) { c.Bucket = "" },
		func(c *S3Config) { c.AccessKeyID = "" }, func(c *S3Config) { c.SecretAccessKey = "" },
	} {
		c := full
		edit(&c)
		if c.Enabled() {
			t.Errorf("%+v should be disabled", c)
		}
	}
}

func TestURL(t *testing.T) {
	name := "0123456789abcdef01234567.png"
	_, srv := newFakeS3(t, "media-bucket")
	for _, c := range []struct {
		label string
		cfg   S3Config
		want  string
	}{
		{"local", S3Config{PublicURL: "https://media.example.com"}, "/media/" + name},
		{"bucket, public domain", s3Cfg(srv, "https://media.example.com/"), "https://media.example.com/" + name},
		{"bucket, streamed", s3Cfg(srv, ""), "/media/" + name},
	} {
		s := newS3Store(t, c.cfg, false)
		if got := s.URL(name); got != c.want {
			t.Errorf("%s: URL = %q, want %q", c.label, got, c.want)
		}
		if got := s.finish(Media{Filename: name}).URL; got != c.want {
			t.Errorf("%s: finish URL = %q", c.label, got)
		}
	}
}

func TestRedirectHandler(t *testing.T) {
	_, srv := newFakeS3(t, "media-bucket")
	h := newS3Store(t, s3Cfg(srv, "https://media.example.com"), false).Handler()
	name := "0123456789abcdef01234567.svg"
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/media/"+name, nil))
		if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "https://media.example.com/"+name {
			t.Fatalf("%s: %d %q", m, rec.Code, rec.Header().Get("Location"))
		}
	}
	for _, p := range []string{"/media/evil.example.com", "/media//evil.example.com/x.png", "/media/..%2fx",
		"/media/0123456789abcdef01234567.html", "/media/0123456789abcdef01234567.png?x=//evil"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if p == "/media/0123456789abcdef01234567.png?x=//evil" {
			if rec.Header().Get("Location") != "https://media.example.com/0123456789abcdef01234567.png" {
				t.Fatalf("query leaked into redirect: %q", rec.Header().Get("Location"))
			}
			continue
		}
		if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
			t.Fatalf("%s: %d %q", p, rec.Code, rec.Header().Get("Location"))
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/media/"+name, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post: %d", rec.Code)
	}
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestS3PutHeaders(t *testing.T) {
	fake, srv := newFakeS3(t, "media-bucket")
	s := newS3Store(t, s3Cfg(srv, "https://media.example.com"), false)
	ctx := context.Background()
	for _, c := range []struct{ name, ctype, disp string }{
		{"0123456789abcdef01234567.svg", "image/svg+xml", "attachment"},
		{"0123456789abcdef01234567.png", "image/png", ""},
		{"0123456789abcdef01234567.pdf", "application/pdf", "inline"},
	} {
		if err := s.s3.put(ctx, c.name, c.ctype, writeTemp(t, svgDoc), int64(len(svgDoc))); err != nil {
			t.Fatal(err)
		}
		o, ok := fake.get(c.name)
		if !ok || string(o.body) != string(svgDoc) {
			t.Fatalf("%s not stored", c.name)
		}
		if o.header.Get("Content-Type") != c.ctype || o.header.Get("Cache-Control") != immutableCache ||
			o.header.Get("Content-Disposition") != c.disp {
			t.Fatalf("%s headers %v", c.name, o.header)
		}
	}
	for _, h := range fake.hosts {
		if !strings.HasPrefix(h, "media-bucket.r2.test:") {
			t.Fatalf("not virtual-hosted: %q", h)
		}
	}
	ok, err := s.s3.exists(ctx, "0123456789abcdef01234567.svg")
	if err != nil || !ok {
		t.Fatalf("exists: %v %v", ok, err)
	}
	if err := s.s3.remove(ctx, "0123456789abcdef01234567.svg"); err != nil {
		t.Fatal(err)
	}
	ok, err = s.s3.exists(ctx, "0123456789abcdef01234567.svg")
	if err != nil || ok {
		t.Fatalf("after delete: %v %v", ok, err)
	}
}

func TestStreamFromBucket(t *testing.T) {
	_, srv := newFakeS3(t, "media-bucket")
	cfg := s3Cfg(srv, "")
	cfg.PathStyle = true
	s := newS3Store(t, cfg, false)
	ctx := context.Background()
	svg := "0123456789abcdef01234567.svg"
	pdf := "0123456789abcdef01234567.pdf"
	if err := s.s3.put(ctx, svg, "image/svg+xml", writeTemp(t, svgDoc), int64(len(svgDoc))); err != nil {
		t.Fatal(err)
	}
	pdfData := []byte("%PDF-1.7 0123456789")
	if err := s.s3.put(ctx, pdf, "application/pdf", writeTemp(t, pdfData), int64(len(pdfData))); err != nil {
		t.Fatal(err)
	}
	h := s.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/media/"+svg, nil))
	hd := rec.Header()
	if rec.Code != 200 || rec.Body.String() != string(svgDoc) ||
		hd.Get("Content-Type") != "image/svg+xml" ||
		hd.Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; sandbox" ||
		hd.Get("Cache-Control") != immutableCache || hd.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("svg: %d %v %q", rec.Code, hd, rec.Body.String())
	}
	etag := hd.Get("ETag")

	req := httptest.NewRequest(http.MethodGet, "/media/"+svg, nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if etag == "" || rec.Code != http.StatusNotModified {
		t.Fatalf("conditional: %q %d", etag, rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/media/"+pdf, nil)
	req.Header.Set("Range", "bytes=0-3")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "%PDF" ||
		rec.Header().Get("Content-Range") != "bytes 0-3/19" || rec.Header().Get("Content-Disposition") != "inline" {
		t.Fatalf("range: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/media/"+pdf, nil))
	if rec.Code != 200 || rec.Header().Get("Content-Length") != "19" || rec.Body.Len() != 0 {
		t.Fatalf("head: %d %v", rec.Code, rec.Header())
	}

	for _, m := range []string{http.MethodGet, http.MethodHead} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(m, "/media/0123456789abcdef01234567.png", nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s missing: %d", m, rec.Code)
		}
	}
}

func TestS3SaveDeleteMigrate(t *testing.T) {
	fake, srv := newFakeS3(t, "media-bucket")
	s := newS3Store(t, s3Cfg(srv, "https://media.example.com"), true)
	ctx := context.Background()

	m, err := s.Save(ctx, "logo.svg", strings.NewReader(string(svgDoc)))
	if err != nil {
		t.Fatal(err)
	}
	if m.URL != "https://media.example.com/"+m.Filename {
		t.Fatalf("url %q", m.URL)
	}
	if o, ok := fake.get(m.Filename); !ok || o.header.Get("Content-Disposition") != "attachment" {
		t.Fatalf("object %v %v", ok, o.header)
	}
	if entries, _ := os.ReadDir(s.dir); len(entries) != 0 {
		t.Fatalf("local files left: %d", len(entries))
	}
	list, _, err := s.List(ctx, 1, 10)
	if err != nil || len(list) != 1 || list[0].URL != m.URL {
		t.Fatalf("list %v %v", list, err)
	}

	// A file uploaded before the bucket existed: on disk and in the table only.
	old := "aaaaaaaaaaaaaaaaaaaaaaaa.png"
	if err := os.WriteFile(filepath.Join(s.dir, old), pngHead, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO media (filename, original, content_type, size_bytes, sha256)
		VALUES ($1, 'old.png', 'image/png', $2, 'oldsum'), ('bbbbbbbbbbbbbbbbbbbbbbbb.png', 'gone.png', 'image/png', 1, 'gonesum')`,
		old, len(pngHead)); err != nil {
		t.Fatal(err)
	}
	st, err := s.MigrateToBucket(ctx)
	if err != nil || st != (MigrateStats{Uploaded: 1, Present: 0, NoLocal: 2}) {
		t.Fatalf("migrate: %+v %v", st, err)
	}
	if o, ok := fake.get(old); !ok || string(o.body) != string(pngHead) || o.header.Get("Content-Type") != "image/png" {
		t.Fatalf("migrated object %v %v", ok, o.header)
	}
	st, err = s.MigrateToBucket(ctx)
	if err != nil || st != (MigrateStats{Present: 1, NoLocal: 2}) {
		t.Fatalf("second migrate: %+v %v", st, err)
	}

	if ok, err := s.Delete(ctx, m.ID); err != nil || !ok {
		t.Fatalf("delete %v %v", ok, err)
	}
	if _, ok := fake.get(m.Filename); ok {
		t.Fatal("object not deleted")
	}
}

func (f *fakeS3) setFailPuts(n int) {
	f.mu.Lock()
	f.failPuts = n
	f.mu.Unlock()
}

// The S3 client fails a bucket that never answers but sets no total timeout,
// which would cut off a large upload.
func TestS3HTTPClientTimeouts(t *testing.T) {
	c := s3HTTPClient()
	if c.GetTimeout() != 0 {
		t.Fatalf("total timeout = %v, want none", c.GetTimeout())
	}
	if tr := c.GetTransport(); tr.ResponseHeaderTimeout != s3ResponseHeaderTimeout || tr.TLSHandshakeTimeout == 0 {
		t.Fatalf("transport timeouts = %v / %v", tr.ResponseHeaderTimeout, tr.TLSHandshakeTimeout)
	}
	if d := c.GetDialer(); d.Timeout != s3DialTimeout {
		t.Fatalf("dial timeout = %v", d.Timeout)
	}
}

// A bucket error is a JSON 503: Traefik's error pages replace 502 bodies.
func TestServeObjectStorageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	s := newS3Store(t, s3Cfg(srv, ""), false)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/media/"+strings.Repeat("a", 24)+".webp", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"storage_unavailable"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
