package listening

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

const (
	streamKeepAlive    = 25 * time.Second // < Cloudflare's 100 s idle limit
	streamRetryMS      = 5000
	streamWriteTimeout = 30 * time.Second // per write: drops peers that stop reading
	maxStreams         = 500
	// maxStreamsPerClient bounds one client IP (IPv6 /64) so it cannot take
	// every slot. A page opens one stream however many widgets it has; this
	// leaves room for a few tabs, or a few people behind one NAT. Clients
	// over it fall back to polling.
	maxStreamsPerClient = 8
)

// streamSlots counts open streams, in total and per client bucket.
type streamSlots struct {
	mu        sync.Mutex
	total     int
	perClient map[string]int
}

// acquire takes a slot, or returns the HTTP status to refuse with.
func (s *streamSlots) acquire(key string, maxTotal, maxPerClient int) (release func(), status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.total >= maxTotal {
		return nil, http.StatusServiceUnavailable
	}
	if s.perClient[key] >= maxPerClient {
		return nil, http.StatusTooManyRequests
	}
	if s.perClient == nil {
		s.perClient = map[string]int{}
	}
	s.total++
	s.perClient[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.total--
			if s.perClient[key]--; s.perClient[key] <= 0 {
				delete(s.perClient, key)
			}
		})
	}, 0
}

func (s *streamSlots) open() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.total
}

// Streams implements httpapi.Streamer: long-lived routes mounted under
// /api/v1 without the request timeout.
func (m *Module) Streams(r chi.Router) {
	r.Get("/listening/now-playing/stream", m.stream)
}

// stream serves now-playing as Server-Sent Events: the cached snapshot first,
// then every change, with keep-alive comments in between.
func (m *Module) stream(w http.ResponseWriter, r *http.Request) {
	release, status := m.streams.acquire(httpapi.ClientBucket(r), m.maxStreams, m.maxStreamsPerClient)
	if release == nil {
		w.Header().Set("Retry-After", "30")
		httpapi.Error(w, status, "too_many_streams", "too many listeners, try again later")
		return
	}
	defer release()

	// The server's ReadTimeout/WriteTimeout (2 min) would cut the stream off.
	// Clear the read deadline; each write below sets its own short deadline,
	// so a peer that stops reading is dropped instead of pinning this
	// goroutine. Unsupported writers (tests) just ignore it.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	extend := func() { _ = rc.SetWriteDeadline(time.Now().Add(m.writeTimeout)) }
	extend()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	var updates <-chan []byte
	if m.hub != nil {
		sub, unsubscribe := m.hub.subscribe()
		defer unsubscribe()
		updates = sub.ch
	}
	if _, err := fmt.Fprintf(w, "retry: %d\n\n", streamRetryMS); err != nil {
		return
	}
	if m.hub == nil {
		// SPOTIFY_API_URL unset: one empty snapshot, then only keep-alives.
		if err := writeEvent(w, []byte("null")); err != nil {
			return
		}
	}
	if rc.Flush() != nil {
		return
	}

	ka := time.NewTicker(m.keepAlive)
	defer ka.Stop()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return
		case <-ka.C:
			extend()
			_, err = fmt.Fprint(w, ": keep-alive\n\n")
		case b := <-updates:
			extend()
			err = writeEvent(w, b)
			ka.Reset(m.keepAlive)
		}
		if err == nil {
			err = rc.Flush()
		}
		if err != nil {
			return
		}
	}
}

// writeEvent writes one now-playing event. b is compact JSON (no newlines).
func writeEvent(w http.ResponseWriter, b []byte) error {
	_, err := fmt.Fprintf(w, "event: now-playing\ndata: %s\n\n", b)
	return err
}
