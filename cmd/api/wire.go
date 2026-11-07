package main

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/analytics"
	"github.com/baoopn/baoopn-2026/internal/blog"
	"github.com/baoopn/baoopn-2026/internal/config"
	"github.com/baoopn/baoopn-2026/internal/contact"
	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/listening"
	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/overview"
	"github.com/baoopn/baoopn-2026/internal/platform/app"
	"github.com/baoopn/baoopn-2026/internal/profile"
	"github.com/baoopn/baoopn-2026/internal/projects"
	"github.com/baoopn/baoopn-2026/internal/seedimport"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
	"github.com/baoopn/baoopn-2026/seed"
)

// modules holds every feature module plus the handles main needs for
// background jobs and shutdown.
type modules struct {
	list    []httpapi.Module
	media   http.Handler // serves /media/*
	rec     *analytics.Recorder
	syncer  *projects.Syncer
	contact *contact.Module
}

// buildModules constructs the feature modules in the order of spec §2.
func buildModules(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) (*modules, error) {
	httpClient := &http.Client{Timeout: 15 * time.Second}

	mailer := mail.NewSMTP(mail.Config{Enabled: cfg.MailEnabled(), Host: cfg.SMTPHost, Port: cfg.SMTPPort,
		User: cfg.SMTPUser, Password: cfg.SMTPPassword, From: cfg.MailFrom})
	if !cfg.MailEnabled() {
		log.Warn("mail is disabled (SMTP_PASSWORD empty or still the REPLACE_ placeholder)")
	}
	site := sitesettings.New(sitesettings.Deps{DB: pool, Log: log, DefaultNotify: cfg.NotifyEmail, Mailer: mailer})

	mediaStore, err := media.New(media.Deps{DB: pool, Log: log, Dir: cfg.MediaDir, MaxBytes: cfg.MaxUploadMB << 20})
	if err != nil {
		return nil, err
	}

	profileMod := profile.New(profile.Deps{DB: pool, Log: log, GitHubUser: cfg.GitHubUser,
		GitHubToken: cfg.GitHubToken, HTTP: httpClient, Seed: seed.FS})
	if err := profileMod.EnsureSeeded(ctx); err != nil {
		log.Warn("profile seed failed", logging.Err(err))
	}

	blogMod := blog.New(blog.Deps{DB: pool, Log: log})
	if err := seedimport.Run(ctx, seedimport.Deps{DB: pool, Log: log, Media: mediaStore, Blog: blogMod, FS: seed.FS}); err != nil {
		log.Warn("seed import failed", logging.Err(err))
	}

	syncer := projects.NewSyncer(projects.SyncDeps{DB: pool, Log: log, User: cfg.GitHubUser,
		Token: cfg.GitHubToken, HTTP: httpClient})
	projectsMod := projects.New(projects.Deps{DB: pool, Log: log, Syncer: syncer})

	listeningMod := listening.New(listening.Deps{BaseURL: cfg.SpotifyAPIURL, Log: log, HTTP: httpClient})

	contactMod := contact.New(contact.Deps{DB: pool, Log: log, Mailer: mailer, Settings: site,
		PublicURL: cfg.PublicURL, InboundDomain: cfg.InboundDomain, InboundSecret: cfg.InboundSecret,
		TurnstileSecret: cfg.TurnstileSecret, HTTP: httpClient})

	rec := analytics.New(analytics.Deps{DB: pool, Log: log,
		Salt: analytics.DeriveSalt(cfg.SecretsKey), SiteHost: analytics.HostOf(cfg.PublicURL)})
	ov := overview.New(overview.Deps{DB: pool, Log: log, Analytics: rec})

	return &modules{
		list: []httpapi.Module{profileMod, projectsMod, blogMod, mediaStore, listeningMod,
			contactMod, site, rec, ov},
		media:   mediaStore.Handler(),
		rec:     rec,
		syncer:  syncer,
		contact: contactMod,
	}, nil
}

// adminConfig is AdminConfig in types.ts.
type adminConfig struct {
	ClerkPublishableKey string `json:"clerk_publishable_key"`
	Version             string `json:"version"`
	MailEnabled         bool   `json:"mail_enabled"`
	InboundEnabled      bool   `json:"inbound_enabled"`
	GitHubUser          string `json:"github_user"`
}

// adminConfigHandler serves GET /api/admin/config (unauthenticated).
func adminConfigHandler(cfg config.Config) http.HandlerFunc {
	body := adminConfig{
		ClerkPublishableKey: cfg.ClerkPublishableKey,
		Version:             app.Version,
		MailEnabled:         cfg.MailEnabled(),
		InboundEnabled:      cfg.InboundDomain != "" && cfg.InboundSecret != "",
		GitHubUser:          cfg.GitHubUser,
	}
	return func(w http.ResponseWriter, _ *http.Request) {
		httpapi.JSON(w, http.StatusOK, body)
	}
}
