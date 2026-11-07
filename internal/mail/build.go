package mail

import (
	"bytes"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"regexp"
	"strings"
	"time"
)

// letter is one message as it is built: who it is from and to, and what it says.
type letter struct {
	from, to *mail.Address
	// replyTo, when set, is where an answer goes instead of from.
	replyTo             *mail.Address
	subject, text, html string
	// messageID, inReplyTo and references are sanitised Message-IDs with their
	// angle brackets.
	messageID  string
	inReplyTo  string
	references []string
	// autoSubmitted marks mail a machine wrote; "" for a person's mail.
	autoSubmitted string
	date          time.Time
}

// autoValue is what an Auto-Submitted value may look like (RFC 3834 keywords).
var autoValue = regexp.MustCompile(`^[A-Za-z0-9-]{1,40}$`)

// letterOf turns m into a letter from the sender. Addresses are reduced to the
// bare address and ids are sanitised, so nothing a caller passes can add a
// header.
func (s *SMTP) letterOf(m Message) (letter, error) {
	if s.from == nil {
		return letter{}, fmt.Errorf("%w: the sender is not an address", ErrRefused)
	}
	to, err := mail.ParseAddress(m.To)
	if err != nil {
		return letter{}, fmt.Errorf("%w: the recipient is not an address", ErrRefused)
	}
	l := letter{
		from:    &mail.Address{Name: s.FromName(), Address: s.from.Address},
		to:      &mail.Address{Address: to.Address},
		subject: m.Subject,
		text:    m.Text,
		html:    m.HTML,
		date:    time.Now(),
	}
	if l.html == "" {
		l.html = TextToHTML(m.Text)
	}
	if strings.TrimSpace(m.ReplyTo) != "" {
		rt, err := mail.ParseAddress(m.ReplyTo)
		if err != nil {
			return letter{}, fmt.Errorf("%w: the reply address is not an address", ErrRefused)
		}
		l.replyTo = &mail.Address{Address: rt.Address}
	}
	// Ids from received mail are a stranger's: only well-formed ones become headers.
	l.inReplyTo = MessageID(m.InReplyTo)
	for _, ref := range m.References {
		if id := MessageID(ref); id != "" {
			l.references = append(l.references, id)
		}
	}
	if n := len(l.references); n > maxReferences {
		l.references = l.references[n-maxReferences:]
	}
	if autoValue.MatchString(m.AutoSubmitted) {
		l.autoSubmitted = strings.ToLower(m.AutoSubmitted)
	}
	l.messageID = fmt.Sprintf("<%s@%s>", randomID(), domainOf(l.from.Address))
	return l, nil
}

// build renders a multipart/alternative message (plain text and HTML, each
// quoted-printable). Header values are encoded, so user input cannot add
// headers.
func build(l letter) ([]byte, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	header("From", l.from.String())
	header("To", l.to.String())
	if l.replyTo != nil {
		header("Reply-To", l.replyTo.String())
	}
	header("Subject", mime.QEncoding.Encode("utf-8", oneLine(l.subject)))
	header("Date", l.date.UTC().Format(time.RFC1123Z))
	header("Message-ID", l.messageID)
	if l.inReplyTo != "" {
		header("In-Reply-To", l.inReplyTo)
	}
	if len(l.references) > 0 {
		// One id to a line: folded, so a long thread stays inside the line limit.
		header("References", strings.Join(l.references, "\r\n "))
	}
	if l.autoSubmitted != "" {
		header("Auto-Submitted", l.autoSubmitted)
		header("X-Auto-Response-Suppress", "All")
	}
	header("MIME-Version", "1.0")
	header("Content-Type", `multipart/alternative; boundary="`+mw.Boundary()+`"`)
	buf.WriteString("\r\n")

	for _, part := range []struct{ ctype, body string }{
		{"text/plain; charset=utf-8", l.text},
		{"text/html; charset=utf-8", l.html},
	} {
		w, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {part.ctype},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		if err != nil {
			return nil, err
		}
		qp := quotedprintable.NewWriter(w)
		if _, err := qp.Write([]byte(part.body)); err != nil {
			return nil, err
		}
		if err := qp.Close(); err != nil {
			return nil, err
		}
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// TextToHTML is text as an HTML email: every character escaped, line breaks
// kept. Nothing anyone typed is ever sent as markup.
func TextToHTML(text string) string {
	lines := strings.ReplaceAll(html.EscapeString(text), "\n", "<br>\n")
	return `<!doctype html>
<html><body style="margin:0;padding:24px;font-family:Helvetica,Arial,sans-serif;font-size:15px;line-height:1.6;color:#1f2328">
<div>` + lines + `</div>
</body></html>
`
}
