package auth

import (
	"crypto/sha256"
	"regexp"
	"sync"
	"time"
)

// Limits on refused API keys, so a client cannot use the API as a free
// oracle against Clerk or spend Clerk's rate limit for us.
const (
	// rejectTTL is how long a refused key is remembered and refused without
	// asking Clerk again.
	rejectTTL = 60 * time.Second
	// rejectMax bounds the refused keys remembered; when full, expired
	// entries go first, then the whole table is cleared.
	rejectMax = 1024
	// keyFailuresPerMinute is how many refused keys one client may send
	// before it gets 429 for new keys. A cached valid key is never limited.
	keyFailuresPerMinute = 10
	// maxKeyFailClients bounds the clients keyFailLimiter tracks; past it
	// the table is cleared, which only forgets failures.
	maxKeyFailClients = 4096
)

// apiKeyShape is what a Clerk API key secret looks like ("ak_" and an
// opaque token). Anything else is refused without asking Clerk.
var apiKeyShape = regexp.MustCompile(`^ak_[A-Za-z0-9_-]{16,256}$`)

// keyBucket is an in-memory token bucket: up to size takes, refilled at size
// per window. It does not survive a restart, and need not.
type keyBucket struct {
	mu     sync.Mutex
	size   float64
	window time.Duration
	tokens float64
	last   time.Time
}

func newKeyBucket(size int, window time.Duration) *keyBucket {
	return &keyBucket{size: float64(size), window: window, tokens: float64(size)}
}

// refill adds the tokens earned since the last call. b.mu must be held.
func (b *keyBucket) refill(now time.Time) {
	if !b.last.IsZero() {
		if d := now.Sub(b.last); d > 0 {
			b.tokens = min(b.size, b.tokens+b.size*float64(d)/float64(b.window))
		}
	}
	b.last = now
}

// take uses one token at now, or reports false when none is left.
func (b *keyBucket) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill(now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// give returns one token, up to size.
func (b *keyBucket) give() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.size, b.tokens+1)
}

// keyFailLimiter counts refused API keys per client. Every uncached key
// reserves a failure first; only refusals keep it.
type keyFailLimiter struct {
	mu      sync.Mutex
	perMin  int
	clients map[string]*keyBucket
}

func newKeyFailLimiter(perMinute int) *keyFailLimiter {
	return &keyFailLimiter{perMin: perMinute, clients: map[string]*keyBucket{}}
}

// reserve takes one of client's failures at now, atomically, before the
// outcome is known. It reports false when none is left. refund gives the
// failure back, for an outcome that must not count; calling it more than
// once gives back only one.
func (f *keyFailLimiter) reserve(client string, now time.Time) (refund func(), ok bool) {
	f.mu.Lock()
	b := f.clients[client]
	if b == nil {
		if len(f.clients) >= maxKeyFailClients {
			clear(f.clients)
		}
		b = newKeyBucket(f.perMin, time.Minute)
		f.clients[client] = b
	}
	f.mu.Unlock()
	if !b.take(now) {
		return func() {}, false
	}
	var once sync.Once
	return func() { once.Do(b.give) }, true
}

// keyRefused reports whether secret was refused less than rejectTTL ago.
func (a *Authenticator) keyRefused(secret string) bool {
	h := sha256.Sum256([]byte(secret))
	a.mu.Lock()
	defer a.mu.Unlock()
	at, hit := a.rejectedKeys[h]
	if !hit {
		return false
	}
	if !a.now().Before(at.Add(rejectTTL)) {
		delete(a.rejectedKeys, h)
		return false
	}
	return true
}

// storeRefused remembers that Clerk refused secret, under its SHA-256.
func (a *Authenticator) storeRefused(secret string) {
	h := sha256.Sum256([]byte(secret))
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if len(a.rejectedKeys) >= rejectMax {
		for k, at := range a.rejectedKeys {
			if !now.Before(at.Add(rejectTTL)) {
				delete(a.rejectedKeys, k)
			}
		}
		if len(a.rejectedKeys) >= rejectMax {
			clear(a.rejectedKeys)
		}
	}
	a.rejectedKeys[h] = now
}
