package sitesettings

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

// Public mounts nothing.
func (s *Store) Public(chi.Router) {}

// Hooks mounts nothing.
func (s *Store) Hooks(chi.Router) {}

// Admin mounts GET/PUT /settings and POST /settings/test-email.
func (s *Store) Admin(r chi.Router) {
	r.Get("/settings", s.handleGet)
	r.Put("/settings", s.handlePut)
	r.Post("/settings/test-email", s.handleTestEmail)
}

func (s *Store) handleGet(w http.ResponseWriter, r *http.Request) {
	v, err := s.Get(r.Context())
	if err != nil {
		s.d.Log.Error("sitesettings: get", logging.Err(err))
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	httpapi.JSON(w, http.StatusOK, v)
}

func (s *Store) handlePut(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if !httpapi.Decode(w, r, &in) {
		return
	}
	v, problem := normalise(in)
	if problem != "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", problem)
		return
	}
	if err := s.put(r.Context(), v); err != nil {
		s.d.Log.Error("sitesettings: put", logging.Err(err))
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	httpapi.JSON(w, http.StatusOK, v)
}

func (s *Store) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	if s.d.Mailer == nil || !s.d.Mailer.Enabled() {
		httpapi.Error(w, http.StatusServiceUnavailable, "mail_disabled", "outgoing mail is not configured")
		return
	}
	to := s.NotifyTo(r.Context())
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 30*time.Second)
	defer cancel()
	_, err := s.d.Mailer.Send(ctx, bmail.Message{
		To:            to,
		Subject:       "Test email from baoopn.com",
		Text:          "This is a test email from the baoopn.com admin. If you can read it, outgoing mail works.",
		AutoSubmitted: "auto-generated",
	})
	if errors.Is(err, bmail.ErrDisabled) {
		httpapi.Error(w, http.StatusServiceUnavailable, "mail_disabled", "outgoing mail is not configured")
		return
	}
	if err != nil {
		// The mailer's errors carry an SMTP code at most, never the server's words.
		s.d.Log.Warn("sitesettings: test email failed", logging.Err(err))
		httpapi.Error(w, http.StatusBadGateway, "mail_failed", "sending failed: "+err.Error())
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]string{"sent_to": to})
}
