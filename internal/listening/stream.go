package listening

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

const (
	streamKeepAlive = 25 * time.Second // < Cloudflare's 100 s idle limit
	streamRetryMS   = 5000
	maxStreams      = 500
)

// Streams implements httpapi.Streamer: long-lived routes mounted under
// /api/v1 without the request timeout.
func (m *Module) Streams(r chi.Router) {
	r.Get("/listening/now-playing/stream", m.stream)
}

// stream serves now-playing as Server-Sent Events: the cached snapshot first,
// then every change, with keep-alive comments in between.
func (m *Module) stream(w http.ResponseWriter, r *http.Request) {
	if n := m.streams.Add(1); n > int64(m.maxStreams) {
		m.streams.Add(-1)
		w.Header().Set("Retry-After", "30")
		httpapi.Error(w, http.StatusServiceUnavailable, "too_many_streams", "too many listeners, try again later")
		return
	}
	defer m.streams.Add(-1)

	// The server's Read/WriteTimeout (2 min) would cut the stream off; clear
	// them for this connection. Unsupported writers (tests) just ignore it.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	_ = rc.SetReadDeadline(time.Time{})

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
			_, err = fmt.Fprint(w, ": keep-alive\n\n")
		case b := <-updates:
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
