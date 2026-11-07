package mail

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/jhillyerd/enmime/v2"
)

// maxRecipients bounds how many header recipients are read from one email.
const maxRecipients = 50

// Bounds on how much work one email can cause. A message with more than
// maxParts MIME parts fails to parse; longer bodies are cut.
const (
	maxParts     = 100
	maxHTMLBytes = 2 << 20
	maxTextBytes = 1 << 20
)

// Inbound is a received email, reduced to what the inbox needs.
type Inbound struct {
	MessageID string
	// From is the bare sender address in lower case; FromName its display name.
	From, FromName string
	Subject        string
	// Text is the text/plain part, "" when there is none (it is never made from
	// the HTML: CleanBody has its own HTML path). HTML is the text/html part.
	Text, HTML  string
	Attachments int
	// Recipients are the bare addresses in To, Cc and Delivered-To, in that
	// order, at most 50.
	Recipients []string
	// Headers holds the first value of every header, by lower-case name.
	Headers map[string]string
}

var parser = enmime.NewParser(enmime.DisableTextConversion(true), enmime.MaxMIMEParts(maxParts))

// ParseInbound parses a raw RFC 822 message.
func ParseInbound(raw []byte) (Inbound, error) {
	env, err := parser.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return Inbound{}, err
	}
	in := Inbound{
		MessageID: env.GetHeader("Message-ID"),
		Subject:   env.GetHeader("Subject"),
		Text:      clipUTF8(env.Text, maxTextBytes),
		HTML:      clipUTF8(env.HTML, maxHTMLBytes),
		Headers:   map[string]string{},
	}
	for _, k := range env.GetHeaderKeys() {
		name := strings.ToLower(k)
		if _, ok := in.Headers[name]; !ok {
			in.Headers[name] = env.GetHeader(k)
		}
	}
	if list, err := env.AddressList("From"); err == nil && len(list) > 0 {
		in.From = strings.ToLower(list[0].Address)
		in.FromName = HeaderText(list[0].Name)
	} else {
		in.From = BareAddress(env.GetHeader("From"))
	}
	for _, name := range []string{"To", "Cc"} {
		list, err := env.AddressList(name)
		if err != nil {
			continue
		}
		for _, a := range list {
			if len(in.Recipients) < maxRecipients {
				in.Recipients = append(in.Recipients, strings.ToLower(a.Address))
			}
		}
	}
	for _, v := range env.GetHeaderValues("Delivered-To") {
		if a := BareAddress(v); a != "" && len(in.Recipients) < maxRecipients {
			in.Recipients = append(in.Recipients, a)
		}
	}
	in.Attachments = len(env.Attachments) + len(env.OtherParts)
	for _, p := range env.Inlines {
		if !strings.HasPrefix(p.ContentType, "text/") {
			in.Attachments++
		}
	}
	return in, nil
}

// clipUTF8 cuts s to at most n bytes without splitting a UTF-8 sequence.
func clipUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
