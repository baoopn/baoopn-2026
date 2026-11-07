// Command api is the baoopn.com backend: public API, admin API, webhooks.
//
//	api              run the server
//	api healthcheck  probe the local server (Docker HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	// The distroless image has no zoneinfo; analytics loads IANA zones.
	_ "time/tzdata"

	"google.golang.org/grpc"

	"github.com/baoopn/baoopn-2026/internal/auth"
	"github.com/baoopn/baoopn-2026/internal/config"
	"github.com/baoopn/baoopn-2026/internal/db"
	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/mediagrpc"
	"github.com/baoopn/baoopn-2026/internal/platform/app"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(app.Healthcheck(envOr("HTTP_ADDR", ":8080")))
	}
	if err := run(); err != nil {
		slog.Error("fatal", logging.Err(err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logging.New("api", cfg.LogLevel, cfg.LogFormat)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
	}

	mods, err := buildModules(ctx, cfg, pool, log)
	if err != nil {
		return err
	}
	var adminAuth func(http.Handler) http.Handler
	if cfg.AuthEnabled() {
		adminAuth = auth.New(auth.Config{
			SecretKey:      cfg.ClerkSecretKey,
			AllowedEmails:  cfg.AdminAllowedEmails,
			AllowedUserIDs: cfg.AdminAllowedUserIDs,
			AdminRole:      cfg.AdminRole,
			PublicURL:      cfg.PublicURL,
			Dev:            !cfg.IsProduction(),
		}, log).Middleware
	} else {
		log.Warn("CLERK_SECRET_KEY is not set; the admin API is disabled")
	}
	handler := httpapi.New(httpapi.Deps{
		Log:         log,
		DB:          pool,
		AdminAuth:   adminAuth,
		ClientIP:    httpapi.TrustedClientIP(cfg.InternalToken),
		PublicRPM:   cfg.RateLimitPerMinute,
		Media:       mods.media,
		Modules:     mods.list,
		Version:     app.Version,
		AdminConfig: adminConfigHandler(cfg),
	})

	// Background jobs run on their own context so that they outlive the
	// HTTP server's shutdown (in-flight beacons are still recorded).
	jobCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()
	var wg sync.WaitGroup
	wg.Go(func() { mods.rec.Run(jobCtx) })
	if cfg.GitHubSyncInterval > 0 {
		wg.Go(func() { mods.syncer.Run(jobCtx, cfg.GitHubSyncInterval) })
	}
	// Media queue upkeep: expired leases, fallbacks, original cleanup.
	wg.Go(func() { mods.store.RunJanitor(jobCtx, media.JanitorInterval) })

	// The media worker's gRPC API (private network only).
	var grpcSrv *grpc.Server
	if cfg.InternalToken != "" && cfg.GRPCAddr != "" {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			return fmt.Errorf("grpc listen: %w", err)
		}
		grpcSrv = mediagrpc.NewServer(mods.store, cfg.InternalToken, log)
		go func() {
			log.Info("grpc listening", slog.String("addr", cfg.GRPCAddr))
			if err := grpcSrv.Serve(lis); err != nil {
				log.Error("grpc server", logging.Err(err))
			}
		}()
	} else {
		log.Warn("INTERNAL_API_TOKEN is not set; the media worker API is off and uploads fall back to originals")
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Bounds slow clients; uploads (up to MAX_UPLOAD_MB) and 10 MiB inbound mail fit easily.
		ReadTimeout:  2 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", cfg.HTTPAddr), slog.String("version", app.Version))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
		close(errc)
	}()
	var runErr error
	select {
	case err := <-errc:
		runErr = fmt.Errorf("http: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := srv.Shutdown(shutdownCtx); err != nil && runErr == nil {
		runErr = err
	}
	cancel()
	if grpcSrv != nil {
		stopGRPC(grpcSrv, 10*time.Second)
	}
	// Contact notifications are sent in the background; let them finish.
	cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
	mods.contact.Shutdown(cctx)
	ccancel()
	cancelJobs()
	waitTimeout(&wg, 10*time.Second, log)
	return runErr
}

// waitTimeout waits for the background jobs, giving up after d.
func waitTimeout(wg *sync.WaitGroup, d time.Duration, log *slog.Logger) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		log.Warn("background jobs did not stop in time")
	}
}

// stopGRPC drains the gRPC server, forcing it closed after d.
func stopGRPC(s *grpc.Server, d time.Duration) {
	done := make(chan struct{})
	go func() { s.GracefulStop(); close(done) }()
	select {
	case <-done:
	case <-time.After(d):
		s.Stop()
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
