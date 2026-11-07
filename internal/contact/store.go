package contact

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// ThreadSummary is a thread as the inbox lists it. The reply token is never
// part of it.
type ThreadSummary struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Subject       string    `json:"subject"`
	Status        string    `json:"status"`
	Unread        bool      `json:"unread"`
	LastMessageAt time.Time `json:"last_message_at"`
	LastDirection string    `json:"last_direction"`
	LastPreview   string    `json:"last_preview"`
	MessageCount  int       `json:"message_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// ThreadMessage is one message on a thread.
type ThreadMessage struct {
	ID                 int64      `json:"id"`
	Direction          string     `json:"direction"`
	Source             string     `json:"source"`
	Body               string     `json:"body"`
	FullBody           string     `json:"full_body"`
	Status             string     `json:"status"`
	Error              string     `json:"error"`
	FromAddress        string     `json:"from_address"`
	OtherSender        bool       `json:"other_sender"`
	AttachmentsDropped int        `json:"attachments_dropped"`
	CreatedAt          time.Time  `json:"created_at"`
	SentAt             *time.Time `json:"sent_at"`
}

// ThreadDetail is a thread with its messages.
type ThreadDetail struct {
	Thread       ThreadSummary   `json:"thread"`
	Messages     []ThreadMessage `json:"messages"`
	ReplyEnabled bool            `json:"reply_enabled"`
}

// thread is a summary plus the reply token, for internal use only.
type thread struct {
	ThreadSummary
	token string
}

const summaryCols = `id, name, email, subject, status, read_at IS NULL, last_message_at, last_direction, last_preview, message_count, created_at`

const messageCols = `id, direction, source, body, full_body, status, error, from_address, other_sender, attachments_dropped, created_at, sent_at`

func scanSummary(row pgx.Row) (ThreadSummary, error) {
	var t ThreadSummary
	err := row.Scan(&t.ID, &t.Name, &t.Email, &t.Subject, &t.Status, &t.Unread, &t.LastMessageAt,
		&t.LastDirection, &t.LastPreview, &t.MessageCount, &t.CreatedAt)
	t.LastMessageAt, t.CreatedAt = t.LastMessageAt.UTC(), t.CreatedAt.UTC()
	return t, err
}

func scanMessage(row pgx.Row) (ThreadMessage, error) {
	var m ThreadMessage
	err := row.Scan(&m.ID, &m.Direction, &m.Source, &m.Body, &m.FullBody, &m.Status, &m.Error,
		&m.FromAddress, &m.OtherSender, &m.AttachmentsDropped, &m.CreatedAt, &m.SentAt)
	m.CreatedAt = m.CreatedAt.UTC()
	if m.SentAt != nil {
		u := m.SentAt.UTC()
		m.SentAt = &u
	}
	return m, err
}

// querier is a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// loadThread finds a thread by id; ok is false when there is none.
func loadThread(ctx context.Context, q querier, id int64) (thread, bool, error) {
	return threadWhere(ctx, q, `id = $1`, id)
}

func threadWhere(ctx context.Context, q querier, where string, arg any) (thread, bool, error) {
	var t thread
	err := q.QueryRow(ctx, `SELECT `+summaryCols+`, reply_token FROM threads WHERE `+where, arg).Scan(
		&t.ID, &t.Name, &t.Email, &t.Subject, &t.Status, &t.Unread, &t.LastMessageAt,
		&t.LastDirection, &t.LastPreview, &t.MessageCount, &t.CreatedAt, &t.token)
	if errors.Is(err, pgx.ErrNoRows) {
		return thread{}, false, nil
	}
	t.LastMessageAt, t.CreatedAt = t.LastMessageAt.UTC(), t.CreatedAt.UTC()
	return t, err == nil, err
}

// newToken is a reply token: 32 hex characters.
func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// preview is the first 140 characters of body, white space collapsed.
func preview(body string) string {
	s := strings.Join(strings.Fields(body), " ")
	if utf8.RuneCountInString(s) > 140 {
		s = strings.TrimSpace(string([]rune(s)[:140]))
	}
	return s
}

// likePattern is q for ILIKE, its wildcards escaped (used with ESCAPE '\').
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// clip cuts s to at most n runes.
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) > n {
		return string([]rune(s)[:n])
	}
	return s
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
