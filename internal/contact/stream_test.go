package contact

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

// streamServer serves m's admin streams (no auth: the router adds it).
func streamServer(t *testing.T, m *Module) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	m.AdminStreams(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func newStreamModule() *Module {
	return New(Deps{Log: slog.New(slog.DiscardHandler)})
}

// sseConn reads an open stream frame by frame (frames end with a blank line).
type sseConn struct {
	t    *testing.T
	res  *http.Response
	rd   *bufio.Reader
	stop context.CancelFunc
}

func openStream(t *testing.T, srv *httptest.Server) *sseConn {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/inbox/events", nil)
	res, err := srv.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	c := &sseConn{t: t, res: res, rd: bufio.NewReader(res.Body), stop: cancel}
	t.Cleanup(c.close)
	return c
}

func (c *sseConn) close() { c.stop(); _ = c.res.Body.Close() }

// frame returns the next frame without its trailing blank line, or io.EOF.
func (c *sseConn) frame() (string, error) {
	c.t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := c.rd.ReadString('\n')
			if err != nil {
				ch <- result{b.String(), err}
				return
			}
			if line == "\n" {
				ch <- result{b.String(), nil}
				return
			}
			b.WriteString(line)
		}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(3 * time.Second):
		c.t.Fatal("timed out waiting for a frame")
		return "", nil
	}
}

func (c *sseConn) want(frame string) {
	c.t.Helper()
	got, err := c.frame()
	if err != nil || got != frame {
		c.t.Fatalf("frame = %q (%v), want %q", got, err, frame)
	}
}

func (c *sseConn) wantEOF() {
	c.t.Helper()
	if got, err := c.frame(); err != io.EOF {
		c.t.Fatalf("want end of stream, got %q %v", got, err)
	}
}

// waitSubs waits until m has n subscribers.
func waitSubs(t *testing.T, m *Module, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for m.events.count() != n {
		if time.Now().After(deadline) {
			t.Fatalf("subscribers = %d, want %d", m.events.count(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestEventStream(t *testing.T) {
	m := newStreamModule()
	srv := streamServer(t, m)
	c := openStream(t, srv)
	if c.res.StatusCode != 200 {
		t.Fatalf("status %d", c.res.StatusCode)
	}
	h := c.res.Header
	if h.Get("Content-Type") != "text/event-stream" || h.Get("Cache-Control") != "no-store" || h.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers %v", h)
	}
	c.want("retry: 5000\n")
	c.want("event: ready\ndata: {}\n")

	m.events.publish(Event{Type: evMessageCreated, ThreadID: 3, MessageID: 9, Direction: "outbound", RequestID: "req-abc-123"})
	c.want("event: message.created\ndata: {\"thread_id\":3,\"message_id\":9,\"direction\":\"outbound\",\"request_id\":\"req-abc-123\"}\n")

	// The client going away frees its slot.
	c.close()
	waitSubs(t, m, 0)
}

func TestEventStreamKeepAlive(t *testing.T) {
	m := newStreamModule()
	m.eventsKeepAlive = 50 * time.Millisecond
	c := openStream(t, streamServer(t, m))
	c.want("retry: 5000\n")
	c.want("event: ready\ndata: {}\n")
	c.want(": keep-alive\n")
	c.want(": keep-alive\n")
}

func TestEventStreamFull(t *testing.T) {
	m := newStreamModule()
	m.events.max = 1
	srv := streamServer(t, m)
	c := openStream(t, srv)
	c.want("retry: 5000\n")
	res, err := srv.Client().Get(srv.URL + "/inbox/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 503 || res.Header.Get("Retry-After") != "10" || !strings.Contains(string(body), `"too_many_streams"`) {
		t.Fatalf("second stream: %d %q %s", res.StatusCode, res.Header.Get("Retry-After"), body)
	}
}

func TestEventStreamMaxAge(t *testing.T) {
	m := newStreamModule()
	m.eventsMaxAge = 100 * time.Millisecond
	c := openStream(t, streamServer(t, m))
	c.want("retry: 5000\n")
	c.want("event: ready\ndata: {}\n")
	c.wantEOF()
	waitSubs(t, m, 0)
}

func TestCloseStreamsEndsStreams(t *testing.T) {
	m := newStreamModule()
	srv := streamServer(t, m)
	a, b := openStream(t, srv), openStream(t, srv)
	for _, c := range []*sseConn{a, b} {
		c.want("retry: 5000\n")
		c.want("event: ready\ndata: {}\n")
	}
	m.CloseStreams()
	a.wantEOF()
	b.wantEOF()
	res, err := srv.Client().Get(srv.URL + "/inbox/events")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != 503 || res.Header.Get("Retry-After") != "10" || !strings.Contains(string(body), `"shutting_down"`) {
		t.Fatalf("stream after CloseStreams: %d %s", res.StatusCode, body)
	}
}
