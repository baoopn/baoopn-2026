package contact

import (
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

func (m *Module) brand() bmail.Brand {
	b := bmail.BrandOf(m.d.PublicURL)
	if m.d.EmailLogo != nil {
		b.LogoURL, b.LogoWidth, b.LogoHeight = m.d.EmailLogo()
	}
	return b
}

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
// an unverified address, so it shows only fixed, low-risk facts: never the
// message or its subject, and the name only when it passes safeName. owner is
// the site owner's name (the mail sender name), or the brand when empty.
func ackDoc(b bmail.Brand, owner string, id int64, s submission, template, signature string) (bmail.Doc, string) {
	if owner = strings.TrimSpace(owner); owner == "" {
		owner = b.Name
	}
	header := bmail.HeaderText("Re: your message to " + owner)
	var blocks []bmail.Block
	for _, p := range paragraphs(strings.ReplaceAll(template, "{name}", safeName(s.name))) {
		blocks = append(blocks, bmail.Block{Kind: bmail.Para, Text: p})
	}
	blocks = append(blocks, bmail.Block{Kind: bmail.Fields, Label: "Your message", Fields: []bmail.Field{
		{Label: "Reference", Value: ref(id), Mono: true},
		{Label: "Sent", Value: s.at.UTC().Format(sentLayout)},
	}})
	return bmail.Doc{
		Brand:     b,
		Subject:   header,
		Preheader: "Got your message. Reference " + ref(id) + ".",
		Kicker:    "Message received",
		Blocks:    blocks,
		Signature: signature,
		Footer: "You got this email because this address was entered on the contact form at " + b.Name +
			". If that wasn't you, you can ignore it.",
	}, header
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
	return bmail.Doc{Brand: b, Subject: subject, Preheader: replyPreheader(subject, blocks), Blocks: blocks,
		Signature: signature, Personal: true}
}

// replyPreheader is the inbox snippet of a reply: its own (unquoted) text on
// one line, or the subject when there is none. Without it, mail apps show the
// badge text as the snippet.
func replyPreheader(subject string, blocks []bmail.Block) string {
	var parts []string
	for _, b := range blocks {
		if b.Kind == bmail.Para {
			parts = append(parts, b.Text)
		}
	}
	if p := oneLine(strings.Join(parts, " ")); p != "" {
		return clipRunes(p, preheaderRunes)
	}
	return clipRunes(oneLine(subject), preheaderRunes)
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
	// digitRun is digits with separators between them, as in a phone number.
	digitRun = regexp.MustCompile(`\p{Nd}[\p{Nd}\s().\-–—_/·]{3,}\p{Nd}`)
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

// safeName returns the visitor's name for the greeting, or "there" when it
// looks like it carries a link, address or phone number (relay/phishing
// content). The checks run on the NFKC form, which is also what is shown.
func safeName(name string) string {
	n := strings.TrimSpace(norm.NFKC.String(name))
	if n == "" || utf8.RuneCountInString(n) > 40 || linky(n) || strings.Count(n, ".") > 1 || phoneLike(n) {
		return "there"
	}
	return n
}

// phoneLike reports a number in s: three or more digits in all, or a run of
// five or more digits broken only by separators ("1 800 555 0199").
func phoneLike(s string) bool {
	if digits(s) >= 3 {
		return true
	}
	for _, run := range digitRun.FindAllString(s, -1) {
		if digits(run) >= 5 {
			return true
		}
	}
	return false
}

func digits(s string) int {
	n := 0
	for _, r := range s {
		if unicode.Is(unicode.Nd, r) {
			n++
		}
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
