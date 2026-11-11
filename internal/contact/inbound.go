package contact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

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

	// inboundReads is how many request bodies are read at once. Past it the
	// hook answers 503 without reading, and the worker tries again. A slot is
	// only taken by a request whose size and signature headers pass, and is
	// held for at most inboundReadTimeout (then 408, which the worker retries).
	inboundReads       = 8
	inboundReadTimeout = 20 * time.Second
	// badSignaturesPerMinute is how many failed signature checks one client
	// may make; past it a failed check answers 429 instead of 401. Only
	// failures count and only failures are answered 429: a call that passes
	// the signature check never consults the limiter.
	badSignaturesPerMinute = 30
	// inboundPerMinute caps signed deliveries site-wide (not per IP: every
	// call comes from Cloudflare). Past it the hook answers 503.
	inboundPerMinute = 300
	// junkRejectsPerMinute caps the no_token and unknown_token rows written.
	// Past it the row is skipped and the hook still answers 204, so junk is
	// never retried.
	junkRejectsPerMinute = 30
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
	now := m.d.Now()
	client := httpapi.ClientBucket(r)
	// Cheap checks before a read slot is taken or the body read: the size the
	// client announces, and signature headers that could be valid now.
	if r.ContentLength > maxInboundBytes {
		httpapi.Error(w, http.StatusRequestEntityTooLarge, "too_large", "message too large")
		return
	}
	ts, sig := r.Header.Get("X-Timestamp"), r.Header.Get("X-Signature")
	if _, _, ok := signedHeaders(ts, sig, now); !ok {
		m.badSignature(w, client, now)
		return
	}
	raw, status := m.readInbound(w, r)
	switch status {
	case 0:
	case http.StatusServiceUnavailable:
		httpapi.Error(w, status, "busy", "busy, try again later")
		return
	case http.StatusRequestEntityTooLarge:
		httpapi.Error(w, status, "too_large", "message too large")
		return
	case http.StatusRequestTimeout:
		httpapi.Error(w, status, "timeout", "the body took too long")
		return
	default:
		// An unclassified read error (statusReadFailed) is retryable: the
		// hook never answers 400, which the worker would drop.
		httpapi.Error(w, http.StatusServiceUnavailable, "read_failed", "could not read the body, try again later")
		return
	}
	envTo := r.Header.Get("X-Envelope-To")
	if !verify(m.d.InboundSecret, ts, envTo, sig, raw, m.d.Now()) {
		m.badSignature(w, client, now)
		return
	}
	if !m.inboundRate.take(m.d.Now()) {
		w.Header().Set("Retry-After", "60")
		httpapi.Error(w, http.StatusServiceUnavailable, "rate_limited", "too many emails, try again later")
		return
	}
	// The worker delivers again after a 503, so everything below must be safe
	// to repeat. The message insert is: a known Message-ID is stored once, and
	// a duplicate neither notifies nor counts. Only mail without a Message-ID
	// could be stored twice, when a retry arrives with different bytes (new
	// Received or ARC headers) and so a different sha256 id; that is rare and
	// accepted. A repeated reject only adds a second debug row.
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

// badSignature answers a failed signature check: it counts the failure for
// client, then answers 429 once the client is over badSignaturesPerMinute,
// else 401.
func (m *Module) badSignature(w http.ResponseWriter, client string, now time.Time) {
	if !m.failures.fail(client, now) {
		httpapi.Error(w, http.StatusTooManyRequests, "rate_limited", "too many bad signatures")
		return
	}
	httpapi.Error(w, http.StatusUnauthorized, "bad_signature", "bad signature")
}

// receive stores one signed email. It returns a reject for mail that is not
// stored, and an error only when trying again could help.
//
// The token comes from the signed envelope recipient only, never from the
// message headers, and the message is parsed only once the token names a
// live thread under its flood limit: junk sent to random addresses costs a
// lookup, not a MIME parse.
func (m *Module) receive(ctx context.Context, raw []byte, envTo, envFrom string) (*reject, error) {
	rej := &reject{recipient: redact(envTo), sender: clip(bmail.BareAddress(envFrom), 254)}
	token := tokenOf(envTo, m.d.InboundDomain)
	if token == "" {
		rej.reason = "no_token"
		return rej, nil
	}
	rej.recipient = "reply+…@" + strings.ToLower(m.d.InboundDomain)

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

	in, perr := m.parse(raw)
	if perr == nil {
		rej.sender = clip(in.From, 254)
		rej.subject = clip(strings.Join(strings.Fields(bmail.Tidy(in.Subject)), " "), 200)
	}
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
	msgRow, stored, err := m.storeInbound(ctx, t, body, full, msgID, in.From, min(max(in.Attachments, 0), maxAttachments))
	if err != nil {
		return nil, err
	}
	if stored {
		m.events.publish(Event{Type: evMessageCreated, ThreadID: t.ID, MessageID: msgRow, Direction: "inbound"})
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

// storeInbound adds the message and returns its id; stored is false for a
// duplicate Message-ID.
func (m *Module) storeInbound(ctx context.Context, t thread, body, full, msgID, from string, attachments int) (int64, bool, error) {
	tx, err := m.d.DB.Begin(ctx)
	if err != nil {
		return 0, false, err
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
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE threads SET last_message_at = now(), last_direction = 'inbound', last_preview = $2,
			message_count = message_count + 1, read_at = NULL,
			status = CASE WHEN status = 'archived' THEN 'open' ELSE status END
		WHERE id = $1`, t.ID, preview(body)); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// storeReject records r. Rows for mail to no live thread (no_token,
// unknown_token) are capped site-wide by junkRejectsPerMinute; past the cap
// the row is skipped, a sampled count is logged, and nil is returned.
func (m *Module) storeReject(ctx context.Context, r reject) error {
	if (r.reason == "no_token" || r.reason == "unknown_token") && !m.junkRate.take(m.d.Now()) {
		if n := m.junkSkipped.Add(1); n == 1 || n%100 == 0 {
			m.d.Log.Warn("contact: inbound rejects over the cap, not recorded", slog.Int64("skipped", n))
		}
		return nil
	}
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
