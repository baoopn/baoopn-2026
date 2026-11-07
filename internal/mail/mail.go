// Package mail sends outgoing mail over SMTP (useSend's SMTP proxy in
// production, Mailpit locally) and reads inbound MIME messages: parsing,
// reducing a reply to the words a person wrote, and spotting machine mail.
package mail

import (
	"errors"
	"net/mail"
	"strings"
)

// Config is an SMTP server and the sender. Enabled is decided by the caller
// (config.MailEnabled); when false, Send never dials.
type Config struct {
	Enabled  bool
	Host     string
	Port     int
	User     string
	Password string
	// From is the sender, e.g. "Jane Doe <jane@mail.example.com>".
	From string
}

// Message is one email to send. Header values are sanitised when the message
// is built, so user input cannot add headers.
type Message struct {
	To, ReplyTo, Subject, Text, HTML string
	// InReplyTo and References are Message-IDs ("<id@host>"); invalid ones are
	// dropped.
	InReplyTo  string
	References []string
	// AutoSubmitted ("auto-generated", "auto-replied") marks machine mail.
	AutoSubmitted string
}

var (
	// ErrDisabled is returned by Send when mail is not configured.
	ErrDisabled = errors.New("mail: sending is disabled")
	// ErrRefused wraps a permanent failure (a 5xx answer, a bad address):
	// sending the same message again will not help.
	ErrRefused = errors.New("mail: refused")
)

// SMTP sends mail through one server.
type SMTP struct {
	cfg  Config
	from *mail.Address
}

// NewSMTP returns the mailer. An unparseable From leaves FromName and
// FromAddress empty and makes Send fail.
func NewSMTP(c Config) *SMTP {
	s := &SMTP{cfg: c}
	if a, err := mail.ParseAddress(c.From); err == nil {
		s.from = a
	}
	return s
}

// Enabled reports whether Send will try to deliver.
func (s *SMTP) Enabled() bool { return s != nil && s.cfg.Enabled }

// FromName is the sender's display name ("Bao Nguyen"), also when disabled.
func (s *SMTP) FromName() string {
	if s == nil || s.from == nil {
		return ""
	}
	return HeaderText(s.from.Name)
}

// FromAddress is the sender's address in lower case, also when disabled.
func (s *SMTP) FromAddress() string {
	if s == nil || s.from == nil {
		return ""
	}
	return strings.ToLower(s.from.Address)
}
