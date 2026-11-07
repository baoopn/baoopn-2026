// Package mediaworker is the media worker's loop: lease a conversion job
// from the API over gRPC, download the original, convert it to WebP
// (internal/imageconv) and upload the result, or report the failure.
package mediaworker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	mediav1 "github.com/baoopn/baoopn-2026/gen/media/v1"
	"github.com/baoopn/baoopn-2026/internal/imageconv"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// tokenCreds attaches the internal bearer token to every call.
type tokenCreds string

func (t tokenCreds) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(t)}, nil
}

// RequireTransportSecurity is false: traffic stays on the private Docker network.
func (tokenCreds) RequireTransportSecurity() bool { return false }

const maxMsg = 1 << 20

// Dial connects to the API's gRPC endpoint. Extra options are for tests (bufconn).
func Dial(addr, token string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(tokenCreds(token)),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxMsg), grpc.MaxCallSendMsgSize(maxMsg)),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: 30 * time.Second, Timeout: 10 * time.Second,
			PermitWithoutStream: true}),
	}, extra...)
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", addr, err)
	}
	return conn, nil
}

// Config tunes the worker.
type Config struct {
	ID          string
	Concurrency int
	// Poll is the wait after an empty LeaseJob (with jitter).
	Poll time.Duration
	// JobTimeout bounds one job; in-flight jobs get it after shutdown starts.
	JobTimeout time.Duration
	Convert    imageconv.Options
	// MaxOriginal caps a downloaded original.
	MaxOriginal int64
}

// Worker runs conversion jobs.
type Worker struct {
	c      mediav1.MediaWorkerServiceClient
	cfg    Config
	log    *slog.Logger
	lastOK atomic.Int64 // unix nanos of the last successful API call
	// convert is imageconv.Convert (replaceable in tests).
	convert func([]byte, string, imageconv.Options) (imageconv.Result, error)
}

// New returns a worker using client.
func New(client mediav1.MediaWorkerServiceClient, cfg Config, log *slog.Logger) *Worker {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 2
	}
	if cfg.Poll <= 0 {
		cfg.Poll = 2 * time.Second
	}
	if cfg.JobTimeout <= 0 {
		cfg.JobTimeout = 4 * time.Minute
	}
	if cfg.MaxOriginal <= 0 {
		cfg.MaxOriginal = 256 << 20
	}
	if cfg.ID == "" {
		cfg.ID = "mediaworker"
	}
	w := &Worker{c: client, cfg: cfg, log: log, convert: imageconv.Convert}
	w.lastOK.Store(time.Now().UnixNano())
	return w
}

// Run processes jobs with Concurrency loops until ctx is done; jobs in
// flight then get up to JobTimeout to finish.
func (w *Worker) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for i := range w.cfg.Concurrency {
		wg.Go(func() { w.loop(ctx, i) })
	}
	wg.Wait()
	return nil
}

func (w *Worker) sleep(ctx context.Context, d time.Duration) {
	d = d/2 + time.Duration(rand.Int64N(int64(d)+1)) // jitter so loops do not poll in step
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (w *Worker) loop(ctx context.Context, n int) {
	id := fmt.Sprintf("%s/%d", w.cfg.ID, n)
	errWait := w.cfg.Poll
	for ctx.Err() == nil {
		if worked, err := w.Once(ctx, id); err != nil {
			if ctx.Err() == nil {
				w.log.Warn("lease failed", logging.Err(err))
			}
			w.sleep(ctx, errWait)
			errWait = min(errWait*2, time.Minute)
			continue
		} else if !worked {
			w.sleep(ctx, w.cfg.Poll)
		}
		errWait = w.cfg.Poll
	}
}

// Once leases and processes at most one job; it reports whether there was one.
func (w *Worker) Once(ctx context.Context, workerID string) (bool, error) {
	lctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	resp, err := w.c.LeaseJob(lctx, &mediav1.LeaseJobRequest{WorkerId: workerID})
	cancel()
	if err != nil {
		return false, err
	}
	w.lastOK.Store(time.Now().UnixNano())
	job := resp.GetJob()
	if job == nil {
		return false, nil
	}
	// A job in flight finishes even when shutdown starts.
	jctx, jcancel := context.WithTimeout(context.WithoutCancel(ctx), w.cfg.JobTimeout)
	defer jcancel()
	w.process(jctx, job)
	return true, nil
}

func (w *Worker) process(ctx context.Context, job *mediav1.Job) {
	log := w.log.With(slog.Int64("job", job.GetId()), slog.Int64("media", job.GetMediaId()),
		slog.Int("attempt", int(job.GetAttempt())))
	start := time.Now()
	data, err := w.download(ctx, job)
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			log.Info("lease lost before download")
			return
		}
		w.fail(ctx, log, job, "download original: "+err.Error(), true)
		return
	}
	opts := w.cfg.Convert
	if c := job.GetCrop(); c != nil {
		opts.Crop = &imageconv.Crop{X: int(c.GetX()), Y: int(c.GetY()), W: int(c.GetWidth()), H: int(c.GetHeight())}
	}
	res, err := w.safeConvert(data, job.GetOriginalContentType(), opts)
	if err != nil {
		permanent := errors.Is(err, imageconv.ErrDecode) || errors.Is(err, imageconv.ErrTooLarge) ||
			errors.Is(err, imageconv.ErrCrop)
		w.fail(ctx, log, job, err.Error(), !permanent)
		return
	}
	if err := w.upload(ctx, job, res); err != nil {
		switch status.Code(err) {
		case codes.Unavailable:
			// The API could not store it and re-queued the job itself (or
			// the API is down: the lease expires and the job is re-leased).
			log.Warn("result not stored, job re-queued", logging.Err(err))
		case codes.FailedPrecondition, codes.NotFound:
			log.Info("lease lost before upload", logging.Err(err))
		default:
			w.fail(ctx, log, job, "upload result: "+err.Error(), true)
		}
		return
	}
	w.lastOK.Store(time.Now().UnixNano())
	log.Info("converted", slog.Int("in_bytes", len(data)), slog.Int("out_bytes", len(res.Data)),
		slog.Int("width", res.Width), slog.Int("height", res.Height), slog.Duration("took", time.Since(start)))
}

func (w *Worker) safeConvert(data []byte, ctype string, o imageconv.Options) (res imageconv.Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("converter panic: %v", p)
		}
	}()
	return w.convert(data, ctype, o)
}

func (w *Worker) download(ctx context.Context, job *mediav1.Job) ([]byte, error) {
	stream, err := w.c.GetOriginal(ctx, &mediav1.GetOriginalRequest{JobId: job.GetId(), LeaseToken: job.GetLeaseToken()})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if s := job.GetOriginalSize(); s > 0 && s <= w.cfg.MaxOriginal {
		buf.Grow(int(s))
	}
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
		if int64(buf.Len()+len(msg.GetChunk())) > w.cfg.MaxOriginal {
			return nil, errors.New("original too large")
		}
		buf.Write(msg.GetChunk())
	}
}

func (w *Worker) upload(ctx context.Context, job *mediav1.Job, res imageconv.Result) error {
	stream, err := w.c.CompleteJob(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&mediav1.CompleteJobRequest{Payload: &mediav1.CompleteJobRequest_Metadata{
		Metadata: &mediav1.CompleteJobMetadata{JobId: job.GetId(), LeaseToken: job.GetLeaseToken(),
			Width: int32(res.Width), Height: int32(res.Height)}}}); err != nil {
		_, rerr := stream.CloseAndRecv()
		return rerr
	}
	const chunk = 256 << 10
	for off := 0; off < len(res.Data); off += chunk {
		end := min(off+chunk, len(res.Data))
		if err := stream.Send(&mediav1.CompleteJobRequest{Payload: &mediav1.CompleteJobRequest_Chunk{
			Chunk: res.Data[off:end]}}); err != nil {
			_, rerr := stream.CloseAndRecv() // the real status
			return rerr
		}
	}
	_, err = stream.CloseAndRecv()
	return err
}

func (w *Worker) fail(ctx context.Context, log *slog.Logger, job *mediav1.Job, msg string, retryable bool) {
	resp, err := w.c.FailJob(ctx, &mediav1.FailJobRequest{JobId: job.GetId(), LeaseToken: job.GetLeaseToken(),
		Error: msg, Retryable: retryable})
	if err != nil {
		log.Warn("report failure", slog.String("error_reported", msg), logging.Err(err))
		return
	}
	log.Warn("conversion failed", slog.String("reason", msg), slog.Bool("retryable", retryable),
		slog.Bool("will_retry", resp.GetWillRetry()))
}

// Healthy reports whether the API answered within maxAge.
func (w *Worker) Healthy(maxAge time.Duration) bool {
	return time.Since(time.Unix(0, w.lastOK.Load())) < maxAge
}

// HealthHandler serves /healthz: 200 while the API answered recently.
func (w *Worker) HealthHandler() http.Handler {
	// A job may run for JobTimeout without any other call to the API.
	maxAge := w.cfg.JobTimeout + 3*w.cfg.Poll + time.Minute
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(rw http.ResponseWriter, _ *http.Request) {
		if !w.Healthy(maxAge) {
			http.Error(rw, "api unreachable", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(rw, "ok")
	})
	return mux
}
