package mediaworker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/webp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	mediav1 "github.com/baoopn/baoopn-2026/gen/media/v1"
	"github.com/baoopn/baoopn-2026/internal/imageconv"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/mediagrpc"
	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/internal/testimg"
)

const token = "test-internal-token"

type env struct {
	store *media.Store
	dir   string
	lis   *bufconn.Listener
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	log := slog.New(slog.DiscardHandler)
	store, err := media.New(media.Deps{DB: testdb.New(t), Log: log, Dir: dir, UploadWait: -1})
	if err != nil {
		t.Fatal(err)
	}
	lis := bufconn.Listen(1 << 20)
	srv := mediagrpc.NewServer(store, token, log)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return &env{store: store, dir: dir, lis: lis}
}

func (e *env) client(t *testing.T, tok string) mediav1.MediaWorkerServiceClient {
	t.Helper()
	conn, err := Dial("passthrough:///bufnet", tok, grpc.WithContextDialer(
		func(ctx context.Context, _ string) (net.Conn, error) { return e.lis.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return mediav1.NewMediaWorkerServiceClient(conn)
}

func (e *env) worker(t *testing.T) *Worker {
	return New(e.client(t, token), Config{ID: "test", Convert: imageconv.Options{MaxDimension: 32}},
		slog.New(slog.DiscardHandler))
}

func TestFullFlow(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	src := testimg.JPEGWithExif(testimg.Halves(64, 32), 6, true)
	m, err := e.store.SaveCrop(ctx, "photo.jpg", bytes.NewReader(src), &media.Crop{X: 0, Y: 0, W: 32, H: 48})
	if err != nil || m.Status != media.StatusProcessing {
		t.Fatalf("save %+v %v", m, err)
	}
	w := e.worker(t)
	worked, err := w.Once(ctx, "test/0")
	if err != nil || !worked {
		t.Fatalf("once: %v %v", worked, err)
	}
	got, err := e.store.Get(ctx, m.ID)
	if err != nil || got.Status != media.StatusReady || !got.Converted || got.ContentType != "image/webp" {
		t.Fatalf("media %+v %v", got, err)
	}
	data, err := os.ReadFile(filepath.Join(e.dir, got.Filename))
	if err != nil || int64(len(data)) != got.SizeBytes {
		t.Fatalf("stored file: %v", err)
	}
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	// Oriented 32×64, cropped to 32×48, then fit into 32 px: 21×32.
	if b := img.Bounds(); b.Dx() != 21 || b.Dy() != 32 {
		t.Fatalf("result %v", b)
	}
	if bytes.Contains(data, []byte("GPSDATA")) {
		t.Fatal("GPS kept")
	}
	if _, err := os.Stat(filepath.Join(e.dir, "originals", got.Filename[:24])); !os.IsNotExist(err) {
		t.Fatal("original not deleted")
	}
	if worked, err := w.Once(ctx, "test/0"); err != nil || worked {
		t.Fatalf("empty queue: %v %v", worked, err)
	}
	if !w.Healthy(1e9) {
		t.Fatal("not healthy after talking to the API")
	}
}

func TestAuthRejected(t *testing.T) {
	e := newEnv(t)
	for _, tok := range []string{"", "wrong"} {
		_, err := e.client(t, tok).LeaseJob(context.Background(), &mediav1.LeaseJobRequest{WorkerId: "x"})
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("token %q: %v", tok, err)
		}
		stream, err := e.client(t, tok).GetOriginal(context.Background(), &mediav1.GetOriginalRequest{JobId: 1})
		if err == nil {
			_, err = stream.Recv()
		}
		if status.Code(err) != codes.Unauthenticated {
			t.Fatalf("stream with token %q: %v", tok, err)
		}
	}
}

func TestRetryableAndPermanentFailures(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	m, err := e.store.Save(ctx, "p.png", bytes.NewReader(testimg.PNG(testimg.Halves(8, 8))))
	if err != nil {
		t.Fatal(err)
	}
	w := e.worker(t)
	w.convert = func([]byte, string, imageconv.Options) (imageconv.Result, error) {
		return imageconv.Result{}, errors.New("out of memory")
	}
	if worked, err := w.Once(ctx, "t"); err != nil || !worked {
		t.Fatalf("once: %v %v", worked, err)
	}
	// Retryable: re-queued with backoff, still processing, not leasable now.
	if got, _ := e.store.Get(ctx, m.ID); got.Status != media.StatusProcessing || got.Error != "" {
		t.Fatalf("after retryable failure %+v", got)
	}
	if j, _ := e.store.Lease(ctx, "x"); j != nil {
		t.Fatal("leasable during backoff")
	}

	// Permanent (undecodable), on a fresh upload: falls back to the original.
	m2, _ := e.store.Save(ctx, "q.png", bytes.NewReader(testimg.PNG(testimg.Halves(9, 8))))
	w.convert = func([]byte, string, imageconv.Options) (imageconv.Result, error) {
		return imageconv.Result{}, imageconv.ErrDecode
	}
	if worked, err := w.Once(ctx, "t"); err != nil || !worked {
		t.Fatalf("once: %v %v", worked, err)
	}
	if err := e.store.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.store.Get(ctx, m2.ID)
	if got.Status != media.StatusReady || got.Converted || got.ContentType != "image/png" {
		t.Fatalf("fallback %+v", got)
	}
}

func TestCompleteNeedsLease(t *testing.T) {
	e := newEnv(t)
	c := e.client(t, token)
	stream, err := c.CompleteJob(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = stream.Send(&mediav1.CompleteJobRequest{Payload: &mediav1.CompleteJobRequest_Metadata{
		Metadata: &mediav1.CompleteJobMetadata{JobId: 42, LeaseToken: "nope"}}})
	_ = stream.Send(&mediav1.CompleteJobRequest{Payload: &mediav1.CompleteJobRequest_Chunk{Chunk: []byte("x")}})
	if _, err := stream.CloseAndRecv(); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("complete without lease: %v", err)
	}
}
