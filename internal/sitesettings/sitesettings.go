// Package sitesettings stores the admin-editable site settings (contact form,
// notification address, auto-reply, signature) under the settings key "site".
package sitesettings

import (
	"context"
	"log/slog"
	"net/mail"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

// Key is the settings key the document is stored under.
const Key = "site"

// Mailer sends mail. *mail.SMTP implements it.
type Mailer interface {
	Enabled() bool
	Send(context.Context, bmail.Message) (string, error)
	// FromName and FromAddress are the sender, also when mail is disabled.
	FromName() string
	FromAddress() string
}

// Settings is the SiteSettings document.
type Settings struct {
	NotifyEmail      string `json:"notify_email"`
	ContactEnabled   bool   `json:"contact_enabled"`
	AutoReplyEnabled bool   `json:"auto_reply_enabled"`
	AutoReplyText    string `json:"auto_reply_text"`
	Signature        string `json:"signature"`
}

// Defaults are the settings before anything was saved.
func Defaults() Settings {
	return Settings{
		ContactEnabled: true,
		AutoReplyText:  "Hi {name},\n\nThanks for reaching out. I got your message and will get back to you soon. You can reply to this email to add anything.\n\nBao",
		Signature:      "Bao Nguyen\nhttps://baoopn.com",
	}
}

// Deps are the store's dependencies.
type Deps struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
	// DefaultNotify (NOTIFY_EMAIL) is used when notify_email is empty.
	DefaultNotify string
	Mailer        Mailer
}

// Store reads and writes the settings, cached in memory (one API instance).
type Store struct {
	d      Deps
	mu     sync.Mutex
	cached *Settings
}

// New returns the store.
func New(d Deps) *Store {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Store{d: d}
}

// Get returns the settings, the defaults filled in for what was never saved.
// On a database error it returns the defaults and the error.
func (s *Store) Get(ctx context.Context) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil {
		return *s.cached, nil
	}
	v := Defaults()
	if _, err := settings.Get(ctx, s.d.DB, Key, &v); err != nil {
		return Defaults(), err
	}
	s.cached = &v
	return v, nil
}

// put validates nothing: callers pass a normalised document.
func (s *Store) put(ctx context.Context, v Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := settings.Put(ctx, s.d.DB, Key, v); err != nil {
		s.cached = nil
		return err
	}
	s.cached = &v
	return nil
}

// NotifyTo is the address that receives notifications: notify_email, or
// DefaultNotify when that is empty (or the settings cannot be read).
func (s *Store) NotifyTo(ctx context.Context) string {
	v, err := s.Get(ctx)
	if err != nil {
		s.d.Log.Warn("sitesettings: read failed, using NOTIFY_EMAIL", slog.Any("err", err))
	}
	if v.NotifyEmail != "" {
		return v.NotifyEmail
	}
	return s.d.DefaultNotify
}

// normalise trims and checks v, returning the field at fault and why.
func normalise(v Settings) (Settings, string) {
	v.NotifyEmail = strings.TrimSpace(v.NotifyEmail)
	if v.NotifyEmail != "" {
		a, err := mail.ParseAddress(v.NotifyEmail)
		if err != nil || len(a.Address) > 254 || !strings.Contains(a.Address, "@") {
			return v, "notify_email must be a valid email address"
		}
		v.NotifyEmail = a.Address
	}
	v.AutoReplyText = strings.TrimSpace(bmail.Tidy(v.AutoReplyText))
	if utf8.RuneCountInString(v.AutoReplyText) > 2000 {
		return v, "auto_reply_text must be at most 2000 characters"
	}
	v.Signature = strings.TrimSpace(bmail.Tidy(v.Signature))
	if utf8.RuneCountInString(v.Signature) > 500 {
		return v, "signature must be at most 500 characters"
	}
	return v, ""
}
