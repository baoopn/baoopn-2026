// Package config loads all runtime configuration from environment variables.
// Every variable is documented in .env.example and the compose files.
package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the API server configuration.
type Config struct {
	AppEnv    string `env:"APP_ENV" envDefault:"development"`
	LogLevel  string `env:"LOG_LEVEL" envDefault:"info"`
	LogFormat string `env:"LOG_FORMAT" envDefault:"text"`
	HTTPAddr  string `env:"HTTP_ADDR" envDefault:":8080"`

	// PublicURL is the site origin (https://baoopn.com); used for links in
	// emails, Clerk authorized parties and the sitemap.
	PublicURL string `env:"PUBLIC_URL" envDefault:"http://localhost:5173"`

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

	// SpotifyAPIURL is the spotify-tracker public API (embedded widgets).
	SpotifyAPIURL string `env:"SPOTIFY_API_URL"`

	RateLimitPerMinute int `env:"RATE_LIMIT_PER_MINUTE" envDefault:"240"`
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
	c.SpotifyAPIURL = strings.TrimRight(c.SpotifyAPIURL, "/")
	c.MediaPublicURL = strings.TrimRight(c.MediaPublicURL, "/")
	if c.MediaPublicURL != "" {
		u, err := url.Parse(c.MediaPublicURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" ||
			(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("config: MEDIA_PUBLIC_URL must be an origin like https://media.example.com")
		}
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
