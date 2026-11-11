package media

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

// cropJPEG is a JPEG the crop below fits (8x4).
func cropJPEG() []byte { return testimg.JPEGWithExif(testimg.Halves(8, 4), 1, true) }

var testCrop = &Crop{X: 0, Y: 0, W: 4, H: 4}

// failedWithOriginal asserts media id failed with want as its error, nothing
// was published under any name, and its original is still on disk.
func failedWithOriginal(t *testing.T, s *Store, m Media, want string) {
	t.Helper()
	got, err := s.Get(context.Background(), m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusFailed || got.Error != want || !strings.Contains(got.Error, "uncropped") {
		t.Fatalf("media %+v, want failed %q", got, want)
	}
	if _, err := os.Stat(s.originalPath(stemOf(m.Filename))); err != nil {
		t.Fatalf("original not kept: %v", err)
	}
	stem := stemOf(m.Filename)
	matches, _ := filepath.Glob(filepath.Join(s.dir, stem+".*"))
	if len(matches) != 0 {
		t.Fatalf("published %v", matches)
	}
}

// The converter is down: a cropped upload fails instead of publishing its
// uncropped original, and nothing reaches the bucket.
func TestFallbackNeverPublishesUncroppedOriginal(t *testing.T) {
	fake, srv := newFakeS3(t, "media-bucket")
	s, err := New(Deps{DB: testdb.New(t), Log: slog.New(slog.DiscardHandler), Dir: t.TempDir(), UploadWait: -1,
		S3: s3Cfg(srv, "https://media.example.com"), FallbackAfter: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), testCrop)
	exec(t, s, `UPDATE media_jobs SET next_attempt_at = now() - interval '2 minutes'`)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	failedWithOriginal(t, s, m, "converter unavailable"+msgCropKept)
	fake.mu.Lock()
	n := len(fake.objs)
	fake.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d objects put to the store", n)
	}
	if phase, status, _ := jobState(t, s, m.ID); phase != PhaseConvert || status != "failed" {
		t.Fatalf("job %s %s", phase, status)
	}
}

// A worker's permanent failure of a cropped, browser-safe upload fails it.
func TestPermanentFailureWithCropFails(t *testing.T) {
	s, renames := newQueueStore(t, nil)
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), testCrop)
	j := mustLease(t, s)
	if retry, err := s.Fail(ctx, j.ID, j.LeaseToken, "the image could not be decoded", false); err != nil || retry {
		t.Fatalf("fail: %v %v", retry, err)
	}
	failedWithOriginal(t, s, m, "WebP conversion failed: the image could not be decoded"+msgCropKept)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	failedWithOriginal(t, s, m, "WebP conversion failed: the image could not be decoded"+msgCropKept)
	if len(*renames) != 0 {
		t.Fatalf("renames %v", *renames)
	}
}

// A cropped fallback row queued before this change fails when the janitor
// picks it up, without reading or publishing the original.
func TestQueuedFallbackWithCropFails(t *testing.T) {
	s, _ := newQueueStore(t, nil)
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), testCrop)
	exec(t, s, `UPDATE media_jobs SET phase = 'fallback', status = 'queued', next_attempt_at = now(),
		last_error = 'converter unavailable' WHERE media_id = $1`, m.ID)
	exec(t, s, `UPDATE media SET error = 'converter unavailable' WHERE id = $1`, m.ID)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	failedWithOriginal(t, s, m, "converter unavailable"+msgCropKept)
	if phase, status, _ := jobState(t, s, m.ID); phase != PhaseFallback || status != "failed" {
		t.Fatalf("job %s %s", phase, status)
	}
}

// Retrying a cropped upload that failed this way re-queues its conversion
// with the same crop.
func TestRetryAfterCropFailureKeepsCrop(t *testing.T) {
	s, _ := newQueueStore(t, nil)
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), testCrop)
	j := mustLease(t, s)
	_, _ = s.Fail(ctx, j.ID, j.LeaseToken, "bad", false)
	got, err := s.Retry(ctx, m.ID)
	if err != nil || got.Status != StatusProcessing {
		t.Fatalf("retry: %+v %v", got, err)
	}
	j = mustLease(t, s)
	if j.Crop == nil || *j.Crop != *testCrop || j.Attempt != 1 {
		t.Fatalf("re-leased job %+v crop %+v", j, j.Crop)
	}
	if phase, status, _ := jobState(t, s, m.ID); phase != PhaseConvert || status != "running" {
		t.Fatalf("job %s %s", phase, status)
	}
}

// deadWorker makes media m's running job look abandoned for ago.
func deadWorker(t *testing.T, s *Store, m Media, ago time.Duration) {
	t.Helper()
	exec(t, s, `UPDATE media_jobs SET lease_until = now() - make_interval(secs => $2) WHERE media_id = $1`,
		m.ID, ago.Seconds())
}

// A worker leased the job and stopped responding; no worker re-leased it for
// FallbackAfter. The janitor publishes the original, before the last attempt.
func TestRunningJobWithDeadWorkerFallsBack(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.FallbackAfter = time.Minute; d.MaxAttempts = 3 })
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), nil)
	j := mustLease(t, s)
	if j.Attempt >= j.MaxAttempts {
		t.Fatalf("attempts %d/%d", j.Attempt, j.MaxAttempts)
	}
	deadWorker(t, s, m, time.Minute+time.Second)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, m.ID)
	if got.Status != StatusReady || got.Converted ||
		got.Error != "converter unavailable: the worker stopped responding; saved the original" {
		t.Fatalf("media %+v", got)
	}
}

// A lease that expired less than FallbackAfter ago is left for a worker.
func TestRunningJobRecentlyExpiredUntouched(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.FallbackAfter = time.Minute; d.MaxAttempts = 3 })
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), nil)
	mustLease(t, s)
	deadWorker(t, s, m, 30*time.Second)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, m.ID); got.Status != StatusProcessing || got.Error != "" {
		t.Fatalf("media %+v", got)
	}
	if phase, status, attempts := jobState(t, s, m.ID); phase != PhaseConvert || status != "running" || attempts != 1 {
		t.Fatalf("job %s %s %d", phase, status, attempts)
	}
	// A worker can still pick it up.
	if j := mustLease(t, s); j.Attempt != 2 {
		t.Fatalf("re-lease %+v", j)
	}
}

// A cropped job whose worker died fails instead of falling back.
func TestCroppedRunningJobWithDeadWorkerFails(t *testing.T) {
	s, _ := newQueueStore(t, func(d *Deps) { d.FallbackAfter = time.Minute; d.MaxAttempts = 3 })
	ctx := context.Background()
	m := mustSave(t, s, "photo.jpg", cropJPEG(), testCrop)
	mustLease(t, s)
	deadWorker(t, s, m, time.Minute+time.Second)
	if err := s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	failedWithOriginal(t, s, m, "converter unavailable: the worker stopped responding"+msgCropKept)
}
