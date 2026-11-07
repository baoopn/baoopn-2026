package listening

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/baoopn/baoopn-2026/internal/logging"
)

const upstreamStreamPath = "/api/v1/now-playing/stream"

// hub keeps ONE upstream SSE connection to spotify-tracker while at least one
// client is subscribed, fans each (trimmed) now-playing payload out to the
// subscribers and caches the latest one. The upstream connection is closed
// idleClose after the last subscriber leaves.
type hub struct {
	url    string
	client *http.Client
	log    *slog.Logger

	idleClose  time.Duration // close upstream this long after the last client
	readIdle   time.Duration // reconnect when nothing arrives for this long
	backoffMin time.Duration
	backoffMax time.Duration

	mu        sync.Mutex
	subs      map[*subscriber]struct{}
	latest    []byte // trimmed NowPlaying JSON (may be "null"); nil = none yet
	live      bool   // the upstream stream is connected and has sent a snapshot
	cancel    context.CancelFunc
	idleTimer *time.Timer
	conns     int // upstream connections opened (for tests)
}

// subscriber receives payloads on a 1-slot channel: a slow client only ever
// misses intermediate updates, never the latest one.
type subscriber struct {
	ch chan []byte
}

func (s *subscriber) offer(b []byte) {
	for {
		select {
		case s.ch <- b:
			return
		default:
		}
		select {
		case <-s.ch: // drop the stale pending value, then retry
		default:
		}
	}
}

// streamClient has no overall timeout (the response is long-lived) but bounds
// dialing, TLS and waiting for response headers.
func streamClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
}

func newHub(base string, client *http.Client, log *slog.Logger) *hub {
	if client == nil {
		client = streamClient()
	}
	return &hub{
		url: base + upstreamStreamPath, client: client, log: log,
		idleClose: 60 * time.Second, readIdle: 70 * time.Second,
		backoffMin: time.Second, backoffMax: 30 * time.Second,
		subs: map[*subscriber]struct{}{},
	}
}

// subscribe registers a client. The cached snapshot, if any, is queued first.
// The returned func unsubscribes.
func (h *hub) subscribe() (*subscriber, func()) {
	s := &subscriber{ch: make(chan []byte, 1)}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subs[s] = struct{}{}
	if h.latest != nil {
		s.offer(h.latest)
	}
	if h.idleTimer != nil {
		h.idleTimer.Stop()
		h.idleTimer = nil
	}
	if h.cancel == nil {
		ctx, cancel := context.WithCancel(context.Background())
		h.cancel = cancel
		go h.run(ctx)
	}
	var once sync.Once
	return s, func() { once.Do(func() { h.unsubscribe(s) }) }
}

func (h *hub) unsubscribe(s *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subs, s)
	if len(h.subs) == 0 && h.cancel != nil && h.idleTimer == nil {
		h.idleTimer = time.AfterFunc(h.idleClose, h.stopIfIdle)
	}
}

func (h *hub) stopIfIdle() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.idleTimer = nil
	if len(h.subs) > 0 || h.cancel == nil {
		return
	}
	h.cancel()
	h.cancel = nil
	h.live = false
	h.latest = nil // would be stale by the next subscribe
}

// snapshot returns the cached payload while the upstream stream is live.
func (h *hub) snapshot() ([]byte, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest, h.live && h.latest != nil
}

func (h *hub) running() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cancel != nil
}

func (h *hub) publish(ctx context.Context, b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() != nil {
		return // a stopped run must not overwrite a newer one
	}
	h.latest = b
	h.live = true
	for s := range h.subs {
		s.offer(b)
	}
}

func (h *hub) setLive(ctx context.Context, v bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ctx.Err() == nil {
		h.live = v
	}
}

// run keeps the upstream connection open until ctx is cancelled,
// reconnecting with exponential backoff and jitter.
func (h *hub) run(ctx context.Context) {
	attempt := 0
	for {
		got, retry, err := h.connect(ctx)
		h.setLive(ctx, false)
		if ctx.Err() != nil {
			return
		}
		if got {
			attempt = 0
		}
		d := h.backoff(attempt, retry)
		attempt++
		h.log.Warn("listening: upstream stream lost", logging.Err(err), slog.Duration("retry_in", d))
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// backoff is min·2^attempt capped at max (at least the server's retry:),
// with ±50 % jitter.
func (h *hub) backoff(attempt int, retry time.Duration) time.Duration {
	d := h.backoffMin
	for i := 0; i < attempt && d < h.backoffMax; i++ {
		d *= 2
	}
	d = max(d, retry)
	d = min(d, h.backoffMax)
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + rand.N(d-half+1)
}

var errStreamEnded = errors.New("upstream stream ended")

// connect runs one upstream connection. got reports whether at least one
// event was received; retry is the last retry: value seen.
func (h *hub) connect(parent context.Context) (got bool, retry time.Duration, err error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	h.mu.Lock()
	h.conns++
	h.mu.Unlock()
	resp, err := h.client.Do(req)
	if err != nil {
		return false, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return false, 0, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}

	// Read-idle watchdog: keep-alives arrive every 25 s, so silence for
	// readIdle means the connection is dead even if TCP has not noticed.
	watchdog := time.AfterFunc(h.readIdle, cancel)
	defer watchdog.Stop()
	rd := newSSEReader(resp.Body, func() { watchdog.Reset(h.readIdle) })
	for {
		ev, err := rd.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = errStreamEnded
			}
			if ctx.Err() != nil && parent.Err() == nil {
				err = fmt.Errorf("no data for %s", h.readIdle)
			}
			return got, retry, err
		}
		if ev.Retry > 0 {
			retry = ev.Retry
		}
		if ev.Type != "now-playing" {
			continue
		}
		b, err := trimNowPlaying([]byte(ev.Data))
		if err != nil {
			h.log.Warn("listening: bad now-playing event", logging.Err(err))
			continue
		}
		got = true
		h.publish(parent, b)
	}
}

// trimNowPlaying decodes a NowPlaying (or null), accepting either the bare
// object or a {"data": …} envelope, and re-encodes it in our trimmed shape.
func trimNowPlaying(raw []byte) ([]byte, error) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err == nil {
		if d, ok := probe["data"]; ok {
			if _, has := probe["is_playing"]; !has {
				raw = d
			}
		}
	}
	var v *NowPlaying
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	if v != nil {
		v.FetchedAt = v.FetchedAt.UTC()
		v.Track.normalize()
	}
	return json.Marshal(v)
}
