package media

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

type renameCall struct{ old, new string }

// newQueueStore is a DB-backed local-disk store that does not wait on uploads.
func newQueueStore(t *testing.T, edit func(*Deps)) (*Store, *[]renameCall) {
	t.Helper()
	var mu sync.Mutex
	calls := &[]renameCall{}
	d := Deps{DB: testdb.New(t), Log: slog.New(slog.DiscardHandler), Dir: t.TempDir(), UploadWait: -1,
		OnRename: func(_ context.Context, o, n string) error {
			mu.Lock()
			*calls = append(*calls, renameCall{o, n})
			mu.Unlock()
			return nil
		}}
	if edit != nil {
		edit(&d)
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return s, calls
}

// pngN is a distinct small PNG per n.
func pngN(n int) []byte { return testimg.PNG(testimg.Halves(4+n, 4)) }

func mustSave(t *testing.T, s *Store, name string, data []byte, crop *Crop) Media {
	t.Helper()
	m, err := s.SaveCrop(context.Background(), name, bytes.NewReader(data), crop)
	if err != nil {
		t.Fatalf("save %s: %v", name, err)
	}
	return m
}

func mustLease(t *testing.T, s *Store) *Job {
	t.Helper()
	j, err := s.Lease(context.Background(), "test")
	if err != nil || j == nil {
		t.Fatalf("lease: %v %v", j, err)
	}
	return j
}

func exec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(context.Background(), q, args...); err != nil {
		t.Fatal(err)
	}
}

func jobState(t *testing.T, s *Store, mediaID int64) (phase, status string, attempts int) {
	t.Helper()
	if err := s.db.QueryRow(context.Background(), `SELECT phase, status, attempts FROM media_jobs WHERE media_id = $1`,
		mediaID).Scan(&phase, &status, &attempts); err != nil {
		t.Fatal(err)
	}
	return
}

func writeResult(t *testing.T, s *Store) (string, int64) {
	t.Helper()
	f, err := s.CreateTemp(".test-*")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("RIFF\x04\x00\x00\x00WEBP")
	_, _ = f.Write(data)
	_ = f.Close()
	return f.Name(), int64(len(data))
}

func TestStoredAsIs(t *testing.T) {
	s, _ := newQueueStore(t, nil)
	for _, c := range []struct {
		name  string
		data  []byte
		ctype string
	}{
		{"anim.gif", testimg.GIF(3), "image/gif"},
		{"logo.svg", svgDoc, "image/svg+xml"},
		{"doc.pdf", pdfHead, "application/pdf"},
	} {
		m := mustSave(t, s, c.name, c.data, &Crop{0, 0, 1, 1}) // crop is ignored for these
		if m.Status != StatusReady || m.Converted || m.ContentType != c.ctype {
			t.Fatalf("%s: %+v", c.name, m)
		}
		got, err := os.ReadFile(s.dir + "/" + m.Filename)
		if err != nil || !bytes.Equal(got, c.data) {
			t.Fatalf("%s: stored bytes differ (%v)", c.name, err)
		}
	}
	if j, _ := s.Lease(context.Background(), "w"); j != nil {
		t.Fatal("a job was queued for a file stored as is")
	}
}

func TestUploadGuards(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.MaxBytes = 1 << 20 })
	ctx := context.Background()
	bomb := testimg.PNGSetSize(pngN(0), 100000, 100000)
	if _, err := s.Save(ctx, "bomb.png", bytes.NewReader(bomb)); !errors.Is(err, ErrTooManyPixels) {
		t.Fatalf("bomb: %v", err)
	}
	broken := append(append([]byte{}, pngN(0)[:33]...), "garbage"...)
	broken = testimg.PNGSetSize(broken, 0, 0)
	if _, err := s.Save(ctx, "broken.png", bytes.NewReader(broken)); !errors.Is(err, ErrUndecodable) {
		t.Fatalf("broken: %v", err)
	}
	if _, err := s.SaveCrop(ctx, "c.png", bytes.NewReader(pngN(0)), &Crop{0, 0, 5, 5}); !errors.Is(err, ErrBadCrop) {
		t.Fatalf("crop: %v", err)
	}

	r := chi.NewRouter()
	s.Admin(r)
	post := func(fields map[string]string, data []byte) *httptest.ResponseRecorder {
		body, ct := multipartBody(t, "x.png", data)
		if fields != nil {
			body, ct = multipartFields(t, data, fields)
		}
		req := httptest.NewRequest(http.MethodPost, "/media", body)
		req.Header.Set("Content-Type", ct)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	for _, c := range []struct {
		fields map[string]string
		data   []byte
		code   int
	}{
		{nil, bomb, http.StatusRequestEntityTooLarge},
		{nil, broken, http.StatusUnsupportedMediaType},
		{map[string]string{"crop_x": "0", "crop_y": "0", "crop_w": "9", "crop_h": "2"}, pngN(0), http.StatusBadRequest},
		{map[string]string{"crop_x": "0", "crop_y": "0", "crop_w": "2"}, pngN(0), http.StatusBadRequest},
		{map[string]string{"crop_x": "-1", "crop_y": "0", "crop_w": "2", "crop_h": "2"}, pngN(0), http.StatusBadRequest},
		{map[string]string{"crop_x": "1", "crop_y": "1", "crop_w": "2", "crop_h": "2"}, pngN(0), http.StatusAccepted},
	} {
		if rec := post(c.fields, c.data); rec.Code != c.code {
			t.Errorf("%v: %d %s", c.fields, rec.Code, rec.Body)
		}
	}
	j := mustLease(t, s)
	if j.Crop == nil || *j.Crop != (Crop{1, 1, 2, 2}) {
		t.Fatalf("job crop %+v", j.Crop)
	}

	// The originals store is not writable: 503.
	_ = os.RemoveAll(s.originalsDir())
	if err := os.WriteFile(s.originalsDir(), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if rec := post(nil, pngN(7)); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("originals unwritable: %d", rec.Code)
	}
}

func TestLeaseSkipLocked(t *testing.T) {
	s, _ := newQueueStore(t, nil)
	for i := range 3 {
		mustSave(t, s, "p.png", pngN(i), nil)
	}
	var mu sync.Mutex
	got := map[int64]int{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			j, err := s.Lease(context.Background(), "w")
			if err != nil {
				t.Error(err)
				return
			}
			if j != nil {
				mu.Lock()
				got[j.ID]++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(got) != 3 {
		t.Fatalf("leased %v", got)
	}
	for id, n := range got {
		if n != 1 {
			t.Fatalf("job %d leased %d times", id, n)
		}
	}
}

func TestLeaseExpiryAndComplete(t *testing.T) {
	s, _ := newQueueStore(t, nil)
	ctx := context.Background()
	m := mustSave(t, s, "p.png", pngN(0), nil)
	j1 := mustLease(t, s)
	if j, _ := s.Lease(ctx, "other"); j != nil {
		t.Fatal("leased twice")
	}
	exec(t, s, `UPDATE media_jobs SET lease_until = now() - interval '1 second'`)
	j2 := mustLease(t, s)
	if j2.ID != j1.ID || j2.LeaseToken == j1.LeaseToken || j2.Attempt != 2 {
		t.Fatalf("re-lease %+v after %+v", j2, j1)
	}
	path, size := writeResult(t, s)
	if _, err := s.Complete(ctx, j1.ID, j1.LeaseToken, path, size, "abc"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale token: %v", err)
	}
	if _, err := s.OpenOriginal(ctx, j1.ID, j1.LeaseToken); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale open: %v", err)
	}
	f, err := s.OpenOriginal(ctx, j2.ID, j2.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	done, err := s.Complete(ctx, j2.ID, j2.LeaseToken, path, size, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if done.ID != m.ID || done.Status != StatusReady || !done.Converted || done.SizeBytes != size {
		t.Fatalf("done %+v", done)
	}
	if _, err := os.Stat(s.originalPath(stemOf(m.Filename))); !os.IsNotExist(err) {
		t.Fatal("original not deleted")
	}
	if _, err := os.Stat(s.dir + "/" + m.Filename); err != nil {
		t.Fatal("webp not stored")
	}
	// The same original uploaded again is deduplicated to the WebP.
	if again := mustSave(t, s, "again.png", pngN(0), nil); again.ID != m.ID {
		t.Fatalf("not deduplicated: %+v", again)
	}
}

func TestBackoff(t *testing.T) {
	for _, c := range []struct {
		attempt int
		lo, hi  time.Duration
	}{{1, 2500 * time.Millisecond, 5 * time.Second}, {3, 10 * time.Second, 20 * time.Second},
		{12, 5 * time.Minute, 10 * time.Minute}} {
		for range 20 {
			if d := backoff(c.attempt); d < c.lo || d > c.hi {
				t.Fatalf("backoff(%d) = %v", c.attempt, d)
			}
		}
	}
}

func TestRetryableThenMaxAttemptsFallback(t *testing.T) {
	s, renames := newQueueStore(t, func(d *Deps) { d.MaxAttempts = 2 })
	ctx := context.Background()
	src := testimg.JPEGWithExif(testimg.Halves(8, 4), 6, true)
	m := mustSave(t, s, "photo.jpg", src, nil)

	j := mustLease(t, s)
	retry, err := s.Fail(ctx, j.ID, j.LeaseToken, "network hiccup", true)
	if err != nil || !retry {
		t.Fatalf("first failure: %v %v", retry, err)
	}
	if j, _ := s.Lease(ctx, "w"); j != nil {
		t.Fatal("leased during backoff")
	}
	exec(t, s, `UPDATE media_jobs SET next_attempt_at = now()`)
	j = mustLease(t, s)
	if retry, err := s.Fail(ctx, j.ID, j.LeaseToken, "network hiccup", true); err != nil || retry {
		t.Fatalf("last attempt: %v %v", retry, err)
	}
	if phase, status, _ := jobState(t, s, m.ID); phase != PhaseFallback || status != "queued" {
		t.Fatalf("job %s %s", phase, status)
	}
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, m.ID)
	if got.Status != StatusReady || got.Converted || got.ContentType != "image/jpeg" ||
		!strings.HasSuffix(got.Filename, ".jpg") || stemOf(got.Filename) != stemOf(m.Filename) ||
		got.Error != "WebP conversion failed: network hiccup; saved the original" {
		t.Fatalf("fallback media %+v", got)
	}
	stored, err := os.ReadFile(s.dir + "/" + got.Filename)
	if err != nil || bytes.Contains(stored, []byte("GPSDATA")) || bytes.Contains(stored, []byte("XMPSECRET")) {
		t.Fatalf("published original not stripped (%v)", err)
	}
	if len(*renames) != 1 || (*renames)[0] != (renameCall{m.URL, got.URL}) {
		t.Fatalf("renames %v", *renames)
	}
	if _, err := os.Stat(s.originalPath(stemOf(m.Filename))); !os.IsNotExist(err) {
		t.Fatal("original kept after fallback")
	}
}

func TestUploadWaitsForWorker(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.UploadWait = 5 * time.Second })
	r := chi.NewRouter()
	s.Admin(r)
	path, size := writeResult(t, s)
	go func() {
		for range 100 {
			if j, _ := s.Lease(context.Background(), "w"); j != nil {
				_, _ = s.Complete(context.Background(), j.ID, j.LeaseToken, path, size, "x")
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	body, ct := multipartBody(t, "p.png", pngN(0))
	req := httptest.NewRequest(http.MethodPost, "/media", body)
	req.Header.Set("Content-Type", ct)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"converted":true`) {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body)
	}
}

// multipartFields writes the file first and the fields after it.
func multipartFields(t *testing.T, data []byte, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "x.png")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}
