package mail

import (
	"html"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

// What a received email is reduced to before it is stored. Nothing from an
// email is ever kept as HTML: the result is text, and is shown as text.

const (
	// maxInboundRunes caps a received message, in characters.
	maxInboundRunes = 8000
	// maxFullRunes caps the whole email kept beside a message that had history cut.
	maxFullRunes = 20000
	// noText stands in for an email that had attachments and no words.
	noText = "(no text)"
	// maxAddress is the longest address field read from an email.
	maxAddress = 1000
)

var (
	// Blocks whose content is never text a person wrote. Removed whole, before tags.
	htmlBlocks = regexp.MustCompile(`(?is)<!--.*?-->|<style\b.*?</style\s*>|<script\b.*?</script\s*>|<head\b.*?</head\s*>`)
	// A block left open runs to the end.
	htmlOpenBlock = regexp.MustCompile(`(?is)<(?:style|script)\b.*$`)
	// Tags that end a line of text.
	htmlBreaks = regexp.MustCompile(`(?i)<br\s*/?>|</(?:p|div|tr|li|h[1-6]|blockquote)\s*>`)
	// What looks like a tag: "<" and then a letter, "/", "!" or "?". A "<" before
	// anything else ("<3", "a < b") is the writer's own.
	htmlTag = regexp.MustCompile(`(?s)<[A-Za-z/!?][^>]*>`)
	// Where a quotation starts and ends in HTML.
	blockquoteTag = regexp.MustCompile(`(?i)<(/?)blockquote\b[^>]*>`)

	blankRuns = regexp.MustCompile(`\n{3,}`)
	spaceRuns = regexp.MustCompile(`[ \t\x{00a0}]{2,}`)

	// lineEnds makes every line end "\n" and every space an ordinary one (no-break
	// U+00A0, narrow no-break U+202F; line and paragraph separators U+2028, U+2029).
	lineEnds = strings.NewReplacer("\r\n", "\n", "\r", "\n",
		string(rune(0x00a0)), " ", string(rune(0x202f)), " ",
		string(rune(0x2028)), "\n", string(rune(0x2029)), "\n")
)

// Received is an email's words and what is needed to tell them from the
// history under them.
type Received struct {
	Text, HTML  string
	Attachments int
	// Sender is our display name, which our own mail carries in its From.
	Sender string
	// Sent is the text of what we mailed on this thread: an unmarked quotation
	// is only taken for one when it consists of these lines.
	Sent []string
}

// CleanBody turns a received email into the text that is stored: the text
// part, or the HTML part reduced to text when there is none; without the
// quoted history under it (see cutQuoted); trimmed and capped. full is the
// whole text, cleaned and with nothing cut, when it differs from body, and ""
// when nothing was left out: the cutter can be wrong, and what it removed must
// not be lost. When taking the history out leaves nothing, the text is kept as
// it came. ok is false when the email had no text at all and no attachments
// either, so there is nothing to add.
func CleanBody(in Received) (body, full string, ok bool) {
	ours := linesOf(in.Sent)
	var whole, reply string
	if strings.TrimSpace(in.Text) != "" {
		whole = Tidy(in.Text)
		reply = cutQuoted(whole, in.Sender, ours)
	} else {
		whole = Tidy(htmlToText(in.HTML))
		reply = withoutQuoteBlocks(in.HTML, whole, in.Sender, ours)
	}
	body = squeeze(reply, maxInboundRunes)
	if body == "" {
		body = squeeze(whole, maxInboundRunes)
	}
	if body == "" {
		if in.Attachments > 0 {
			return noText, "", true
		}
		return "", "", false
	}
	if full = squeeze(whole, maxFullRunes); full == body {
		full = ""
	}
	return body, full, true
}

// squeeze trims, closes up runs of blank lines and caps the length at limit
// characters.
func squeeze(s string, limit int) string {
	s = strings.TrimSpace(blankRuns.ReplaceAllString(s, "\n\n"))
	if utf8.RuneCountInString(s) > limit {
		s = strings.TrimSpace(string([]rune(s)[:limit]))
	}
	return s
}

// quoteMark stands on a line of its own where an HTML quotation was.
const quoteMark = ">"

// withoutQuoteBlocks is an HTML-only email's reply without its history. HTML
// marks a quotation as a <blockquote>: each is reduced to one quoted line, so
// the cutter treats it as it treats ">" lines in text (the attribution above
// it goes with it), and those lines are then dropped whether or not anything
// introduced them. Only when words remain outside the blocks: a reply written
// inside the quotation keeps everything (whole is that text).
func withoutQuoteBlocks(htmlPart, whole, sender string, ours sentLines) string {
	marked := Tidy(htmlToText(markBlockquotes(htmlPart)))
	words := func(s string) []string {
		var out []string
		for line := range strings.SplitSeq(s, "\n") {
			if strings.TrimSpace(line) != quoteMark {
				out = append(out, line)
			}
		}
		return out
	}
	if strings.TrimSpace(strings.Join(words(marked), "")) == "" {
		return cutQuoted(whole, sender, ours)
	}
	return strings.Join(words(cutQuoted(marked, sender, ours)), "\n")
}

// markBlockquotes replaces every <blockquote> block, nested ones with the block
// they are in, by a line holding quoteMark. A block left open runs to the end.
func markBlockquotes(s string) string {
	const mark = "<br>&gt;<br>"
	var b strings.Builder
	depth, at := 0, 0
	for _, m := range blockquoteTag.FindAllStringSubmatchIndex(s, -1) {
		closing := m[2] != m[3]
		if depth == 0 {
			b.WriteString(s[at:m[0]])
		}
		switch {
		case !closing:
			depth++
		case depth > 0:
			if depth--; depth == 0 {
				b.WriteString(mark)
			}
		}
		at = m[1]
	}
	if depth == 0 {
		b.WriteString(s[at:])
	} else {
		b.WriteString(mark)
	}
	return b.String()
}

// htmlToText removes what is not text, then the tags, and only then decodes
// entities, once: an encoded tag (&lt;script&gt;) therefore comes out as the
// characters a person would read, not as a tag that the earlier steps would
// have had to catch.
func htmlToText(s string) string {
	s = htmlBlocks.ReplaceAllString(s, " ")
	s = htmlOpenBlock.ReplaceAllString(s, " ")
	s = htmlBreaks.ReplaceAllString(s, "\n")
	s = htmlTag.ReplaceAllString(s, "")
	// Markup's indentation and the gaps tags leave are not the writer's spacing.
	return spaceRuns.ReplaceAllString(html.UnescapeString(s), " ")
}

// Tidy makes text storable and plain: valid UTF-8, Unix line ends, ordinary
// spaces, and nothing a reader cannot see: no control characters (Postgres
// refuses NUL), no zero-width characters, no bidi overrides.
func Tidy(s string) string {
	return Visible(lineEnds.Replace(strings.ToValidUTF8(s, "")))
}

// BareAddress is the address alone, in lower case, from a field that may also
// carry a name ("Priya <p@example.com>"); "" when there is none to be read.
func BareAddress(field string) string {
	field = strings.TrimSpace(field)
	if field == "" || len(field) > maxAddress {
		return ""
	}
	if a, err := mail.ParseAddress(field); err == nil {
		return strings.ToLower(a.Address)
	}
	if strings.Count(field, "@") == 1 && !strings.ContainsAny(field, " \t\r\n<>\"") {
		return strings.ToLower(field)
	}
	return ""
}
