// Package mediagrpc serves media.v1.MediaWorkerService from the API: the
// media worker leases conversion jobs, downloads originals and uploads WebP
// results through it. Every call needs "authorization: Bearer
// <INTERNAL_API_TOKEN>"; the server listens on the private network only.
package mediagrpc

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	mediav1 "github.com/baoopn/baoopn-2026/gen/media/v1"
	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/media"
)

const (
	// ChunkSize is the size of streamed file chunks.
	ChunkSize = 256 << 10
	// MaxMsgSize caps one gRPC message (a chunk plus framing).
	MaxMsgSize = 1 << 20
	// MaxResultBytes caps a whole uploaded WebP.
	MaxResultBytes = 64 << 20
)

// NewServer builds the gRPC server with auth, logging, recovery and health.
func NewServer(store *media.Store, token string, log *slog.Logger) *grpc.Server {
	srv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(recoverUnary(log), logUnary(log), authUnary(token)),
		grpc.ChainStreamInterceptor(recoverStream(log), logStream(log), authStream(token)),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: 10 * time.Second, PermitWithoutStream: true}),
		grpc.MaxRecvMsgSize(MaxMsgSize),
		grpc.MaxSendMsgSize(MaxMsgSize),
	)
	mediav1.RegisterMediaWorkerServiceServer(srv, &server{store: store, log: log})
	hs := health.NewServer()
	healthpb.RegisterHealthServer(srv, hs)
	return srv
}

// ─── Auth, logging, recovery ─────────────────────────────────────────────────

func authorized(ctx context.Context, method string, want []byte) error {
	if strings.HasPrefix(method, "/grpc.health.v1.Health/") {
		return nil
	}
	md, _ := metadata.FromIncomingContext(ctx)
	vals := md.Get("authorization")
	if len(want) == len("Bearer ") || len(vals) != 1 || subtle.ConstantTimeCompare([]byte(vals[0]), want) != 1 {
		return status.Error(codes.Unauthenticated, "invalid or missing token")
	}
	return nil
}

func authUnary(token string) grpc.UnaryServerInterceptor {
	want := []byte("Bearer " + token)
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		if err := authorized(ctx, info.FullMethod, want); err != nil {
			return nil, err
		}
		return h(ctx, req)
	}
}

func authStream(token string) grpc.StreamServerInterceptor {
	want := []byte("Bearer " + token)
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		if err := authorized(ss.Context(), info.FullMethod, want); err != nil {
			return err
		}
		return h(srv, ss)
	}
}

func logCall(log *slog.Logger, ctx context.Context, method string, start time.Time, err error) {
	code := status.Code(err)
	level := slog.LevelDebug
	attrs := []any{slog.String("method", method), slog.String("code", code.String()),
		slog.Duration("duration", time.Since(start))}
	if code != codes.OK {
		level = slog.LevelWarn
		attrs = append(attrs, logging.Err(err))
	}
	log.Log(ctx, level, "grpc request", attrs...)
}

func logUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := h(ctx, req)
		logCall(log, ctx, info.FullMethod, start, err)
		return resp, err
	}
}

func logStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
		start := time.Now()
		err := h(srv, ss)
		logCall(log, ss.Context(), info.FullMethod, start, err)
		return err
	}
}

func recoverUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if p := recover(); p != nil {
				log.ErrorContext(ctx, "grpc panic", slog.String("method", info.FullMethod), slog.Any("panic", p))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return h(ctx, req)
	}
}

func recoverStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) (err error) {
		defer func() {
			if p := recover(); p != nil {
				log.ErrorContext(ss.Context(), "grpc panic", slog.String("method", info.FullMethod), slog.Any("panic", p))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return h(srv, ss)
	}
}

func toStatus(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, media.ErrLeaseLost):
		return status.Error(codes.FailedPrecondition, "job is not leased with this token")
	case errors.Is(err, media.ErrStorageUnavailable):
		return status.Error(codes.Unavailable, "storage unavailable; the job was re-queued")
	case errors.Is(err, os.ErrNotExist):
		return status.Error(codes.NotFound, "original not found")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, "internal error")
	}
}

// ─── MediaWorkerService ──────────────────────────────────────────────────────

type server struct {
	mediav1.UnimplementedMediaWorkerServiceServer
	store *media.Store
	log   *slog.Logger
}

func (s *server) LeaseJob(ctx context.Context, req *mediav1.LeaseJobRequest) (*mediav1.LeaseJobResponse, error) {
	id := req.GetWorkerId()
	if len(id) > 200 {
		id = id[:200]
	}
	j, err := s.store.Lease(ctx, id)
	if err != nil || j == nil {
		return &mediav1.LeaseJobResponse{}, toStatus(err)
	}
	pj := &mediav1.Job{Id: j.ID, MediaId: j.MediaID, LeaseToken: j.LeaseToken,
		OriginalContentType: j.ContentType, OriginalSize: j.Size, Attempt: int32(j.Attempt),
		MaxAttempts: int32(j.MaxAttempts), LeaseSeconds: int64(time.Until(j.LeaseUntil).Seconds())}
	if c := j.Crop; c != nil {
		pj.Crop = &mediav1.Crop{X: int32(c.X), Y: int32(c.Y), Width: int32(c.W), Height: int32(c.H)}
	}
	return &mediav1.LeaseJobResponse{Job: pj}, nil
}

func (s *server) GetOriginal(req *mediav1.GetOriginalRequest, stream grpc.ServerStreamingServer[mediav1.GetOriginalResponse]) error {
	f, err := s.store.OpenOriginal(stream.Context(), req.GetJobId(), req.GetLeaseToken())
	if err != nil {
		return toStatus(err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, ChunkSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if serr := stream.Send(&mediav1.GetOriginalResponse{Chunk: buf[:n]}); serr != nil {
				return serr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return toStatus(err)
		}
	}
}

func (s *server) CompleteJob(stream grpc.ClientStreamingServer[mediav1.CompleteJobRequest, mediav1.CompleteJobResponse]) error {
	ctx := stream.Context()
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	meta := first.GetMetadata()
	if meta == nil {
		return status.Error(codes.InvalidArgument, "the first message must be the metadata")
	}
	tmp, err := s.store.CreateTemp(".wk-*")
	if err != nil {
		return toStatus(err)
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	hash := sha256.New()
	var size int64
	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		chunk := msg.GetChunk()
		if msg.GetMetadata() != nil {
			return status.Error(codes.InvalidArgument, "metadata sent twice")
		}
		size += int64(len(chunk))
		if size > MaxResultBytes {
			return status.Error(codes.ResourceExhausted, "result too large")
		}
		if _, err := tmp.Write(chunk); err != nil {
			return toStatus(err)
		}
		hash.Write(chunk)
	}
	if size == 0 {
		return status.Error(codes.InvalidArgument, "empty result")
	}
	if err := tmp.Close(); err != nil {
		return toStatus(err)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	m, err := s.store.Complete(ctx, meta.GetJobId(), meta.GetLeaseToken(), tmp.Name(), size, sum)
	if err != nil {
		return toStatus(err)
	}
	s.log.Info("media converted", slog.Int64("media", m.ID), slog.Int64("bytes", size),
		slog.Int("width", int(meta.GetWidth())), slog.Int("height", int(meta.GetHeight())))
	return stream.SendAndClose(&mediav1.CompleteJobResponse{Url: m.URL, SizeBytes: m.SizeBytes, Sha256: sum})
}

func (s *server) FailJob(ctx context.Context, req *mediav1.FailJobRequest) (*mediav1.FailJobResponse, error) {
	msg := strings.TrimSpace(req.GetError())
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300])
	}
	if msg == "" {
		msg = "unknown error"
	}
	retry, err := s.store.Fail(ctx, req.GetJobId(), req.GetLeaseToken(), msg, req.GetRetryable())
	if err != nil {
		return nil, toStatus(err)
	}
	return &mediav1.FailJobResponse{WillRetry: retry}, nil
}
