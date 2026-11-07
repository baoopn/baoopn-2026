package contact

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

var requestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,100}$`)

func mailDisabled(w http.ResponseWriter) {
	httpapi.Error(w, http.StatusServiceUnavailable, "mail_disabled", "outgoing mail is not configured")
}

func (m *Module) handleReply(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		Body      string `json:"body"`
		RequestID string `json:"request_id"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	body := strings.TrimSpace(bmail.Tidy(in.Body))
	if n := utf8.RuneCountInString(body); n < 1 || n > 10000 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", "body must be 1 to 10000 characters")
		return
	}
	if !requestID.MatchString(in.RequestID) {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", "request_id must be 8 to 100 letters, digits, - or _")
		return
	}
	if !m.d.Mailer.Enabled() {
		mailDisabled(w)
		return
	}

	ctx := r.Context()
	tx, err := m.d.DB.Begin(ctx)
	if err != nil {
		m.internal(w, "reply begin", err)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	t, found, err := loadThread(ctx, tx, id)
	if err != nil {
		m.internal(w, "reply load thread", err)
		return
	}
	if !found {
		notFound(w)
		return
	}
	if t.Status == "spam" {
		httpapi.Error(w, http.StatusConflict, "conflict", "cannot reply to a spam thread")
		return
	}
	msg, err := scanMessage(tx.QueryRow(ctx, `
		INSERT INTO thread_messages (thread_id, direction, source, body, status, request_id, from_address)
		VALUES ($1, 'outbound', 'admin', $2, 'sending', $3, $4)
		ON CONFLICT (thread_id, request_id) WHERE request_id IS NOT NULL DO NOTHING
		RETURNING `+messageCols, id, body, in.RequestID, m.d.Mailer.FromAddress()))
	if isNoRows(err) {
		// The same request again: answer with what it made the first time.
		msg, err = scanMessage(tx.QueryRow(ctx,
			`SELECT `+messageCols+` FROM thread_messages WHERE thread_id = $1 AND request_id = $2`, id, in.RequestID))
		if err != nil {
			m.internal(w, "reply replay", err)
			return
		}
		httpapi.JSON(w, http.StatusOK, msg)
		return
	}
	if err != nil {
		m.internal(w, "reply insert", err)
		return
	}
	if _, err := tx.Exec(ctx, `
		UPDATE threads SET last_message_at = now(), last_direction = 'outbound', last_preview = $2,
			message_count = message_count + 1 WHERE id = $1`, id, preview(body)); err != nil {
		m.internal(w, "reply update thread", err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		m.internal(w, "reply commit", err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, m.deliver(ctx, t, msg))
}

func (m *Module) handleRetry(w http.ResponseWriter, r *http.Request) {
	id, ok1 := idParam(r, "id")
	mid, ok2 := idParam(r, "mid")
	if !ok1 || !ok2 {
		notFound(w)
		return
	}
	if !m.d.Mailer.Enabled() {
		mailDisabled(w)
		return
	}
	ctx := r.Context()
	t, found, err := loadThread(ctx, m.d.DB, id)
	if err != nil {
		m.internal(w, "retry load thread", err)
		return
	}
	if !found {
		notFound(w)
		return
	}
	msg, err := scanMessage(m.d.DB.QueryRow(ctx, `
		UPDATE thread_messages SET status = 'sending', error = ''
		WHERE id = $1 AND thread_id = $2 AND direction = 'outbound'
		  AND (status = 'failed' OR (status = 'sending' AND created_at < now() - interval '10 minutes'))
		RETURNING `+messageCols, mid, id))
	if isNoRows(err) {
		var exists bool
		if err := m.d.DB.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM thread_messages WHERE id = $1 AND thread_id = $2)`, mid, id).Scan(&exists); err != nil {
			m.internal(w, "retry lookup", err)
			return
		}
		if !exists {
			httpapi.Error(w, http.StatusNotFound, "not_found", "message not found")
			return
		}
		httpapi.Error(w, http.StatusConflict, "not_retryable", "only a failed message can be sent again")
		return
	}
	if err != nil {
		m.internal(w, "retry update", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, m.deliver(ctx, t, msg))
}

// deliver mails an outbound message on t and records the outcome. It returns
// the message as recorded.
func (m *Module) deliver(parent context.Context, t thread, msg ThreadMessage) ThreadMessage {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 30*time.Second)
	defer cancel()

	site, err := m.d.Settings.Get(ctx)
	if err != nil {
		m.d.Log.Warn("contact: settings unavailable, sending without signature", logging.Err(err))
	}
	text := msg.Body
	if site.Signature != "" {
		text += sigSep + site.Signature
	}
	out := bmail.Message{
		To:      t.Email,
		ReplyTo: m.replyTo(ctx, t.token),
		Subject: replySubject(t.Subject),
		Text:    text,
		HTML:    bmail.TextToHTML(text),
	}
	refs, err := m.threadingIDs(ctx, t.ID)
	if err != nil {
		m.d.Log.Warn("contact: threading ids unavailable", slog.Int64("thread_id", t.ID), logging.Err(err))
	}
	if len(refs) > 0 {
		out.References = refs
		out.InReplyTo = refs[len(refs)-1]
	}

	rfcID, sendErr := m.d.Mailer.Send(ctx, out)
	now := m.d.Now().UTC()
	if sendErr != nil {
		msg.Status, msg.Error, msg.SentAt = "failed", clip(sendErr.Error(), 500), nil
		lvl := slog.LevelWarn
		if errors.Is(sendErr, bmail.ErrDisabled) {
			lvl = slog.LevelInfo
		}
		m.d.Log.Log(ctx, lvl, "contact: reply not sent", slog.Int64("thread_id", t.ID), slog.Int64("message_id", msg.ID), logging.Err(sendErr))
	} else {
		msg.Status, msg.Error, msg.SentAt = "sent", "", &now
	}
	for attempt := range 3 {
		if sendErr != nil {
			_, err = m.d.DB.Exec(ctx, `UPDATE thread_messages SET status = 'failed', error = $2 WHERE id = $1`, msg.ID, msg.Error)
		} else {
			_, err = m.d.DB.Exec(ctx, `UPDATE thread_messages SET status = 'sent', sent_at = $3, rfc_message_id = $2, error = '' WHERE id = $1`,
				msg.ID, nullable(rfcID), now)
		}
		if err == nil {
			break
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
		}
	}
	if err != nil {
		m.d.Log.Error("contact: recording the send outcome failed", slog.Int64("message_id", msg.ID), logging.Err(err))
	}
	return msg
}

// threadingIDs are the Message-IDs of the latest inbound mail on a thread,
// oldest first, at most 10.
func (m *Module) threadingIDs(ctx context.Context, threadID int64) ([]string, error) {
	rows, err := m.d.DB.Query(ctx, `
		SELECT rfc_message_id FROM thread_messages
		WHERE thread_id = $1 AND direction = 'inbound' AND rfc_message_id IS NOT NULL
		ORDER BY created_at DESC, id DESC LIMIT 10`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	slices.Reverse(ids)
	return ids, rows.Err()
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
