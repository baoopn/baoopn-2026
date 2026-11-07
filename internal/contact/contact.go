// Package contact is the contact form and the email threads it starts, shown
// as chats in the admin inbox: admin replies go out by mail, and visitor
// replies come back through the Cloudflare Email Worker (POST
// /api/hooks/inbound-email).
package contact

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
	"github.com/baoopn/baoopn-2026/internal/sitesettings"
)

// Mailer sends mail; *mail.SMTP implements it.
type Mailer = sitesettings.Mailer

// Deps are the module's dependencies.
type Deps struct {
	DB       *pgxpool.Pool
	Log      *slog.Logger
	Mailer   Mailer
	Settings *sitesettings.Store
	// PublicURL is the site origin, for admin links in notifications.
	PublicURL string
	// InboundDomain receives replies as reply+<token>@<domain>; empty sends
	// Reply-To the notify address instead.
	InboundDomain string
	// InboundSecret signs Email Worker calls; empty disables the hook.
	InboundSecret string
	// TurnstileSecret enables the captcha check on the form.
	TurnstileSecret string
	HTTP            *http.Client
	Now             func() time.Time
}

const turnstileURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// Module implements httpapi.Module.
type Module struct {
	d            Deps
	turnstileURL string
	bg           sync.WaitGroup

	// notified throttles inbound notifications: thread id → last sent.
	mu       sync.Mutex
	notified map[int64]time.Time

	// Inbound hook limits (see inbound.go): concurrent body reads, signed
	// deliveries per minute, and junk reject rows per minute.
	reads       chan struct{}
	inboundRate *bucket
	junkRate    *bucket
	junkSkipped atomic.Int64

	// parse is bmail.ParseInbound; tests swap it to count calls.
	parse func([]byte) (bmail.Inbound, error)
}

// New returns the module.
func New(d Deps) *Module {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Mailer == nil {
		d.Mailer = bmail.NewSMTP(bmail.Config{})
	}
	if d.Settings == nil {
		d.Settings = sitesettings.New(sitesettings.Deps{DB: d.DB, Log: d.Log, Mailer: d.Mailer})
	}
	return &Module{
		d: d, turnstileURL: turnstileURL, notified: map[int64]time.Time{},
		reads:       make(chan struct{}, inboundReads),
		inboundRate: newBucket(inboundPerMinute, time.Minute),
		junkRate:    newBucket(junkRejectsPerMinute, time.Minute),
		parse:       bmail.ParseInbound,
	}
}

// Public mounts POST /contact (5 per hour per IP).
func (m *Module) Public(r chi.Router) {
	r.With(httpapi.RateLimitWindow(5, time.Hour)).Post("/contact", m.handleContact)
}

// Admin mounts the inbox.
func (m *Module) Admin(r chi.Router) {
	r.Get("/threads", m.handleList)
	r.Get("/threads/unread-count", m.handleUnreadCount)
	r.Get("/threads/{id}", m.handleDetail)
	r.Patch("/threads/{id}", m.handlePatch)
	r.Delete("/threads/{id}", m.handleDelete)
	r.Post("/threads/{id}/read", m.handleRead(true))
	r.Post("/threads/{id}/unread", m.handleRead(false))
	r.Post("/threads/{id}/messages", m.handleReply)
	r.Post("/threads/{id}/messages/{mid}/retry", m.handleRetry)
}

// Hooks mounts POST /inbound-email.
func (m *Module) Hooks(r chi.Router) {
	r.Post("/inbound-email", m.handleInbound)
}

// Shutdown waits for background sends to finish, or for ctx to end.
func (m *Module) Shutdown(ctx context.Context) {
	done := make(chan struct{})
	go func() { m.bg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// background runs fn on its own context (30 s), tracked for Shutdown.
func (m *Module) background(parent context.Context, fn func(ctx context.Context)) {
	m.bg.Add(1)
	go func() {
		defer m.bg.Done()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
		defer cancel()
		fn(ctx)
	}()
}

// replyTo is where a visitor's answer to our mail goes: the thread's reply
// address when inbound mail is set up, else the owner's notify address.
func (m *Module) replyTo(ctx context.Context, token string) string {
	if m.d.InboundDomain != "" {
		return "reply+" + token + "@" + m.d.InboundDomain
	}
	return m.d.Settings.NotifyTo(ctx)
}
