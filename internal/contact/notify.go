package contact

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/baoopn-2026/internal/logging"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

// notifyEvery is the least time between two inbound notifications for one
// thread.
const notifyEvery = 10 * time.Minute

func (m *Module) adminLink(id int64) string {
	return strings.TrimRight(m.d.PublicURL, "/") + "/admin/inbox/" + strconv.FormatInt(id, 10)
}

// notifyNewThread tells the owner a form message arrived.
func (m *Module) notifyNewThread(ctx context.Context, id int64, s submission) {
	text := "New message from the contact form.\n\n" +
		"Name: " + s.name + "\n" +
		"Email: " + s.email + "\n" +
		"Subject: " + s.subject + "\n\n" +
		s.message + "\n\n" +
		"Open it in the admin: " + m.adminLink(id) + "\n"
	text, html := m.render(newThreadDoc(m.brand(), m.adminLink(id), id, s), text)
	m.send(ctx, "notification", id, bmail.Message{
		To:            m.d.Settings.NotifyTo(ctx),
		Subject:       bmail.HeaderText("New message: " + s.subject),
		Text:          text,
		HTML:          html,
		AutoSubmitted: "auto-generated",
	})
}

// acknowledge sends the visitor a fixed confirmation they can answer to
// continue the thread. It is not stored on the thread.
//
// Anyone can type any address into the form, so this is a potential relay:
// it is only sent when Turnstile is configured (checked by the caller), never
// quotes the visitor's message or subject, only uses a name that passes
// safeName, and is capped by reserveAck.
func (m *Module) acknowledge(ctx context.Context, id int64, token string, s submission, template, signature string) {
	if !m.reserveAck(ctx, id, s.email) {
		return
	}
	doc, subject := ackDoc(m.brand(), m.d.Mailer.FromName(), id, s, template, signature)
	fallback := strings.ReplaceAll(template, "{name}", safeName(s.name))
	if signature != "" {
		fallback += sigSep + signature
	}
	text, html := m.render(doc, fallback)
	m.send(ctx, "acknowledgement", id, bmail.Message{
		To:            s.email,
		ReplyTo:       m.replyTo(ctx, token),
		Subject:       subject,
		Text:          text,
		HTML:          html,
		AutoSubmitted: "auto-replied",
	})
}

const (
	// ackPerHour caps visitor acknowledgements across the whole site.
	ackPerHour = 30
	// ackPerAddress caps acknowledgements sent to one address (by
	// normalAddress) in 24 hours.
	ackPerAddress = 3
	// ackLock is the advisory lock key that serialises reserveAck across
	// API instances.
	ackLock int64 = 0x62616f61636b // "baoack"
)

// reserveAck records an acknowledgement to addr for thread id and reports
// whether it may be sent: fewer than ackPerHour sent site-wide in the last
// hour, and fewer than ackPerAddress to this address in 24 hours. Both count
// acknowledgements sent (contact_acks), not threads created, and are checked
// under one lock so parallel requests cannot pass a cap together. Any
// database error refuses (fail closed).
func (m *Module) reserveAck(ctx context.Context, id int64, addr string) bool {
	key := normalAddress(addr)
	ok, why, err := func() (bool, string, error) {
		tx, err := m.d.DB.Begin(ctx)
		if err != nil {
			return false, "", err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, ackLock); err != nil {
			return false, "", err
		}
		var hour, mine int
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE sent_at > now() - interval '1 hour'),
				count(*) FILTER (WHERE address_key = $1)
			FROM contact_acks WHERE sent_at > now() - interval '24 hours'`, key).Scan(&hour, &mine); err != nil {
			return false, "", err
		}
		if hour >= ackPerHour {
			return false, "site", nil
		}
		if mine >= ackPerAddress {
			return false, "address", nil
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO contact_acks (thread_id, address_key) VALUES ($1, $2)`, id, key); err != nil {
			return false, "", err
		}
		return true, "", tx.Commit(ctx)
	}()
	switch {
	case err != nil:
		m.d.Log.Warn("contact: acknowledgement count failed, skipped", slog.Int64("thread_id", id), logging.Err(err))
		return false
	case why == "site":
		m.d.Log.Warn("contact: acknowledgement cap reached, skipped", slog.Int64("thread_id", id))
		return false
	case why == "address":
		m.d.Log.Info("contact: acknowledgement cap for this address reached, skipped", slog.Int64("thread_id", id))
		return false
	}
	if rand.N(50) == 0 {
		if _, err := m.d.DB.Exec(ctx, `DELETE FROM contact_acks WHERE sent_at < now() - interval '30 days'`); err != nil {
			m.d.Log.Warn("contact: prune contact_acks", logging.Err(err))
		}
	}
	return ok
}

// gmailDomains are Gmail's own domains, where dots in the local part do not
// matter and googlemail.com is gmail.com.
var gmailDomains = map[string]bool{"gmail.com": true, "googlemail.com": true}

// normalAddress is the key one mailbox is counted under: lower case, without
// a +tag, and for Gmail without dots on gmail.com:
// "A.B+x@GoogleMail.com" → "ab@gmail.com".
func normalAddress(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return addr
	}
	if i := strings.IndexByte(local, '+'); i >= 0 {
		local = local[:i]
	}
	if gmailDomains[domain] {
		local = strings.ReplaceAll(local, ".", "")
		domain = "gmail.com"
	}
	return local + "@" + domain
}

// notifyInbound tells the owner a visitor answered by email, at most once per
// thread every notifyEvery.
func (m *Module) notifyInbound(parent context.Context, t thread, body string) {
	if !m.d.Mailer.Enabled() {
		return
	}
	now := m.d.Now()
	m.mu.Lock()
	if last, ok := m.notified[t.ID]; ok && now.Sub(last) < notifyEvery {
		m.mu.Unlock()
		return
	}
	m.notified[t.ID] = now
	for id, at := range m.notified {
		if now.Sub(at) >= notifyEvery {
			delete(m.notified, id)
		}
	}
	m.mu.Unlock()
	m.background(parent, func(ctx context.Context) {
		text := t.Name + " <" + t.Email + "> replied:\n\n" + body + "\n\nOpen it in the admin: " + m.adminLink(t.ID) + "\n"
		text, html := m.render(inboundDoc(m.brand(), m.adminLink(t.ID), t, body), text)
		m.send(ctx, "notification", t.ID, bmail.Message{
			To:            m.d.Settings.NotifyTo(ctx),
			Subject:       bmail.HeaderText("New reply: " + t.Subject),
			Text:          text,
			HTML:          html,
			AutoSubmitted: "auto-generated",
		})
	})
}

// send delivers msg and logs a failure, never with an address or body.
func (m *Module) send(ctx context.Context, what string, threadID int64, msg bmail.Message) {
	if strings.TrimSpace(msg.To) == "" {
		// No NOTIFY_EMAIL / notify_email setting: nothing to notify.
		m.d.Log.Info("contact: "+what+" skipped, no recipient configured", slog.Int64("thread_id", threadID))
		return
	}
	if _, err := m.d.Mailer.Send(ctx, msg); err != nil {
		lvl := slog.LevelWarn
		if errors.Is(err, bmail.ErrDisabled) {
			lvl = slog.LevelInfo
		}
		m.d.Log.Log(ctx, lvl, "contact: "+what+" not sent", slog.Int64("thread_id", threadID), logging.Err(err))
	}
}

// replySubject is "Re: subject" unless it already starts with "re:".
func replySubject(subject string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		return subject
	}
	return "Re: " + subject
}
