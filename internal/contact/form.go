package contact

import (
	"context"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

// ContactInput is the form as the browser posts it.
type ContactInput struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	Subject        string `json:"subject"`
	Message        string `json:"message"`
	Website        string `json:"website"`
	StartedAt      int64  `json:"started_at"`
	TurnstileToken string `json:"turnstile_token,omitempty"`
}

// submission is a validated form.
type submission struct {
	name, email, subject, message string
	// at is when the form was received.
	at time.Time
}

const minFillTime = 3 * time.Second

// validate checks the form at now and returns the cleaned fields, or a
// message naming what is wrong.
func validate(in ContactInput, now time.Time) (submission, string) {
	started := time.UnixMilli(in.StartedAt)
	switch {
	case in.StartedAt <= 0, started.After(now.Add(time.Minute)), now.Sub(started) > 24*time.Hour:
		return submission{}, "The form expired, please reload the page"
	case now.Sub(started) < minFillTime:
		return submission{}, "Please wait a moment before sending"
	}

	var s submission
	name := strings.TrimSpace(in.Name)
	if n := utf8.RuneCountInString(name); n < 1 || n > 100 {
		return s, "name must be 1 to 100 characters"
	}
	if s.name = bmail.HeaderText(name); s.name == "" {
		return s, "name must be 1 to 100 characters"
	}

	a, err := mail.ParseAddress(strings.TrimSpace(in.Email))
	if err != nil {
		return s, "email must be a valid email address"
	}
	s.email = strings.ToLower(a.Address)
	_, domain, _ := strings.Cut(s.email, "@")
	if len(s.email) > 254 || !strings.Contains(domain, ".") {
		return s, "email must be a valid email address"
	}

	s.subject = strings.Join(strings.Fields(bmail.Tidy(in.Subject)), " ")
	if utf8.RuneCountInString(s.subject) > 150 {
		return s, "subject must be at most 150 characters"
	}
	if s.subject == "" {
		s.subject = "Message from " + s.name
	}

	s.message = strings.TrimSpace(bmail.Tidy(in.Message))
	if n := utf8.RuneCountInString(s.message); n < 10 || n > 5000 {
		return s, "message must be 10 to 5000 characters"
	}
	return s, ""
}

func (m *Module) handleContact(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	site, err := m.d.Settings.Get(ctx)
	if err != nil {
		m.d.Log.Error("contact: settings", logging.Err(err))
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	if !site.ContactEnabled {
		httpapi.Error(w, http.StatusServiceUnavailable, "contact_disabled", "the contact form is closed")
		return
	}
	var in ContactInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if in.Website != "" {
		// A bot filled the hidden field: pretend it worked.
		httpapi.JSON(w, http.StatusCreated, map[string]bool{"ok": true})
		return
	}
	now := m.d.Now()
	sub, problem := validate(in, now)
	sub.at = now
	if problem != "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", problem)
		return
	}
	if m.d.TurnstileSecret != "" {
		if strings.TrimSpace(in.TurnstileToken) == "" {
			httpapi.Error(w, http.StatusUnprocessableEntity, "captcha_failed", "captcha check failed")
			return
		}
		ok, err := m.verifyTurnstile(ctx, in.TurnstileToken, httpapi.ClientIP(r))
		if err != nil {
			m.d.Log.Warn("contact: turnstile unavailable", logging.Err(err))
			httpapi.Error(w, http.StatusServiceUnavailable, "captcha_unavailable", "captcha check unavailable, try again")
			return
		}
		if !ok {
			httpapi.Error(w, http.StatusUnprocessableEntity, "captcha_failed", "captcha check failed")
			return
		}
	}

	token := newToken()
	id, err := m.storeSubmission(ctx, sub, token)
	if err != nil {
		m.d.Log.Error("contact: store", logging.Err(err))
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	httpapi.JSON(w, http.StatusCreated, map[string]bool{"ok": true})

	if !m.d.Mailer.Enabled() {
		m.d.Log.Info("contact: mail disabled, notification skipped", slog.Int64("thread_id", id))
		return
	}
	m.background(ctx, func(ctx context.Context) {
		m.notifyNewThread(ctx, id, sub)
		// The acknowledgement goes to an unverified address: only with a captcha.
		if site.AutoReplyEnabled && m.d.TurnstileSecret != "" {
			m.acknowledge(ctx, id, token, sub, site.AutoReplyText, site.Signature)
		}
	})
}

func (m *Module) storeSubmission(ctx context.Context, s submission, token string) (int64, error) {
	tx, err := m.d.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO threads (reply_token, name, email, subject, last_preview, message_count, last_direction)
		VALUES ($1, $2, $3, $4, $5, 1, 'inbound') RETURNING id`,
		token, s.name, s.email, s.subject, preview(s.message)).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO thread_messages (thread_id, direction, source, body, status, from_address)
		VALUES ($1, 'inbound', 'form', $2, 'received', $3)`, id, s.message, s.email); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}
