package contact

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/baoopn/baoopn-2026/internal/logging"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

// notifyEvery is the least time between two inbound notifications for one
// thread.
const notifyEvery = 10 * time.Minute

// sigSep starts the signature under a reply.
const sigSep = "\n\n-- \n"

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
	m.send(ctx, "notification", id, bmail.Message{
		To:            m.d.Settings.NotifyTo(ctx),
		Subject:       bmail.HeaderText("New message: " + s.subject),
		Text:          text,
		AutoSubmitted: "auto-generated",
	})
}

// acknowledge sends the visitor a fixed confirmation they can answer to
// continue the thread. It is not stored on the thread.
//
// Anyone can type any address into the form, so this is a potential relay:
// it is only sent when Turnstile is configured (checked by the caller), never
// quotes the visitor's message, only uses a link-free name, and is capped
// site-wide by ackPerHour.
func (m *Module) acknowledge(ctx context.Context, id int64, token string, s submission, template, signature string) {
	if !m.allowAck(time.Now()) {
		m.d.Log.Warn("contact: acknowledgement cap reached, skipped", slog.Int64("thread_id", id))
		return
	}
	text := strings.ReplaceAll(template, "{name}", safeName(s.name))
	if signature != "" {
		text += sigSep + signature
	}
	m.send(ctx, "acknowledgement", id, bmail.Message{
		To:            s.email,
		ReplyTo:       m.replyTo(ctx, token),
		Subject:       replySubject(s.subject),
		Text:          text,
		AutoSubmitted: "auto-replied",
	})
}

// ackPerHour caps visitor acknowledgements across the whole site.
const ackPerHour = 30

func (m *Module) allowAck(now time.Time) bool {
	m.ackMu.Lock()
	defer m.ackMu.Unlock()
	cut := now.Add(-time.Hour)
	kept := m.acks[:0]
	for _, t := range m.acks {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	m.acks = kept
	if len(m.acks) >= ackPerHour {
		return false
	}
	m.acks = append(m.acks, now)
	return true
}

// safeName returns the visitor's name for the greeting, or "there" when it
// looks like it carries a link or address (relay/phishing content).
func safeName(name string) string {
	n := strings.TrimSpace(name)
	l := strings.ToLower(n)
	if n == "" || len([]rune(n)) > 40 || strings.ContainsAny(n, "@/<>:") ||
		strings.Contains(l, "www.") || strings.Contains(l, "http") || strings.Count(n, ".") > 1 {
		return "there"
	}
	return n
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
		m.send(ctx, "notification", t.ID, bmail.Message{
			To:            m.d.Settings.NotifyTo(ctx),
			Subject:       bmail.HeaderText("New reply: " + t.Subject),
			Text:          text,
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
