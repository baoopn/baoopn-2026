package contact

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// maxSkew is how far a webhook timestamp may be from now.
const maxSkew = 300 * time.Second

// Sign is hex(HMAC-SHA256(secret, ts + "." + envTo + "\n" + body)), as the
// Email Worker computes it. Covering the envelope recipient stops a captured
// request from being replayed to a different thread token.
func Sign(secret, ts, envTo string, body []byte) string {
	return hex.EncodeToString(mac(secret, ts, envTo, body))
}

func mac(secret, ts, envTo string, body []byte) []byte {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(ts + "." + envTo + "\n"))
	m.Write(body)
	return m.Sum(nil)
}

// verify reports whether sig signs (envTo, body) at ts with secret, and ts is
// within five minutes of now. The comparison is constant-time.
func verify(secret, ts, envTo, sig string, body []byte, now time.Time) bool {
	if secret == "" {
		return false
	}
	unix, err := strconv.ParseInt(strings.TrimSpace(ts), 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(unix, 0)); d > maxSkew || d < -maxSkew {
		return false
	}
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil {
		return false
	}
	return hmac.Equal(got, mac(secret, strings.TrimSpace(ts), envTo, body))
}

// replyLocal is the local part of a reply address: reply+<32 hex characters>.
var replyLocal = regexp.MustCompile(`^reply\+([0-9a-f]{32})$`)

// tokenOf returns the reply token in addr when it is a reply address on
// domain ("" otherwise). Case does not matter.
func tokenOf(addr, domain string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	addr = strings.TrimSuffix(strings.TrimPrefix(addr, "<"), ">")
	local, d, ok := strings.Cut(addr, "@")
	if !ok || domain == "" || d != strings.ToLower(domain) {
		return ""
	}
	if m := replyLocal.FindStringSubmatch(local); m != nil {
		return m[1]
	}
	return ""
}

// findToken takes the token from the envelope recipient, or failing that
// from the first header recipient that carries one.
func findToken(envelopeTo string, recipients []string, domain string) string {
	if t := tokenOf(envelopeTo, domain); t != "" {
		return t
	}
	for _, r := range recipients {
		if t := tokenOf(r, domain); t != "" {
			return t
		}
	}
	return ""
}

// redact hides the token in a reply address: reply+…@domain.
func redact(addr string) string {
	addr = strings.ToLower(strings.TrimSpace(addr))
	if local, d, ok := strings.Cut(addr, "@"); ok && strings.HasPrefix(local, "reply+") {
		addr = "reply+…@" + d
	} else if strings.HasPrefix(addr, "reply+") {
		addr = "reply+…"
	}
	return clip(addr, 254)
}
