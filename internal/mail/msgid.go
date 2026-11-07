package mail

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxNameRunes clips user text that goes into a header or display name.
	maxNameRunes = 100
	// maxReferences is how many earlier Message-IDs a reply names.
	maxReferences = 10
	// maxMessageID is the longest Message-ID kept or sent.
	maxMessageID = 250
)

// HeaderText makes user-written text safe to sit in an email header or a
// display name: one line, no control or invisible characters, none of the
// characters that end a quoted name or open an address, at most 100 runes.
func HeaderText(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == 0x2028, r == 0x2029:
			return ' '
		case r == '"', r == '\\', r == '<', r == '>':
			return -1
		}
		return r
	}, Visible(strings.ToValidUTF8(s, "")))
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > maxNameRunes {
		s = strings.TrimSpace(string([]rune(s)[:maxNameRunes]))
	}
	return s
}

// MessageID returns a Message-ID header value in the form <id>, or "" when raw
// is not one that is safe to send back out in a header: printable ASCII, no
// spaces, no extra brackets, a sensible length. Received ids come from
// strangers.
func MessageID(raw string) string {
	id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(raw), "<"), ">")
	if id == "" || len(id) > maxMessageID {
		return ""
	}
	for _, r := range id {
		if r <= ' ' || r > '~' || r == '<' || r == '>' {
			return ""
		}
	}
	return "<" + id + ">"
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func domainOf(addr string) string {
	if _, d, ok := strings.Cut(addr, "@"); ok && d != "" {
		return d
	}
	return "localhost"
}
