package contact

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

const (
	eventsKeepAlive    = 25 * time.Second // < Cloudflare's 100 s idle limit
	eventsRetryMS      = 5000
	eventsWriteTimeout = 30 * time.Second // per write: drops peers that stop reading
	// eventsMaxAge ends each stream so the client reconnects with a fresh
	// session token (the token is checked once per connection).
	eventsMaxAge = 15 * time.Minute
)

// AdminStreams implements httpapi.AdminStreamer: the inbox event stream,
// mounted under /api/admin behind AdminAuth without the request timeout.
func (m *Module) AdminStreams(r chi.Router) {
	r.Get("/inbox/events", m.handleEvents)
}

// CloseStreams ends every open inbox stream and refuses new ones, so open
// streams do not hold up server shutdown.
func (m *Module) CloseStreams() { m.events.Close() }

// handleEvents serves inbox changes as Server-Sent Events: retry, then ready,
// then every change, with keep-alive comments in between. There is no replay;
// the client refetches on every ready.
func (m *Module) handleEvents(w http.ResponseWriter, r *http.Request) {
	sub, unsubscribe, err := m.events.subscribe()
	if err != nil {
		w.Header().Set("Retry-After", "10")
		httpapi.Error(w, http.StatusServiceUnavailable, "too_many_streams", "too many inbox streams, try again later")
		return
	}
	defer unsubscribe()

	// The server's ReadTimeout/WriteTimeout (2 min) would cut the stream off.
	// Clear the read deadline; each write sets its own short deadline, so a
	// peer that stops reading is dropped. Unsupported writers ignore it.
	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{})
	extend := func() { _ = rc.SetWriteDeadline(time.Now().Add(m.eventsWriteTimeout)) }
	extend()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := fmt.Fprintf(w, "retry: %d\n\nevent: ready\ndata: {}\n\n", eventsRetryMS); err != nil {
		return
	}
	if rc.Flush() != nil {
		return
	}

	ka := time.NewTicker(m.eventsKeepAlive)
	defer ka.Stop()
	maxAge := time.NewTimer(m.eventsMaxAge)
	defer maxAge.Stop()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return
		case <-sub.gone:
			return
		case <-maxAge.C:
			return
		case <-ka.C:
			extend()
			_, err = fmt.Fprint(w, ": keep-alive\n\n")
		case b := <-sub.ch:
			extend()
			_, err = w.Write(b)
			ka.Reset(m.eventsKeepAlive)
		}
		if err == nil {
			err = rc.Flush()
		}
		if err != nil {
			return
		}
	}
}
