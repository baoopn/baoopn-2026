package mediaworker

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/image/webp"
	"google.golang.org/grpc/test/bufconn"

	mediav1 "github.com/baoopn/baoopn-2026/gen/media/v1"
	"github.com/baoopn/baoopn-2026/internal/imageconv"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/mediagrpc"
	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

// newEnvWith is newEnv with store options (attempts, fallback timing); it
// also returns the pool so tests can age jobs.
func newEnvWith(t *testing.T, edit func(*media.Deps)) (*env, *pgxpool.Pool) {
	t.Helper()
	dir := t.TempDir()
	log := slog.New(slog.DiscardHandler)
	pool := testdb.New(t)
	d := media.Deps{DB: pool, Log: log, Dir: dir, UploadWait: -1}
	if edit != nil {
		edit(&d)
	}
	store, err := media.New(d)
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	srv := mediagrpc.NewServer(store, token, log)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return &env{store: store, dir: dir, lis: lis}, pool
}

func heicFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "imageconv", "testdata", "tiny.heic"))
	if err != nil {
		t.Skipf("HEIC fixture missing: %v", err)
	}
	return data
}

func readWebP(t *testing.T, e *env, m media.Media) (int, int) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.dir, m.Filename))
	if err != nil {
		t.Fatal(err)
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("%s is not a WebP: %v", m.Filename, err)
	}
	if bytes.Contains(data, []byte("GPSDATA")) {
		t.Fatal("GPS kept")
	}
	b := img.Bounds()
	return b.Dx(), b.Dy()
}

// JPEG, PNG and HEIC uploads, each cropped, come out as WebP of the crop.
func TestFormatsConvertWithCrop(t *testing.T) {
	e, _ := newEnvWith(t, nil)
	ctx := context.Background()
	w := New(e.client(t, token), Config{ID: "test", Convert: imageconv.Options{MaxDimension: 4096}},
		slog.New(slog.DiscardHandler))
	for _, c := range []struct {
		name  string
		data  []byte
		crop  media.Crop
		w, h  int
		ctype string
	}{
		// Oriented 32×64 (EXIF 6), cropped to the lower 32×32.
		{"photo.jpg", testimg.JPEGWithExif(testimg.Halves(64, 32), 6, true), media.Crop{X: 0, Y: 32, W: 32, H: 32}, 32, 32, "image/jpeg"},
		{"shot.png", testimg.PNG(testimg.Halves(40, 20)), media.Crop{X: 4, Y: 2, W: 20, H: 10}, 20, 10, "image/png"},
		// 640×480 with a 90° irot: oriented 480×640.
		{"phone.heic", heicFixture(t), media.Crop{X: 0, Y: 100, W: 480, H: 300}, 480, 300, "image/heic"},
	} {
		t.Run(c.name, func(t *testing.T) {
			crop := c.crop
			m, err := e.store.SaveCrop(ctx, c.name, bytes.NewReader(c.data), &crop)
			if err != nil || m.Status != media.StatusProcessing {
				t.Fatalf("save %+v %v", m, err)
			}
			if worked, err := w.Once(ctx, "test/0"); err != nil || !worked {
				t.Fatalf("once: %v %v", worked, err)
			}
			got, err := e.store.Get(ctx, m.ID)
			if err != nil || got.Status != media.StatusReady || !got.Converted || got.ContentType != "image/webp" ||
				!strings.HasSuffix(got.Filename, ".webp") || got.Error != "" {
				t.Fatalf("media %+v %v", got, err)
			}
			if gw, gh := readWebP(t, e, got); gw != c.w || gh != c.h {
				t.Fatalf("result %dx%d, want %dx%d", gw, gh, c.w, c.h)
			}
		})
	}
}

// A worker that leases a job and dies leaves it running; once the lease
// expires another worker takes it as attempt 2 and finishes it.
func TestWorkerCrashLeaseExpiryRetry(t *testing.T) {
	e, _ := newEnvWith(t, func(d *media.Deps) { d.JobLease = time.Second })
	ctx := context.Background()
	m, err := e.store.Save(ctx, "p.png", bytes.NewReader(testimg.PNG(testimg.Halves(8, 8))))
	if err != nil {
		t.Fatal(err)
	}
	// The "crashed" worker: leases over gRPC and never reports back.
	dead, err := e.client(t, token).LeaseJob(ctx, &mediav1.LeaseJobRequest{WorkerId: "dead"})
	if err != nil || dead.GetJob() == nil || dead.GetJob().GetAttempt() != 1 {
		t.Fatalf("lease: %+v %v", dead, err)
	}
	w := e.worker(t)
	if worked, err := w.Once(ctx, "live"); err != nil || worked {
		t.Fatalf("leased while the lease was held: %v %v", worked, err)
	}
	time.Sleep(1200 * time.Millisecond)
	if err := e.store.Tick(ctx); err != nil { // not the last attempt: no fallback
		t.Fatal(err)
	}
	if worked, err := w.Once(ctx, "live"); err != nil || !worked {
		t.Fatalf("retry after expiry: %v %v", worked, err)
	}
	got, _ := e.store.Get(ctx, m.ID)
	if got.Status != media.StatusReady || !got.Converted {
		t.Fatalf("media %+v", got)
	}
	// The crashed worker's late completion is refused.
	if _, err := e.client(t, token).FailJob(ctx, &mediav1.FailJobRequest{JobId: dead.GetJob().GetId(),
		LeaseToken: dead.GetJob().GetLeaseToken(), Error: "late", Retryable: true}); err == nil {
		t.Fatal("stale lease accepted")
	}
}

// The worker dies on the job's last attempt: the janitor publishes the
// stripped original once the lease expires.
func TestCrashOnLastAttemptFallsBack(t *testing.T) {
	e, _ := newEnvWith(t, func(d *media.Deps) { d.JobLease = time.Second; d.MaxAttempts = 1 })
	ctx := context.Background()
	m, err := e.store.Save(ctx, "photo.jpg", bytes.NewReader(testimg.JPEGWithExif(testimg.Halves(8, 4), 1, true)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.client(t, token).LeaseJob(ctx, &mediav1.LeaseJobRequest{WorkerId: "dead"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1200 * time.Millisecond)
	if worked, _ := e.worker(t).Once(ctx, "live"); worked {
		t.Fatal("re-leased past max attempts")
	}
	if err := e.store.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.Get(ctx, m.ID)
	if got.Status != media.StatusReady || got.Converted || got.ContentType != "image/jpeg" ||
		!strings.HasSuffix(got.Filename, ".jpg") || !strings.Contains(got.Error, "did not finish in time") {
		t.Fatalf("fallback %+v", got)
	}
	stored, err := os.ReadFile(filepath.Join(e.dir, got.Filename))
	if err != nil || bytes.Contains(stored, []byte("GPSDATA")) {
		t.Fatalf("published original not stripped (%v)", err)
	}
}

// HEIC with no worker past FallbackAfter fails (browsers cannot show it);
// Retry re-queues it and a worker then converts it.
func TestHEICWorkerDownThenRetry(t *testing.T) {
	e, pool := newEnvWith(t, func(d *media.Deps) { d.FallbackAfter = time.Minute })
	ctx := context.Background()
	m, err := e.store.Save(ctx, "phone.heic", bytes.NewReader(heicFixture(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE media_jobs SET next_attempt_at = now() - interval '2 minutes'`); err != nil {
		t.Fatal(err)
	}
	if err := e.store.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.Get(ctx, m.ID)
	if got.Status != media.StatusFailed || got.Error != "converter unavailable" {
		t.Fatalf("heic without worker %+v", got)
	}

	r := chi.NewRouter()
	e.store.Admin(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, fmt.Sprintf("/media/%d/retry", m.ID), nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry: %d %s", rec.Code, rec.Body)
	}
	if worked, err := e.worker(t).Once(ctx, "test"); err != nil || !worked {
		t.Fatalf("once: %v %v", worked, err)
	}
	got, _ = e.store.Get(ctx, m.ID)
	if got.Status != media.StatusReady || !got.Converted || got.ContentType != "image/webp" {
		t.Fatalf("after retry %+v", got)
	}
}

// Upload guards at the HTTP edge: a 60 MP PNG is 413, HTML named .png is 415.
func TestUploadRejects(t *testing.T) {
	e, pool := newEnvWith(t, nil)
	r := chi.NewRouter()
	e.store.Admin(r)
	post := func(name string, data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, _ := mw.CreateFormFile("file", name)
		_, _ = fw.Write(data)
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/media", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	big := testimg.PNGSetSize(testimg.PNG(testimg.Halves(4, 4)), 10000, 6000)
	if rec := post("big.png", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("60 MP: %d %s", rec.Code, rec.Body)
	}
	if rec := post("page.png", []byte("<!doctype html><html><script>alert(1)</script></html>")); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("html as png: %d %s", rec.Code, rec.Body)
	}
	var count int
	_ = pool.QueryRow(context.Background(), `SELECT count(*) FROM media`).Scan(&count)
	if count != 0 {
		t.Fatalf("%d rows stored for rejected uploads", count)
	}
}
