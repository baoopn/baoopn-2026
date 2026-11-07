package contact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

const (
	maxInboundBytes = 10 << 20
	// floodLimit is how many emails one thread accepts in 24 hours.
	floodLimit = 50
	// maxAttachments caps the count kept of what was not kept.
	maxAttachments = 999
)

// reject is why an email was not stored, with what is kept of it for debugging.
type reject struct {
	reason, recipient, sender, subject string
}

func (m *Module) handleInbound(w http.ResponseWriter, r *http.Request) {
	if m.d.InboundSecret == "" || m.d.InboundDomain == "" {
		httpapi.Error(w, http.StatusNotFound, "not_found", "no such route")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxInboundBytes+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "message too large")
			return
		}
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "could not read the body")
		return
	}
	envTo := r.Header.Get("X-Envelope-To")
	if !verify(m.d.InboundSecret, r.Header.Get("X-Timestamp"), envTo, r.Header.Get("X-Signature"), raw, m.d.Now()) {
		httpapi.Error(w, http.StatusUnauthorized, "bad_signature", "bad signature")
		return
	}
	rej, err := m.receive(r.Context(), raw, envTo, r.Header.Get("X-Envelope-From"))
	if err == nil && rej != nil {
		err = m.storeReject(r.Context(), *rej)
	}
	if err != nil {
		m.d.Log.Error("contact: inbound", logging.Err(err))
		httpapi.Error(w, http.StatusServiceUnavailable, "unavailable", "try again later")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// receive stores one signed email. It returns a reject for mail that is not
// stored, and an error only when trying again could help.
func (m *Module) receive(ctx context.Context, raw []byte, envTo, envFrom string) (*reject, error) {
	in, perr := bmail.ParseInbound(raw)
	rej := &reject{recipient: redact(envTo), sender: clip(bmail.BareAddress(envFrom), 254)}
	if perr == nil {
		rej.sender = clip(in.From, 254)
		rej.subject = clip(strings.Join(strings.Fields(bmail.Tidy(in.Subject)), " "), 200)
	}
	token := findToken(envTo, in.Recipients, m.d.InboundDomain)
	if token == "" {
		rej.reason = "no_token"
		return rej, nil
	}
	rej.recipient = "reply+…@" + strings.ToLower(m.d.InboundDomain)
	switch {
	case perr != nil, in.From == "":
		rej.reason = "unparseable"
		return rej, nil
	case bmail.Automatic(in.Headers, in.From):
		rej.reason = "automatic"
		return rej, nil
	case in.From == m.d.Mailer.FromAddress():
		rej.reason = "self"
		return rej, nil
	}

	t, found, err := threadWhere(ctx, m.d.DB, `reply_token = $1`, token)
	if err != nil {
		return nil, err
	}
	if !found {
		rej.reason = "unknown_token"
		return rej, nil
	}
	if t.Status == "spam" {
		rej.reason = "spam_thread"
		return rej, nil
	}
	var recent int
	if err := m.d.DB.QueryRow(ctx, `
		SELECT count(*) FROM thread_messages
		WHERE thread_id = $1 AND direction = 'inbound' AND source = 'email' AND created_at > now() - interval '24 hours'`,
		t.ID).Scan(&recent); err != nil {
		return nil, err
	}
	if recent >= floodLimit {
		rej.reason = "flood"
		return rej, nil
	}

	msgID := bmail.MessageID(in.MessageID)
	if msgID == "" {
		sum := sha256.Sum256(raw)
		msgID = "<" + hex.EncodeToString(sum[:])[:32] + "@inbound.invalid>"
	}
	ours, err := m.ourText(ctx, t.ID)
	if err != nil {
		return nil, err
	}
	body, full, ok := bmail.CleanBody(bmail.Received{
		Text: in.Text, HTML: in.HTML, Attachments: in.Attachments,
		Sender: m.d.Mailer.FromName(), Sent: ours,
	})
	if !ok {
		rej.reason = "empty"
		return rej, nil
	}
	stored, err := m.storeInbound(ctx, t, body, full, msgID, in.From, min(max(in.Attachments, 0), maxAttachments))
	if err != nil {
		return nil, err
	}
	if stored {
		m.d.Log.Info("contact: inbound email stored", slog.Int64("thread_id", t.ID))
		m.notifyInbound(ctx, t, body)
	}
	return nil, nil
}

// ourText is what we mailed on a thread, each body also with the current
// signature under it: what a reply may quote back.
func (m *Module) ourText(ctx context.Context, threadID int64) ([]string, error) {
	site, _ := m.d.Settings.Get(ctx)
	rows, err := m.d.DB.Query(ctx,
		`SELECT body FROM thread_messages WHERE thread_id = $1 AND direction = 'outbound'`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
		if site.Signature != "" {
			out = append(out, b+sigSep+site.Signature)
		}
	}
	return out, rows.Err()
}

// storeInbound adds the message; stored is false for a duplicate Message-ID.
func (m *Module) storeInbound(ctx context.Context, t thread, body, full, msgID, from string, attachments int) (bool, error) {
	tx, err := m.d.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO thread_messages (thread_id, direction, source, body, full_body, status, rfc_message_id,
			from_address, other_sender, attachments_dropped)
		VALUES ($1, 'inbound', 'email', $2, $3, 'received', $4, $5, $6, $7)
		ON CONFLICT (rfc_message_id) WHERE rfc_message_id IS NOT NULL DO NOTHING RETURNING id`,
		t.ID, body, full, msgID, from, from != strings.ToLower(t.Email), attachments).Scan(&id)
	if isNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE threads SET last_message_at = now(), last_direction = 'inbound', last_preview = $2,
			message_count = message_count + 1, read_at = NULL,
			status = CASE WHEN status = 'archived' THEN 'open' ELSE status END
		WHERE id = $1`, t.ID, preview(body)); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func (m *Module) storeReject(ctx context.Context, r reject) error {
	m.d.Log.Info("contact: inbound email rejected", slog.String("reason", r.reason))
	if _, err := m.d.DB.Exec(ctx,
		`INSERT INTO inbound_rejects (reason, recipient, sender, subject) VALUES ($1, $2, $3, $4)`,
		r.reason, r.recipient, r.sender, r.subject); err != nil {
		return err
	}
	if rand.N(50) == 0 {
		if _, err := m.d.DB.Exec(ctx, `DELETE FROM inbound_rejects WHERE created_at < now() - interval '90 days'`); err != nil {
			m.d.Log.Warn("contact: prune inbound_rejects", logging.Err(err))
		}
	}
	return nil
}
