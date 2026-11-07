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

// signedHeaders checks the signature headers before the body is read: ts is
// unix seconds within five minutes of now, and sig is the hex of one
// HMAC-SHA256. It returns the trimmed ts and the decoded sig.
func signedHeaders(ts, sig string, now time.Time) (string, []byte, bool) {
	ts = strings.TrimSpace(ts)
	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return "", nil, false
	}
	if d := now.Sub(time.Unix(unix, 0)); d > maxSkew || d < -maxSkew {
		return "", nil, false
	}
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(got) != sha256.Size {
		return "", nil, false
	}
	return ts, got, true
}

// verify reports whether sig signs (envTo, body) at ts with secret, and the
// headers pass signedHeaders. The comparison is constant-time.
func verify(secret, ts, envTo, sig string, body []byte, now time.Time) bool {
	if secret == "" {
		return false
	}
	ts, got, ok := signedHeaders(ts, sig, now)
	if !ok {
		return false
	}
	return hmac.Equal(got, mac(secret, ts, envTo, body))
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
