package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/analytics"
	"github.com/baoopn/baoopn-2026/internal/backups"
	"github.com/baoopn/baoopn-2026/internal/blog"
	"github.com/baoopn/baoopn-2026/internal/brand"
	"github.com/baoopn/baoopn-2026/internal/config"
	"github.com/baoopn/baoopn-2026/internal/contact"
	"github.com/baoopn/baoopn-2026/internal/credit"
	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/listening"
	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/mediaurls"
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
	store   *media.Store
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

	mediaStore, err := media.New(media.Deps{DB: pool, Log: log, Dir: cfg.MediaDir, MaxBytes: cfg.MaxUploadMB << 20,
		MaxPixels: cfg.MediaMaxPixels, UploadWait: cfg.MediaUploadWait, JobLease: cfg.MediaJobLease,
		MaxAttempts: cfg.MediaJobMaxAttempts, FallbackAfter: cfg.MediaFallbackAfter,
		FailedRetention: cfg.MediaFailedRetention, OnRename: followRename(pool, cfg.MediaPublicURL, cfg.PublicURL),
		OrphanGrace: orphanGrace(cfg.MediaOrphanGrace), OrphanMaxPerPass: cfg.MediaOrphanMaxPerPass,
		S3: mediaS3(cfg)})
	if err != nil {
		return nil, err
	}
	if cfg.S3Partial() {
		log.Warn("media bucket partly configured (need S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY); using MEDIA_DIR")
	}
	if mediaStore.UsesBucket() {
		log.Info("media stored in bucket", slog.String("bucket", cfg.S3Bucket),
			slog.String("public_url", cfg.MediaPublicURL))
		go migrateMedia(mediaStore, pool, cfg.MediaPublicURL, log)
	}

	brandMod := brand.NewModule(brand.Deps{DB: pool, Log: log, Files: mediaStore, CORSOrigins: cfg.BrandCORSOrigins})
	ensureBrand(ctx, brandMod)
	creditMod := credit.New(credit.Deps{DB: pool, Log: log, Brand: brandMod.Main, PublicURL: cfg.PublicURL})

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
		PublicURL: cfg.PublicURL, EmailLogo: emailLogo(brandMod.Main, cfg.PublicURL, log), InboundDomain: cfg.InboundDomain, InboundSecret: cfg.InboundSecret,
		TurnstileSecret: cfg.TurnstileSecret, HTTP: httpClient})

	rec := analytics.New(analytics.Deps{DB: pool, Log: log,
		Salt: analytics.DeriveSalt(cfg.SecretsKey), SiteHost: analytics.HostOf(cfg.PublicURL)})
	ov := overview.New(overview.Deps{DB: pool, Log: log, Analytics: rec})
	backupsMod := backups.New(backups.Deps{DB: pool, Log: log})

	return &modules{
		list: []httpapi.Module{profileMod, projectsMod, blogMod, mediaStore, listeningMod,
			contactMod, site, brandMod, creditMod, rec, ov, backupsMod},
		media:   mediaStore.Handler(),
		store:   mediaStore,
		rec:     rec,
		syncer:  syncer,
		contact: contactMod,
	}, nil
}

// ensureBrand uploads the default brand files of both brands (30 s at
// most). It only logs: a storage outage must not stop the boot.
func ensureBrand(ctx context.Context, b *brand.Module) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	b.Ensure(ctx)
}

// emailLogo reads the current email logo slot as an absolute URL for the
// mail layout; on any error the emails show the text badge.
func emailLogo(b *brand.Store, publicURL string, log *slog.Logger) func() (string, int, int) {
	return func() (string, int, int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		a, err := b.AbsoluteAsset(ctx, brand.EmailLogo, publicURL)
		if err != nil {
			log.Warn("email logo unavailable, using the text badge", logging.Err(err))
			return "", 0, 0
		}
		return a.URL, a.Width, a.Height
	}
}

// orphanGrace maps MEDIA_ORPHAN_GRACE=0 (cleanup off) to the store's
// "disabled" (negative); a zero Deps value would mean the default instead.
func orphanGrace(d time.Duration) time.Duration {
	if d <= 0 {
		return -1
	}
	return d
}

func mediaS3(cfg config.Config) media.S3Config {
	return media.S3Config{Endpoint: cfg.S3Endpoint, Region: cfg.S3Region, Bucket: cfg.S3Bucket,
		AccessKeyID: cfg.S3AccessKeyID, SecretAccessKey: cfg.S3SecretAccessKey,
		PathStyle: cfg.S3ForcePathStyle, PublicURL: cfg.MediaPublicURL}
}

// migrateMedia copies files still only in MEDIA_DIR to the bucket, then
// (with a public media domain) rewrites stored /media/<file> links to it.
// It runs in the background with a timeout and only logs: it never fails
// the boot.
func migrateMedia(s *media.Store, pool *pgxpool.Pool, publicURL string, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	migrateThenRewrite(ctx, s.MigrateToBucket, func(ctx context.Context) {
		mediaurls.RunLegacyRewrite(ctx, pool, publicURL, log)
	}, log)
}

// migrateThenRewrite runs the link rewrite only after a complete bucket
// migration. Rewriting links to files that never reached the bucket breaks
// them, and unsetting MEDIA_PUBLIC_URL afterwards does not undo it.
func migrateThenRewrite(ctx context.Context, migrate func(context.Context) (media.MigrateStats, error),
	rewrite func(context.Context), log *slog.Logger) {
	st, err := migrate(ctx)
	attrs := []any{slog.Int("uploaded", st.Uploaded), slog.Int("already_present", st.Present),
		slog.Int("not_on_disk", st.NoLocal), slog.Int("failed", st.Failed)}
	if err != nil || st.Failed > 0 {
		if err != nil {
			attrs = append(attrs, logging.Err(err))
		}
		log.Warn("media migration incomplete; media link rewrite skipped", attrs...)
		return
	}
	log.Info("media migration done", attrs...)
	rewrite(ctx)
}

// followRename points stored references at a file the media fallback
// published under a new name. Every link form to the old file follows: the
// bucket's public domain, the site's absolute /media URL and /media/<file>.
func followRename(pool *pgxpool.Pool, mediaPublicURL, sitePublicURL string) func(ctx context.Context, oldURL, newURL string) error {
	var bases []string
	if mediaPublicURL != "" {
		bases = append(bases, mediaPublicURL)
	}
	if sitePublicURL != "" {
		bases = append(bases, strings.TrimRight(sitePublicURL, "/")+"/media")
	}
	return func(ctx context.Context, oldURL, newURL string) error {
		st, err := mediaurls.Rewrite(ctx, pool, mediaurls.Rename(path.Base(oldURL), newURL, bases...))
		if err == nil && len(st.Skipped) > 0 {
			err = fmt.Errorf("rows kept changing, links not updated: %s", strings.Join(st.Skipped, ", "))
		}
		return err
	}
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
