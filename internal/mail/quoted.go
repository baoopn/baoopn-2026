package mail

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Taking the earlier messages out of a reply. A visitor's words lost are far worse than
// a quotation kept, so a line is removed only when it is recognisably the mail app's
// and not theirs; when in doubt it stays. The cutter can still be wrong, which is why
// the whole email is stored beside what it leaves (full_body) whenever it took
// anything out.
//
// Marked history (lines that start with ">"):
//
//   - An attribution line ("On <date>, <name> <address> wrote:", also in French,
//     Spanish and German) introduces it. From then on every ">" line is removed,
//     wherever it stands: the visitor's own lines under them (a reply written below) or
//     between them (answers written in between) are kept.
//   - A line is an attribution only when all of this holds: it starts and ends like
//     one; it names an email address or our own sender name (a date is not enough: "On
//     the phone on 10/05/2026 you wrote:" is a sentence); and the next line with
//     anything on it is a ">" line. One at the end of the text, or with unmarked text
//     under it, stays.
//   - A mail app may wrap an attribution over up to four lines. It never reaches up
//     into the writer's own lines: when a later line in that window starts like an
//     attribution too, the earlier one is not one, and the first line of a wrapped one
//     must itself carry a date, an address or our name.
//   - Apple Mail quotes its attribution too ("> On … wrote:"): that introduces the
//     ">" lines around it in the same way.
//   - ">" lines that no attribution introduced are the writer's own and are kept,
//     however many there are.
//
// Unmarked history (Outlook puts the earlier message under a header block, with no
// marks on it, so the visitor may have typed inside it):
//
//   - It starts at "-----Original Message-----", a forwarded-message line, or a block of
//     three or more From: / Sent: / To: / Subject: lines in a row, one of them the
//     date, whose From: line itself carries an email address or our own sender name.
//   - From there to the end is removed only when every line under the header block that
//     has anything on it is a line of something we sent on this thread (compared with
//     the white space evened out). One line that is not ours, an answer typed between
//     ours for instance, and everything from the header block on is kept.

var (
	originalMessage = regexp.MustCompile(`(?i)^-{2,}\s*(?:original|forwarded) message\s*-{2,}$|^begin forwarded message:$`)
	underscores     = regexp.MustCompile(`^_{5,}$`)

	// A header line as Outlook writes it above a quoted message, in English, French,
	// Spanish or German; sometimes between asterisks (bold, as text).
	headerLine = regexp.MustCompile(`(?i)^\*{0,2}(from|sent|to|cc|subject|date|de|envoyé|à|objet|enviado(?: el)?|para|asunto|fecha|von|gesendet|an|betreff)\s?:`)
	headerFrom = regexp.MustCompile(`(?i)^\*{0,2}(from|de|von)\s?:`)
	headerDate = regexp.MustCompile(`(?i)^\*{0,2}(sent|date|envoyé|enviado(?: el)?|fecha|gesendet)\s?:`)

	// How an attribution line starts and ends, in the same four languages.
	attributionStart = regexp.MustCompile(`^(On|Le|El|Am) `)
	attributionEnd   = regexp.MustCompile(`(?i)(wrote|a écrit|escribió|schrieb)\s?:$|\sschrieb\s.{1,200}:$`)

	addressShape = regexp.MustCompile(`[^\s<>()@]+@[^\s<>()@]+\.[^\s<>()@]+`)
	// A year, a time of day or a numeric date.
	dateShape = regexp.MustCompile(`\b(?:19|20)\d{2}\b|\b\d{1,2}:\d{2}\b|\b\d{1,2}[/.-]\d{1,2}[/.-]\d{2,4}\b`)
	// The marks a mail app puts before a quoted line.
	quoteMarks = regexp.MustCompile(`^(?:>\s*)+`)
)

const (
	// attributionLines is how many lines a wrapped attribution may run over.
	attributionLines = 4
	// headerBlock is the fewest header lines in a row that make an Outlook block.
	headerBlock = 3
)

// sentLines is the lines of what we sent on a thread, white space evened out: what an
// unmarked quotation has to consist of before it is taken for one.
type sentLines map[string]bool

// linesOf collects the lines of the texts we mailed.
func linesOf(texts []string) sentLines {
	out := sentLines{}
	for _, text := range texts {
		for line := range strings.SplitSeq(Tidy(text), "\n") {
			if l := evened(line); l != "" {
				out[l] = true
			}
		}
	}
	return out
}

// evened is a line without the quote marks before it, its white space evened out.
func evened(line string) string {
	line = quoteMarks.ReplaceAllString(strings.TrimSpace(line), "")
	return strings.Join(strings.Fields(line), " ")
}

// maxCutBytes is how much of a received text the cutter reads; what lies beyond is
// dropped first, at a line end. A reply is stored capped at maxInboundRunes (8,000
// characters, at most 32 KB of UTF-8) and the whole email at maxFullRunes, so nothing
// past the first few tens of kilobytes can reach either; 256 KiB is eight times that,
// room for a long reply with answers written between the lines of a long quoted
// thread. The cutter is linear anyway (see cutter), so the cap only bounds the work an
// email can ask for (the fetched email may be 2 MB). The whole email (full_body)
// is taken from the text before this cap, so it is not affected.
const maxCutBytes = 256 << 10

// cutQuoted removes the earlier messages a mail app puts in a reply, by the rule above.
// sender is our display name as our own mail carries it ("" if unknown); ours is what
// we sent on the thread.
func cutQuoted(s, sender string, ours sentLines) string {
	return newCutter(strings.Split(head(s, maxCutBytes), "\n"), sender, ours).cut()
}

// head is s cut to at most limit bytes: at the last line end before it, or, within one
// line longer than that, at a character boundary.
func head(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	if i := strings.LastIndexByte(s[:limit], '\n'); i >= 0 {
		return s[:i]
	}
	for limit > 0 && !utf8.RuneStart(s[limit]) {
		limit--
	}
	return s[:limit]
}

// cutter is a text's lines with what the rules ask about the lines under each one
// worked out once, from the end up. Asking it again at every line (how many header
// lines follow, is everything under them ours) read a run of n header-shaped lines n
// times over: a 2 MB email of "From:" lines took most of an hour.
type cutter struct {
	lines  []string
	sender string
	// Each has one entry more than lines, for "the end".
	// filled[i] is the first line from i on with anything on it (len(lines): none).
	filled []int
	// foreign[i] is the first line from i on with anything on it that we did not send.
	foreign []int
	// header[i] is how many header lines run in a row from line i, and dated[i] whether
	// one of them is the date.
	header []int
	dated  []bool
}

func newCutter(lines []string, sender string, ours sentLines) *cutter {
	n := len(lines)
	c := &cutter{
		lines: lines, sender: sender,
		filled: make([]int, n+1), foreign: make([]int, n+1),
		header: make([]int, n+1), dated: make([]bool, n+1),
	}
	c.filled[n], c.foreign[n] = n, n
	for i := n - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		c.filled[i], c.foreign[i] = c.filled[i+1], c.foreign[i+1]
		if t != "" {
			c.filled[i] = i
		}
		if l := evened(t); l != "" && !ours[l] {
			c.foreign[i] = i
		}
		if headerLine.MatchString(t) {
			c.header[i] = c.header[i+1] + 1
			c.dated[i] = c.dated[i+1] || headerDate.MatchString(t)
		}
	}
	return c
}

// cut is the text without the history the rule above recognises.
func (c *cutter) cut() string {
	lines := c.lines
	kept := make([]string, 0, len(lines))
	introduced, tail := false, false
	for i := 0; i < len(lines); {
		if c.outlookHistory(i) {
			tail = true
			break
		}
		if n := c.attribution(i); n > 0 {
			introduced = true
			i += n
			continue
		}
		if introduced && quoted(lines[i]) {
			i++
			continue
		}
		kept = append(kept, strings.TrimRight(lines[i], " "))
		i++
	}
	// A divider left hanging above the history that was cut.
	for tail && len(kept) > 0 {
		last := strings.TrimSpace(kept[len(kept)-1])
		if last != "" && !underscores.MatchString(last) {
			break
		}
		kept = kept[:len(kept)-1]
	}
	return strings.Join(kept, "\n")
}

// quoted reports a line a mail app marked as quoted: ">" first, but not ">=".
func quoted(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, ">") && !strings.HasPrefix(t, ">=")
}

// attribution returns how many lines the attribution starting at line i runs over, 0
// when line i does not start one. Mail apps wrap a long one over a few lines, and some
// quote it with the message it introduces.
func (c *cutter) attribution(i int) int {
	lines, sender := c.lines, c.sender
	marked := quoted(lines[i])
	// words is line j as the attribution's: "" when it cannot be part of it (blank, or
	// quoted where the first line is not, or the other way round).
	words := func(j int) string {
		t := strings.TrimSpace(lines[j])
		if quoted(t) != marked {
			return ""
		}
		return strings.TrimSpace(quoteMarks.ReplaceAllString(t, ""))
	}
	first := words(i)
	if !attributionStart.MatchString(first) {
		return 0
	}
	whole := ""
	for j := i; j < len(lines) && j < i+attributionLines; j++ {
		t := words(j)
		// A later line that starts one: line i is the writer's own, above it.
		if t == "" || (j > i && attributionStart.MatchString(t)) {
			return 0
		}
		whole += " " + t
		if !attributionEnd.MatchString(t) {
			continue
		}
		if j > i && !evidence(first, sender) && !dateShape.MatchString(first) {
			return 0
		}
		if !evidence(whole, sender) || !c.quotedNext(j+1) {
			return 0
		}
		return j + 1 - i
	}
	return 0
}

// quotedNext reports whether the next line from i on with anything on it is quoted.
func (c *cutter) quotedNext(i int) bool {
	next := c.filled[i]
	return next < len(c.lines) && quoted(c.lines[next])
}

// outlookHistory reports whether line i starts an earlier message quoted without marks
// (a divider, or a block of header lines) that is all ours from there to the end.
func (c *cutter) outlookHistory(i int) bool {
	t := strings.TrimSpace(c.lines[i])
	var under int // the first line under the header block
	switch {
	case originalMessage.MatchString(t):
		under = c.filled[i+1]
		under += c.header[under]
	case headerFrom.MatchString(t) && evidence(t, c.sender):
		if c.header[i] < headerBlock || !c.dated[i] {
			return false
		}
		under = i + c.header[i]
	default:
		return false
	}
	return c.foreign[under] == len(c.lines)
}

// evidence reports whether text a mail app would have written names who wrote the
// earlier message: an email address, or our display name (every email we send carries
// it in its sender).
func evidence(text, sender string) bool {
	if addressShape.MatchString(text) {
		return true
	}
	return sender != "" && strings.Contains(strings.ToLower(text), strings.ToLower(sender))
}
