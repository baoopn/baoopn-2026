package contact

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// bucket is an in-memory token bucket: up to size takes, refilled at size
// per window. It does not survive a restart, and need not.
type bucket struct {
	mu     sync.Mutex
	size   float64
	window time.Duration
	tokens float64
	last   time.Time
}

func newBucket(size int, window time.Duration) *bucket {
	return &bucket{size: float64(size), window: window, tokens: float64(size)}
}

// refill adds the tokens earned since the last call. b.mu must be held.
func (b *bucket) refill(now time.Time) {
	if !b.last.IsZero() {
		if d := now.Sub(b.last); d > 0 {
			b.tokens = min(b.size, b.tokens+b.size*float64(d)/float64(b.window))
		}
	}
	b.last = now
}

// take uses one token at now, or reports false when none is left.
func (b *bucket) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill(now)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// empty reports whether no token is left at now, without using one.
func (b *bucket) empty(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill(now)
	return b.tokens < 1
}

// maxFailClients bounds the clients failLimiter tracks; past it the table is
// cleared, which only forgets failures.
const maxFailClients = 4096

// failLimiter counts failed signature checks per client. Only failures are
// counted, so signed Email Worker calls are never limited by it.
type failLimiter struct {
	mu      sync.Mutex
	perMin  int
	clients map[string]*bucket
}

func newFailLimiter(perMinute int) *failLimiter {
	return &failLimiter{perMin: perMinute, clients: map[string]*bucket{}}
}

// blocked reports whether client used up its failures.
func (f *failLimiter) blocked(client string, now time.Time) bool {
	f.mu.Lock()
	b := f.clients[client]
	f.mu.Unlock()
	return b != nil && b.empty(now)
}

// fail counts one failure for client.
func (f *failLimiter) fail(client string, now time.Time) {
	f.mu.Lock()
	b := f.clients[client]
	if b == nil {
		if len(f.clients) >= maxFailClients {
			clear(f.clients)
		}
		b = newBucket(f.perMin, time.Minute)
		f.clients[client] = b
	}
	f.mu.Unlock()
	b.take(now)
}

// readInbound reads the request body while holding one of the inboundReads
// slots, for at most m.readTimeout. It returns the body, or the status to
// answer: 503 (no free slot), 413 (too large), 408 (too slow) or 400.
func (m *Module) readInbound(w http.ResponseWriter, r *http.Request) ([]byte, int) {
	select {
	case m.reads <- struct{}{}:
		defer func() { <-m.reads }()
	default:
		return nil, http.StatusServiceUnavailable
	}
	rc := http.NewResponseController(w)
	// Not every ResponseWriter supports deadlines (test recorders do not).
	_ = rc.SetReadDeadline(time.Now().Add(m.readTimeout))
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxInboundBytes+1))
	if err == nil {
		_ = rc.SetReadDeadline(time.Time{})
		return raw, 0
	}
	// On a failed read the deadline stays, and the connection is closed after
	// the answer, so the server never waits to drain the rest of the body.
	w.Header().Set("Connection", "close")
	var tooLarge *http.MaxBytesError
	var ne net.Error
	switch {
	case errors.As(err, &tooLarge):
		return nil, http.StatusRequestEntityTooLarge
	case errors.Is(err, os.ErrDeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return nil, http.StatusRequestTimeout
	}
	return nil, http.StatusBadRequest
}
