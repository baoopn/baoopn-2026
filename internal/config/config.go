// Package config loads all runtime configuration from environment variables.
// Every variable is documented in .env.example and the compose files.
package config

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// defaultBrandCORS is BRAND_CORS_ORIGINS when the variable is not set.
const defaultBrandCORS = "https://listening.baoopn.com"

// Config is the API server configuration.
type Config struct {
	AppEnv    string `env:"APP_ENV" envDefault:"development"`
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"text"`
	HTTPAddr  string `env:"HTTP_ADDR" envDefault:":8080"`

	// PublicURL is the site origin (https://baoopn.com); used for links in
	// emails, Clerk authorized parties and the sitemap.
	PublicURL string `env:"PUBLIC_URL" envDefault:"http://localhost:5173"`

	// BrandCORSOrigins are the origins allowed to read the public
	// /api/v1/brand routes cross-origin.
	BrandCORSOrigins []string `env:"BRAND_CORS_ORIGINS" envSeparator:","`

	DatabaseURL    string `env:"DATABASE_URL,required,unset"`
	MigrateOnStart bool   `env:"MIGRATE_ON_START" envDefault:"true"`

	// SecretsKey (hex, 32 bytes) salts visitor hashes.
	SecretsKey string `env:"SECRETS_KEY,unset"`

	// Shared secret between the SvelteKit server and the API. The web server
	// adds it to proxied requests so the API trusts its client-IP header.
	InternalToken string `env:"INTERNAL_API_TOKEN,unset"`

	ClerkPublishableKey string   `env:"CLERK_PUBLISHABLE_KEY"`
	ClerkSecretKey      string   `env:"CLERK_SECRET_KEY,unset"`
	AdminAllowedEmails  []string `env:"ADMIN_ALLOWED_EMAILS" envSeparator:","`
	AdminAllowedUserIDs []string `env:"ADMIN_ALLOWED_USER_IDS" envSeparator:","`
	AdminRole           string   `env:"ADMIN_ROLE" envDefault:"admin"`

	GitHubUser         string        `env:"GITHUB_USER"`
	GitHubToken        string        `env:"GITHUB_TOKEN,unset"`
	GitHubSyncInterval time.Duration `env:"GITHUB_SYNC_INTERVAL" envDefault:"6h"`

	SMTPHost     string `env:"SMTP_HOST"`
	SMTPPort     int    `env:"SMTP_PORT" envDefault:"587"`
	SMTPUser     string `env:"SMTP_USER"`
	SMTPPassword string `env:"SMTP_PASSWORD,unset"`
	// MailFrom is the sender for outgoing mail, e.g. "Jane Doe <jane@mail.example.com>".
	MailFrom string `env:"MAIL_FROM"`
	// NotifyEmail receives new-message notifications (editable in settings).
	NotifyEmail string `env:"NOTIFY_EMAIL"`
	// InboundDomain receives replies as reply+<token>@<domain> through the
	// Cloudflare Email Worker; empty disables reply threading.
	InboundDomain string `env:"INBOUND_MAIL_DOMAIN"`
	// InboundSecret signs Email Worker → API webhook calls (HMAC-SHA256).
	InboundSecret string `env:"INBOUND_WEBHOOK_SECRET,unset"`

	TurnstileSecret string `env:"TURNSTILE_SECRET_KEY,unset"`

	MediaDir    string `env:"MEDIA_DIR" envDefault:"./data/media"`
	MaxUploadMB int64  `env:"MAX_UPLOAD_MB" envDefault:"10"`

	// S3-compatible media bucket (Cloudflare R2; RustFS in development). Used
	// only when the endpoint, bucket and both keys are set; MEDIA_DIR is the
	// fallback and the source of the startup migration.
	S3Endpoint        string `env:"S3_ENDPOINT"`
	S3Region          string `env:"S3_REGION" envDefault:"auto"`
	S3Bucket          string `env:"S3_BUCKET"`
	S3AccessKeyID     string `env:"S3_ACCESS_KEY_ID"`
	S3SecretAccessKey string `env:"S3_SECRET_ACCESS_KEY,unset"`
	// S3ForcePathStyle addresses <endpoint>/<bucket> (RustFS on localhost);
	// R2 uses the default virtual-hosted style.
	S3ForcePathStyle bool `env:"S3_FORCE_PATH_STYLE" envDefault:"false"`
	// MediaPublicURL is the bucket's public origin (https://media.example.com).
	// Set: media URLs point there and /media/<file> redirects to it. Empty:
	// the API streams objects at /media/<file>.
	MediaPublicURL string `env:"MEDIA_PUBLIC_URL"`

	// Raster uploads are converted to WebP by the media worker through a
	// queue the API serves over gRPC (GRPC_ADDR, private network only;
	// needs INTERNAL_API_TOKEN).
	GRPCAddr string `env:"GRPC_ADDR" envDefault:":9100"`
	// MediaMaxPixels rejects images with more pixels (decompression bombs).
	MediaMaxPixels int64 `env:"MEDIA_MAX_PIXELS" envDefault:"50000000"`
	// MediaUploadWait is how long an upload waits for its conversion
	// (201 when done, else 202 processing).
	MediaUploadWait time.Duration `env:"MEDIA_UPLOAD_WAIT" envDefault:"10s"`
	// MediaJobLease is how long a worker owns a job; keep it above the
	// worker's MEDIAWORKER_JOB_TIMEOUT.
	MediaJobLease       time.Duration `env:"MEDIA_JOB_LEASE" envDefault:"5m"`
	MediaJobMaxAttempts int           `env:"MEDIA_JOB_MAX_ATTEMPTS" envDefault:"5"`
	// MediaFallbackAfter: a job no worker leased for this long is published
	// as its metadata-stripped original (JPEG/PNG/GIF/WebP) or failed.
	MediaFallbackAfter time.Duration `env:"MEDIA_FALLBACK_AFTER" envDefault:"2m"`
	// MediaFailedRetention keeps a failed upload's original for a retry.
	MediaFailedRetention time.Duration `env:"MEDIA_FAILED_RETENTION" envDefault:"24h"`
	// MediaOrphanGrace: unreferenced media are deleted only after going this
	// long unused. 0 turns every orphan deletion off (the preview still works).
	MediaOrphanGrace time.Duration `env:"MEDIA_ORPHAN_GRACE" envDefault:"168h"`
	// MediaOrphanMaxPerPass caps the deletions of one cleanup run.
	MediaOrphanMaxPerPass int `env:"MEDIA_ORPHAN_MAX_PER_PASS" envDefault:"50"`

	// SpotifyAPIURL is the spotify-tracker public API (embedded widgets).
	SpotifyAPIURL string `env:"SPOTIFY_API_URL"`

	RateLimitPerMinute int `env:"RATE_LIMIT_PER_MINUTE" envDefault:"240"`
}

// Worker is the media worker's configuration (cmd/mediaworker).
type Worker struct {
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"text"`
	// APIGRPCAddr is the API's GRPC_ADDR as seen from the worker.
	APIGRPCAddr   string        `env:"API_GRPC_ADDR" envDefault:"localhost:9100"`
	InternalToken string        `env:"INTERNAL_API_TOKEN,unset"`
	Concurrency   int           `env:"MEDIAWORKER_CONCURRENCY" envDefault:"2"`
	PollInterval  time.Duration `env:"MEDIAWORKER_POLL_INTERVAL" envDefault:"2s"`
	JobTimeout    time.Duration `env:"MEDIAWORKER_JOB_TIMEOUT" envDefault:"4m"`
	// HealthAddr serves /healthz for `mediaworker healthcheck`.
	HealthAddr   string `env:"MEDIAWORKER_HEALTH_ADDR" envDefault:"127.0.0.1:8081"`
	MaxDimension int    `env:"MEDIA_MAX_DIMENSION" envDefault:"2400"`
	WebPQuality  int    `env:"MEDIA_WEBP_QUALITY" envDefault:"82"`
	MaxPixels    int64  `env:"MEDIA_MAX_PIXELS" envDefault:"50000000"`
}

// LoadWorker parses the media worker's environment.
func LoadWorker() (Worker, error) {
	var c Worker
	if err := env.Parse(&c); err != nil {
		return c, fmt.Errorf("config: %w", err)
	}
	switch {
	case c.InternalToken == "":
		return c, fmt.Errorf("config: INTERNAL_API_TOKEN is required")
	case c.Concurrency < 1 || c.Concurrency > 64:
		return c, fmt.Errorf("config: MEDIAWORKER_CONCURRENCY must be between 1 and 64")
	case c.MaxDimension < 16 || c.MaxDimension > 16384:
		return c, fmt.Errorf("config: MEDIA_MAX_DIMENSION must be between 16 and 16384")
	case c.WebPQuality < 1 || c.WebPQuality > 100:
		return c, fmt.Errorf("config: MEDIA_WEBP_QUALITY must be between 1 and 100")
	case c.MaxPixels < 1:
		return c, fmt.Errorf("config: MEDIA_MAX_PIXELS must be positive")
	}
	return c, nil
}

// IsProduction reports whether APP_ENV is production.
func (c Config) IsProduction() bool { return c.AppEnv == "production" }

// AuthEnabled reports whether the Clerk admin API is configured.
func (c Config) AuthEnabled() bool { return c.ClerkSecretKey != "" }

// MailEnabled reports whether SMTP sending is configured.
// A password still set to the .env placeholder ("REPLACE_...") counts as unset.
func (c Config) MailEnabled() bool {
	return c.SMTPHost != "" && c.MailFrom != "" && c.SMTPPassword != "" &&
		!strings.HasPrefix(c.SMTPPassword, "REPLACE_")
}

// S3Enabled reports whether the media bucket is configured.
func (c Config) S3Enabled() bool {
	return c.S3Endpoint != "" && c.S3Bucket != "" && c.S3AccessKeyID != "" && c.S3SecretAccessKey != ""
}

// S3Partial reports some but not all of the bucket settings (likely a typo).
func (c Config) S3Partial() bool {
	some := c.S3Endpoint != "" || c.S3Bucket != "" || c.S3AccessKeyID != "" || c.S3SecretAccessKey != ""
	return some && !c.S3Enabled()
}

// Load parses the environment.
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return c, fmt.Errorf("config: %w", err)
	}
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	// env applies envDefault to an empty variable too, so the default is set
	// here: unset means the default, empty means no origin.
	if _, ok := os.LookupEnv("BRAND_CORS_ORIGINS"); !ok {
		c.BrandCORSOrigins = []string{defaultBrandCORS}
	}
	origins, err := parseOrigins(c.BrandCORSOrigins)
	if err != nil {
		return c, err
	}
	c.BrandCORSOrigins = origins
	c.SpotifyAPIURL = strings.TrimRight(c.SpotifyAPIURL, "/")
	c.MediaPublicURL = strings.TrimRight(c.MediaPublicURL, "/")
	if c.MediaPublicURL != "" {
		u, err := url.Parse(c.MediaPublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("config: MEDIA_PUBLIC_URL must be an origin like https://media.example.com")
		}
	}
	if c.MediaOrphanGrace < 0 {
		return c, fmt.Errorf("config: MEDIA_ORPHAN_GRACE must not be negative (0 turns the cleanup off)")
	}
	if c.MediaOrphanMaxPerPass < 1 || c.MediaOrphanMaxPerPass > 10000 {
		return c, fmt.Errorf("config: MEDIA_ORPHAN_MAX_PER_PASS must be between 1 and 10000")
	}
	if c.IsProduction() {
		if c.SecretsKey == "" {
			return c, fmt.Errorf("config: SECRETS_KEY is required in production")
		}
		// Without it the API would trust X-Client-IP/X-Client-Country from anyone.
		if c.InternalToken == "" {
			return c, fmt.Errorf("config: INTERNAL_API_TOKEN is required in production")
		}
		if c.InboundDomain != "" && c.InboundSecret == "" {
			return c, fmt.Errorf("config: INBOUND_WEBHOOK_SECRET is required when INBOUND_MAIL_DOMAIN is set")
		}
	}
	return c, nil
}

// badOrigin is the error of parseOrigins for entry e.
func badOrigin(e string) error {
	return fmt.Errorf("config: BRAND_CORS_ORIGINS entry %q must be an origin like https://listening.example.com", e)
}

// canonicalPort reports whether p is empty or a port 1-65535 written
// without a leading zero.
func canonicalPort(p string) bool {
	if p == "" {
		return true
	}
	n, err := strconv.Atoi(p)
	return err == nil && n >= 1 && n <= 65535 && strconv.Itoa(n) == p
}

// parseOrigins trims, checks and normalizes a list of origins: scheme and
// host lowercased, the default port dropped, blanks and duplicates removed
// (order kept). An empty input gives an empty, non-nil slice.
func parseOrigins(in []string) ([]string, error) {
	out := []string{}
	for _, raw := range in {
		e := strings.TrimSpace(raw)
		if e == "" {
			continue
		}
		if e == "*" || strings.EqualFold(e, "null") {
			return nil, badOrigin(e)
		}
		u, err := url.Parse(e)
		if err != nil {
			return nil, badOrigin(e)
		}
		scheme := strings.ToLower(u.Scheme)
		if (scheme != "http" && scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
			return nil, badOrigin(e)
		}
		if strings.HasSuffix(u.Host, ":") || !canonicalPort(u.Port()) {
			return nil, badOrigin(e)
		}
		host := strings.ToLower(u.Host)
		switch {
		case scheme == "https" && strings.HasSuffix(host, ":443"):
			host = strings.TrimSuffix(host, ":443")
		case scheme == "http" && strings.HasSuffix(host, ":80"):
			host = strings.TrimSuffix(host, ":80")
		}
		o := scheme + "://" + host
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	return out, nil
}
