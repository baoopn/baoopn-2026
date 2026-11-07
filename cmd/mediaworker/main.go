// Command mediaworker converts uploaded images to WebP. It leases jobs from
// the API's queue over gRPC (API_GRPC_ADDR, INTERNAL_API_TOKEN), converts
// them with pure-Go codecs and uploads the results.
//
//	mediaworker              run the worker
//	mediaworker healthcheck  probe the local health endpoint (Docker HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"

	mediav1 "github.com/baoopn/baoopn-2026/gen/media/v1"
	"github.com/baoopn/baoopn-2026/internal/config"
	"github.com/baoopn/baoopn-2026/internal/imageconv"
	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/mediaworker"
	"github.com/baoopn/baoopn-2026/internal/platform/app"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		addr := os.Getenv("MEDIAWORKER_HEALTH_ADDR")
		if addr == "" {
			addr = "127.0.0.1:8081"
		}
		os.Exit(app.Healthcheck(addr))
	}
	if err := run(); err != nil {
		slog.Error("fatal", logging.Err(err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return err
	}
	log := logging.New("mediaworker", cfg.LogLevel, cfg.LogFormat)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	conn, err := mediaworker.Dial(cfg.APIGRPCAddr, cfg.InternalToken)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	host, _ := os.Hostname()
	w := mediaworker.New(mediav1.NewMediaWorkerServiceClient(conn), mediaworker.Config{
		ID: host, Concurrency: cfg.Concurrency, Poll: cfg.PollInterval, JobTimeout: cfg.JobTimeout,
		Convert: imageconv.Options{MaxDimension: cfg.MaxDimension, Quality: cfg.WebPQuality, MaxPixels: cfg.MaxPixels},
	}, log)
	log.Info("starting", slog.String("version", app.Version), slog.String("api", cfg.APIGRPCAddr),
		slog.Int("concurrency", cfg.Concurrency), slog.Int("max_dimension", cfg.MaxDimension),
		slog.Int("quality", cfg.WebPQuality))

	health := &http.Server{Addr: cfg.HealthAddr, Handler: w.HealthHandler(), ReadHeaderTimeout: 5 * time.Second}
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := health.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	g.Go(func() error {
		<-gctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return health.Shutdown(sctx)
	})
	g.Go(func() error {
		err := w.Run(gctx)
		log.Info("stopped")
		return err
	})
	return g.Wait()
}
