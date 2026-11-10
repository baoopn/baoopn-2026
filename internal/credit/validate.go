package credit

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Validation messages. Each starts with the field name; the admin app maps
// a 422 to its field by that prefix.
const (
	msgText       = "text must be 1 to 80 characters on one line"
	msgURL        = "url must be an absolute https URL"
	msgLogo       = "logo must be wordmark or mark"
	msgOriginsMax = "origins can hold at most 50 entries"
	msgOriginFmt  = "origins[%d] %q must be an origin like https://example.com (http only for localhost), with no path"
)

// validate checks the input in field order and returns the normalised
// document (without updated_at), or the first problem.
func (in Input) validate() (Doc, string) {
	var d Doc
	var ok bool
	if d.Text, ok = validText(in.Text); !ok {
		return Doc{}, msgText
	}
	if d.URL, ok = validURL(in.URL); !ok {
		return Doc{}, msgURL
	}
	if !validLogo(in.Logo) {
		return Doc{}, msgLogo
	}
	d.Logo = in.Logo
	origins := make([]string, 0, len(in.Origins))
	for i, raw := range in.Origins {
		o, ok := normalizeOrigin(raw)
		if !ok {
			return Doc{}, fmt.Sprintf(msgOriginFmt, i, truncate(raw, 60))
		}
		origins = append(origins, o)
	}
	d.Origins = dedupe(origins)
	if len(d.Origins) > OriginsMax {
		return Doc{}, msgOriginsMax
	}
	return d, ""
}

// validText trims s and checks it is 1 to TextMax characters with no
// control characters (so no line breaks).
func validText(s string) (string, bool) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < 1 || n > TextMax || !utf8.ValidString(s) {
		return "", false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return s, true
}

// validURL trims s and checks it is an absolute https URL with a host and
// no credentials.
func validURL(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > URLMax {
		return "", false
	}
	p, err := url.Parse(s)
	if err != nil || p.Scheme != "https" || p.Host == "" || p.Hostname() == "" || p.User != nil {
		return "", false
	}
	return p.String(), true
}

func validLogo(s string) bool { return s == LogoWordmark || s == LogoMark }

// localHosts may use plain http.
var localHosts = map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}

// normalizeOrigin checks raw is a bare origin and returns it as
// scheme://host[:port], lowercase, without a default port.
func normalizeOrigin(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > originMax || strings.ContainsAny(s, "*?#") {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return "", false // an IDN host must be given as punycode
		}
	}
	p, err := url.Parse(s)
	if err != nil || p.Opaque != "" || p.User != nil || p.Hostname() == "" ||
		(p.Path != "" && p.Path != "/") || p.RawQuery != "" || p.Fragment != "" {
		return "", false
	}
	scheme := strings.ToLower(p.Scheme)
	host := strings.ToLower(p.Hostname())
	switch {
	case scheme == "https":
	case scheme == "http" && localHosts[host]:
	default:
		return "", false
	}
	if port := p.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", false
		}
	}
	return scheme + "://" + hostPort(scheme, p), true
}

// hostPort is p's lowercase host with the scheme's default port dropped.
func hostPort(scheme string, p *url.URL) string {
	host := strings.ToLower(p.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := p.Port()
	if port == "" || (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		return host
	}
	return host + ":" + port
}

// dedupe drops repeats and keeps the first-seen order. Never nil.
func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// truncate cuts s to at most n characters.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
