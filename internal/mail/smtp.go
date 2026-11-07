package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// sendTimeout is the most one send may take.
const sendTimeout = 30 * time.Second

// Send delivers m and returns the Message-ID it gave it. A disabled mailer
// returns ErrDisabled without dialing. Errors carry the SMTP code at most,
// never the server's words (which can name the recipient); a permanent
// failure wraps ErrRefused.
func (s *SMTP) Send(ctx context.Context, m Message) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	l, err := s.letterOf(m)
	if err != nil {
		return "", err
	}
	body, err := build(l)
	if err != nil {
		return "", fmt.Errorf("mail: build: %w", err)
	}
	if err := s.deliver(ctx, l, body); err != nil {
		return "", outcome(err)
	}
	return l.messageID, nil
}

func (s *SMTP) deliver(ctx context.Context, l letter, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	c, err := s.connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if s.cfg.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.cfg.User, s.cfg.Password, s.cfg.Host)); err != nil {
			return err
		}
	}
	if err := c.Mail(l.from.Address); err != nil {
		return err
	}
	if err := c.Rcpt(l.to.Address); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	// The server has taken the message (the end of DATA was answered 250).
	// Saying goodbye is a courtesy: a server that hangs up first, or answers
	// QUIT with an error, has not unsent anything.
	_ = c.Quit()
	return nil
}

// connect opens an encrypted session with the server: TLS from the start on
// port 465, STARTTLS otherwise. Only a mail catcher on this machine may be
// spoken to in the clear.
func (s *SMTP) connect(ctx context.Context) (*smtp.Client, error) {
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	tlsConfig := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
	implicit := s.cfg.Port == 465
	var conn net.Conn
	var err error
	if implicit {
		conn, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if implicit {
		return c, nil
	}
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(tlsConfig); err != nil {
			_ = c.Close()
			return nil, err
		}
		return c, nil
	}
	if !localHost(s.cfg.Host) {
		_ = c.Close()
		return nil, errNoTLS
	}
	return c, nil
}

var errNoTLS = errors.New("smtp: the server does not offer STARTTLS; refusing to send in the clear")

// localHost reports whether mail to host stays on this machine or its compose
// network (Mailpit in development).
func localHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1", "mailpit":
		return true
	}
	return false
}

// outcome reduces an SMTP failure to its code and to whether it was a refusal.
func outcome(err error) error {
	var perr *textproto.Error
	if errors.As(err, &perr) {
		if perr.Code >= 500 && perr.Code < 600 {
			return fmt.Errorf("%w: smtp %d", ErrRefused, perr.Code)
		}
		return fmt.Errorf("smtp %d", perr.Code)
	}
	if errors.Is(err, errNoTLS) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("smtp: %w", context.DeadlineExceeded)
	}
	return errors.New("smtp: the session did not complete")
}
