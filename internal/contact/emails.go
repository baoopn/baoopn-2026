package contact

import (
	"context"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/baoopn/baoopn-2026/internal/logging"
	bmail "github.com/baoopn/baoopn-2026/internal/mail"
)

// This file builds the four emails the contact module sends: the owner's
// new-thread and new-reply notifications, the visitor's acknowledgement and
// the admin reply. Each is a bmail.Doc rendered as matching text and HTML.

// sigSep starts the signature under a reply.
const sigSep = bmail.SigSep

// sentLayout is how times are shown: "7 Nov 2026, 14:05 UTC".
const sentLayout = "2 Jan 2006, 15:04 UTC"

// preheaderRunes is how much of a message the owner's preheader shows.
const preheaderRunes = 90

func (m *Module) brand() bmail.Brand { return bmail.BrandOf(m.d.PublicURL) }

func footerFor(b bmail.Brand) string {
	return "Sent by the " + b.Name + " contact form."
}

func ref(id int64) string { return "#" + strconv.FormatInt(id, 10) }

// newThreadDoc is the owner's notification of a form message. The recipient
// is fixed (the owner), so the whole message is shown, escaped.
func newThreadDoc(b bmail.Brand, link string, id int64, s submission) bmail.Doc {
	return bmail.Doc{
		Brand:     b,
		Subject:   "New message: " + s.subject,
		Preheader: "From " + s.name + ": " + clipRunes(oneLine(s.message), preheaderRunes),
		Kicker:    "New message",
		Title:     s.subject,
		Blocks: []bmail.Block{
			{Kind: bmail.Fields, Fields: []bmail.Field{
				{Label: "Name", Value: s.name},
				{Label: "Email", Value: s.email, Mono: true},
				{Label: "Subject", Value: s.subject},
				{Label: "Received", Value: s.at.UTC().Format(sentLayout)},
				{Label: "Reference", Value: ref(id), Mono: true},
			}},
			{Kind: bmail.Excerpt, Text: s.message},
			{Kind: bmail.Button, Label: "Open in inbox", URL: link},
		},
		Footer: footerFor(b),
	}
}

// inboundDoc is the owner's notification of a visitor's emailed reply.
func inboundDoc(b bmail.Brand, link string, t thread, body string) bmail.Doc {
	return bmail.Doc{
		Brand:     b,
		Subject:   "New reply: " + t.Subject,
		Preheader: t.Name + " replied: " + clipRunes(oneLine(body), preheaderRunes),
		Kicker:    "New reply",
		Title:     t.Subject,
		Blocks: []bmail.Block{
			{Kind: bmail.Fields, Fields: []bmail.Field{
				{Label: "Name", Value: t.Name},
				{Label: "Email", Value: t.Email, Mono: true},
			}},
			{Kind: bmail.Excerpt, Text: body},
			{Kind: bmail.Button, Label: "Open in inbox", URL: link},
		},
		Footer: footerFor(b),
	}
}

// ackDoc is the visitor's acknowledgement and its Subject header. It goes to
// an unverified address, so it shows only low-risk facts: never the message,
// and the name and subject only when they pass safeName and safeSubject.
func ackDoc(b bmail.Brand, id int64, s submission, template, signature string) (bmail.Doc, string) {
	subject, ok := safeSubject(s.subject)
	header := "Re: your message"
	if ok {
		header = replySubject(subject)
	} else {
		subject = "your message"
	}
	var blocks []bmail.Block
	for _, p := range paragraphs(strings.ReplaceAll(template, "{name}", safeName(s.name))) {
		blocks = append(blocks, bmail.Block{Kind: bmail.Para, Text: p})
	}
	blocks = append(blocks, bmail.Block{Kind: bmail.Fields, Label: "Your message", Fields: []bmail.Field{
		{Label: "Reference", Value: ref(id), Mono: true},
		{Label: "Subject", Value: subject},
		{Label: "Sent", Value: s.at.UTC().Format(sentLayout)},
	}})
	return bmail.Doc{
		Brand:     b,
		Subject:   bmail.HeaderText(header),
		Preheader: "Got your message. Reference " + ref(id) + ".",
		Kicker:    "Message received",
		Blocks:    blocks,
		Signature: signature,
		Footer: "You got this email because this address was entered on the contact form at " + b.Name +
			". If that wasn't you, you can ignore it.",
	}, bmail.HeaderText(header)
}

// replyDoc is an admin reply: a person's letter, so minimal chrome. Quoted
// lines (">") become Quote blocks; the text part is body + sigSep + signature.
func replyDoc(b bmail.Brand, subject, body, signature string) bmail.Doc {
	var blocks []bmail.Block
	for _, l := range strings.Split(body, "\n") {
		kind := bmail.Para
		if strings.HasPrefix(l, ">") {
			kind = bmail.Quote
		}
		if n := len(blocks); n > 0 && blocks[n-1].Kind == kind {
			blocks[n-1].Text += "\n" + l
			continue
		}
		blocks = append(blocks, bmail.Block{Kind: kind, Text: l})
	}
	return bmail.Doc{Brand: b, Subject: subject, Blocks: blocks, Signature: signature, Personal: true}
}

// paragraphs splits text on blank lines.
func paragraphs(text string) []string {
	var out []string
	for _, p := range blankLine.Split(strings.TrimSpace(text), -1) {
		if p = strings.Trim(p, "\n"); strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

// render returns the text and HTML parts of d, or fallback as text (and its
// escaped HTML) if the layout fails.
func (m *Module) render(d bmail.Doc, fallback string) (string, string) {
	text, html, err := d.Render()
	if err != nil {
		m.d.Log.Error("contact: email layout failed, sending plain text", logging.Err(err))
		return fallback, bmail.TextToHTML(fallback)
	}
	return text, html
}

var (
	// domainLike is a label, a dot (ASCII, full-width or ideographic) and two
	// or more letters: "evil.com", "exa mple.com", "exam．com".
	domainLike = regexp.MustCompile(`(?i)[\p{L}\p{N}-][.．。｡]\p{L}{2,}`)
	// blankLine separates paragraphs.
	blankLine = regexp.MustCompile(`\n[ \t]*\n`)
	// longDigits is a phone or account number.
	longDigits = regexp.MustCompile(`\p{Nd}{5,}`)
)

// hidden reports whether s holds a control, format (bidi, zero-width) or
// other invisible rune.
func hidden(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == ' ' || r == ' ' ||
			r == '᠎' || r == 'ㅤ' || r == 'ﾠ' || r == 'ᅟ' || r == 'ᅠ' {
			return true
		}
	}
	return false
}

// linky reports whether s could carry a link, address or contact detail.
func linky(s string) bool {
	l := strings.ToLower(s)
	return strings.ContainsAny(s, "@/\\:<>") || strings.Contains(l, "www") || strings.Contains(l, "http") ||
		domainLike.MatchString(s) || hidden(s)
}

// safeSubject returns the subject for the acknowledgement, or false when it
// could carry relay/phishing content. The checks run on the NFKC form, which
// is also what is shown.
func safeSubject(subject string) (string, bool) {
	s := strings.TrimSpace(norm.NFKC.String(subject))
	if s == "" || utf8.RuneCountInString(s) > 80 || linky(s) || longDigits.MatchString(s) {
		return "", false
	}
	return s, true
}

// safeName returns the visitor's name for the greeting, or "there" when it
// looks like it carries a link or address (relay/phishing content).
func safeName(name string) string {
	n := strings.TrimSpace(norm.NFKC.String(name))
	if n == "" || utf8.RuneCountInString(n) > 40 || linky(n) || strings.Count(n, ".") > 1 || longDigits.MatchString(n) {
		return "there"
	}
	return n
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clipRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n])) + "…"
}

// ackAllowed reports whether addr may get another acknowledgement: at most
// ackPerAddress in 24 hours, by address in lower case without its +tag,
// counting the thread just stored. A failed count refuses (fail closed).
func (m *Module) ackAllowed(ctx context.Context, threadID int64, addr string) bool {
	var n int
	err := m.d.DB.QueryRow(ctx, `
		SELECT count(*) FROM threads
		WHERE lower(regexp_replace(email, '\+[^@]*@', '@')) = $1 AND created_at > now() - interval '24 hours'`,
		normalAddress(addr)).Scan(&n)
	if err != nil {
		m.d.Log.Warn("contact: acknowledgement count failed, skipped", slog.Int64("thread_id", threadID), logging.Err(err))
		return false
	}
	return n <= ackPerAddress
}

// ackPerAddress caps acknowledgements to one address per 24 hours.
const ackPerAddress = 3

// normalAddress is addr in lower case without a +tag: "A+x@b.co" → "a@b.co".
func normalAddress(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	local, domain, ok := strings.Cut(addr, "@")
	if !ok {
		return addr
	}
	if i := strings.IndexByte(local, '+'); i >= 0 {
		local = local[:i]
	}
	return local + "@" + domain
}
