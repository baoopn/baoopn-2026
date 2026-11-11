package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/image/bmp"

	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

func bmpBytes(t *testing.T) []byte {
	var b bytes.Buffer
	if err := bmp.Encode(&b, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A permanent failure of an image browsers cannot show fails the media; the
// original is kept for a retry until the cleanup removes it.
func TestPermanentFailureRetryAndCleanup(t *testing.T) {
	s, _ := newQueueStore(t, nil)
	ctx := context.Background()
	r := chi.NewRouter()
	s.Admin(r)
	call := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}
	m := mustSave(t, s, "scan.bmp", bmpBytes(t), nil)
	id := itoa(m.ID)
	if rec := call("POST", "/media/"+id+"/retry"); rec.Code != http.StatusConflict {
		t.Fatalf("retry while processing: %d", rec.Code)
	}
	j := mustLease(t, s)
	if retry, err := s.Fail(ctx, j.ID, j.LeaseToken, "the image could not be decoded", false); err != nil || retry {
		t.Fatalf("fail: %v %v", retry, err)
	}
	if rec := call("GET", "/media/"+id); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"failed"`) ||
		!strings.Contains(rec.Body.String(), "WebP conversion failed: the image could not be decoded") {
		t.Fatalf("get failed: %d %s", rec.Code, rec.Body)
	}
	if rec := call("POST", "/media/"+id+"/retry"); rec.Code != http.StatusAccepted ||
		!strings.Contains(rec.Body.String(), `"status":"processing"`) {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body)
	}
	j = mustLease(t, s)
	if j.Attempt != 1 {
		t.Fatalf("attempts not reset: %d", j.Attempt)
	}
	_, _ = s.Fail(ctx, j.ID, j.LeaseToken, "still broken", false)

	// Within the retention the original stays; after it, cleanup deletes it.
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.originalPath(stemOf(m.Filename))); err != nil {
		t.Fatal("original deleted too early")
	}
	exec(t, s, `UPDATE media_jobs SET failed_at = now() - interval '25 hours'`)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.originalPath(stemOf(m.Filename))); !os.IsNotExist(err) {
		t.Fatal("original not cleaned up")
	}
	if rec := call("POST", "/media/"+id+"/retry"); rec.Code != http.StatusGone {
		t.Fatalf("retry after cleanup: %d %s", rec.Code, rec.Body)
	}
	if rec := call("POST", "/media/999999/retry"); rec.Code != http.StatusNotFound {
		t.Fatalf("retry unknown: %d", rec.Code)
	}
}

// No worker leases the job within FallbackAfter: the API publishes the
// stripped original itself, or fails what browsers cannot show.
func TestFallbackWithoutWorker(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.FallbackAfter = time.Minute })
	ctx := context.Background()
	jpg := mustSave(t, s, "photo.jpg", testimg.JPEGWithExif(testimg.Halves(8, 4), 1, true), nil)
	tif := mustSave(t, s, "scan.bmp", bmpBytes(t), nil)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, jpg.ID); got.Status != StatusProcessing {
		t.Fatalf("fell back too early: %+v", got)
	}
	exec(t, s, `UPDATE media_jobs SET next_attempt_at = now() - interval '2 minutes'`)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, jpg.ID)
	if got.Status != StatusReady || got.Converted || got.Error != "converter unavailable; saved the original" {
		t.Fatalf("jpeg %+v", got)
	}
	got, _ = s.Get(ctx, tif.ID)
	if got.Status != StatusFailed || got.Error != "converter unavailable" {
		t.Fatalf("bmp %+v", got)
	}
}

// R2 answers 500: the job is re-queued with backoff and the media stays
// processing; the next attempt stores it. Out of attempts: "storage unavailable".
func TestStorageFailureRetry(t *testing.T) {
	fake, srv := newFakeS3(t, "media-bucket")
	d := Deps{DB: testdb.New(t), Log: slog.New(slog.DiscardHandler), Dir: t.TempDir(), UploadWait: -1,
		S3: s3Cfg(srv, "https://media.example.com"), MaxAttempts: 2}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := mustSave(t, s, "p.png", pngN(0), nil)
	if !strings.HasPrefix(m.URL, "https://media.example.com/") {
		t.Fatalf("url %q", m.URL)
	}
	if _, ok := fake.get(m.Filename); ok {
		t.Fatal("processing file already public")
	}

	j := mustLease(t, s)
	fake.setFailPuts(100)
	path, size := writeResult(t, s)
	if _, err := s.Complete(ctx, j.ID, j.LeaseToken, path, size, "abc"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("complete during outage: %v", err)
	}
	if got, _ := s.Get(ctx, m.ID); got.Status != StatusProcessing {
		t.Fatalf("media %+v", got)
	}
	if _, status, _ := jobState(t, s, m.ID); status != "queued" {
		t.Fatalf("job %s", status)
	}
	fake.setFailPuts(0)
	exec(t, s, `UPDATE media_jobs SET next_attempt_at = now()`)
	j = mustLease(t, s)
	if done, err := s.Complete(ctx, j.ID, j.LeaseToken, path, size, "abc"); err != nil || done.Status != StatusReady {
		t.Fatalf("complete after outage: %+v %v", done, err)
	}
	if o, ok := fake.get(m.Filename); !ok || o.header.Get("Content-Type") != "image/webp" {
		t.Fatalf("object %v %v", ok, o.header)
	}

	// The last attempt fails too: the media fails with "storage unavailable".
	m2 := mustSave(t, s, "q.png", pngN(1), nil)
	exec(t, s, `UPDATE media_jobs SET attempts = 1 WHERE media_id = $1`, m2.ID)
	j = mustLease(t, s)
	fake.setFailPuts(100)
	path, size = writeResult(t, s)
	if _, err := s.Complete(ctx, j.ID, j.LeaseToken, path, size, "def"); !errors.Is(err, ErrStorageUnavailable) {
		t.Fatalf("last attempt: %v", err)
	}
	if got, _ := s.Get(ctx, m2.ID); got.Status != StatusFailed || got.Error != "storage unavailable" {
		t.Fatalf("media %+v", got)
	}
}

// A fallback publish that hits a storage outage is retried too.
func TestFallbackStorageRetry(t *testing.T) {
	fake, srv := newFakeS3(t, "media-bucket")
	s, err := New(Deps{DB: testdb.New(t), Log: slog.New(slog.DiscardHandler), Dir: t.TempDir(), UploadWait: -1,
		S3: s3Cfg(srv, "https://media.example.com")})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := mustSave(t, s, "p.png", pngN(0), nil)
	j := mustLease(t, s)
	_, _ = s.Fail(ctx, j.ID, j.LeaseToken, "bad", false)
	fake.setFailPuts(100)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, m.ID); got.Status != StatusProcessing {
		t.Fatalf("media during outage %+v", got)
	}
	fake.setFailPuts(0)
	exec(t, s, `UPDATE media_jobs SET next_attempt_at = now()`)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, m.ID)
	if got.Status != StatusReady || got.Converted || !strings.HasSuffix(got.URL, ".png") {
		t.Fatalf("media %+v", got)
	}
	if _, ok := fake.get(got.Filename); !ok {
		t.Fatal("original not published")
	}
}

// A Retry that lands between the cleanup's listing and its delete keeps the
// original: the cleanup claims the row first and skips it when the claim fails.
func TestCleanupOriginalsRaceRetry(t *testing.T) {
	s, _ := newQueueStore(t, nil)
	ctx := context.Background()
	m := mustSave(t, s, "scan.bmp", bmpBytes(t), nil)
	j := mustLease(t, s)
	if _, err := s.Fail(ctx, j.ID, j.LeaseToken, "the image could not be decoded", false); err != nil {
		t.Fatal(err)
	}
	exec(t, s, `UPDATE media_jobs SET failed_at = now() - interval '25 hours'`)
	list, err := s.staleOriginals(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("stale originals = %v %v", list, err)
	}
	if _, err := s.Retry(ctx, m.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := s.dropOriginal(ctx, list[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.originalPath(stemOf(m.Filename))); err != nil {
		t.Fatal("original deleted under a retried job")
	}
	var deleted bool
	if err := s.db.QueryRow(ctx, `SELECT original_deleted FROM media_jobs WHERE id = $1`, list[0]).Scan(&deleted); err != nil || deleted {
		t.Fatalf("original_deleted = %v %v", deleted, err)
	}
	if _, status, _ := jobState(t, s, m.ID); status != "queued" {
		t.Fatalf("job status = %s", status)
	}
}
