package analytics

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// DeriveSalt derives the visitor-hash salt from SECRETS_KEY. An empty key
// yields a random salt, so unique visitors reset when the process restarts.
func DeriveSalt(secretsKey string) string {
	if secretsKey == "" {
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		return hex.EncodeToString(b)
	}
	m := hmac.New(sha256.New, []byte(secretsKey))
	m.Write([]byte("baoopn:visitor-salt:v1"))
	return hex.EncodeToString(m.Sum(nil))
}

// visitorHash is 24 hex characters, stable within one UTC day.
func (r *Recorder) visitorHash(now time.Time, ip, ua string) string {
	day := now.UTC().Format("20060102")
	sum := sha256.Sum256([]byte(r.salt + "|" + day + "|" + ip + "|" + ua))
	return hex.EncodeToString(sum[:12])
}

func (r *Recorder) markLive(visitor string, now time.Time) {
	r.mu.Lock()
	r.live[visitor] = now
	r.mu.Unlock()
}

// trackVisitor queues the site-wide, per-path and per-hour visitor rows not
// already queued today, and marks the visitor live.
func (r *Recorder) trackVisitor(now time.Time, path, visitor string) {
	now = now.UTC()
	dayKey := now.Format("20060102")
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	hour := now.Truncate(time.Hour)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.live[visitor] = now
	if r.seenDay != dayKey || len(r.seen) >= maxSeen {
		r.seenDay = dayKey
		r.seen = map[string]struct{}{}
	}
	if r.firstSeen("d|*|" + visitor) {
		r.days = append(r.days, dayVisitor{day: day, path: "*", visitor: visitor})
	}
	if r.firstSeen("d|" + path + "|" + visitor) {
		r.days = append(r.days, dayVisitor{day: day, path: path, visitor: visitor})
	}
	if r.firstSeen("h|" + hour.Format("15") + "|" + visitor) {
		r.hours = append(r.hours, hourVisitor{hour: hour, visitor: visitor})
	}
}

// firstSeen records k and reports whether it was new. Callers hold r.mu.
func (r *Recorder) firstSeen(k string) bool {
	if _, ok := r.seen[k]; ok {
		return false
	}
	r.seen[k] = struct{}{}
	return true
}
