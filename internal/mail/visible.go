package mail

import "strings"

// Visible removes what a reader cannot see: C0/C1 control characters (except
// newline and tab), zero-width spaces, the word joiner, a byte-order mark
// after the first position, soft hyphens and bidi marks, embeddings,
// overrides and isolates. Joiners (U+200C, U+200D), variation selectors and
// tag characters stay: they are part of how some words and emoji are spelt.
func Visible(s string) string {
	if strings.IndexFunc(s, func(r rune) bool { return invisible(r, false) }) < 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		if !invisible(r, i == 0) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func invisible(r rune, first bool) bool {
	switch {
	case r == '\n', r == '\t':
		return false
	case r < 0x20, r == 0x7f, r >= 0x80 && r <= 0x9f:
		return true
	case r == 0xfeff:
		return !first
	case r == 0x200b, r == 0x2060, r == 0x00ad, r == 0x200e, r == 0x200f, r == 0x061c:
		return true
	case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
