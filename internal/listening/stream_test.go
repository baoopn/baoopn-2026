package listening

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

func TestSSEParser(t *testing.T) {
	in := "\xef\xbb\xbf: hello\n" +
		"retry: 1500\n" +
		"event: now-playing\n" +
		"data: {\"a\":\n" +
		"data:1}\n" +
		"id: 7\n" +
		"\n" +
		"data: plain\r\n\r\n" + // CRLF, default type
		": keep-alive\r" + // lone CR
		"event: ignored-no-data\n\n" +
		"retry: nope\n" +
		"data\n\n" + // field with no colon: empty data line
		"event: x\ndata: partial" // unterminated: dropped at EOF
	lines := 0
	p := newSSEReader(strings.NewReader(in), func() { lines++ })

	want := []sseEvent{
		{Type: "now-playing", Data: "{\"a\":\n1}", ID: "7", Retry: 1500 * time.Millisecond},
		{Type: "message", Data: "plain", ID: "7"},
		{Type: "message", Data: "", ID: "7"},
	}
	for i, w := range want {
		got, err := p.Next()
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		if got != w {
			t.Fatalf("event %d: got %+v want %+v", i, got, w)
		}
	}
	if _, err := p.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("want EOF, got %v", err)
	}
	if lines < 12 {
		t.Fatalf("onLine called %d times", lines)
	}
}

func TestTrimNowPlaying(t *testing.T) {
	for in, want := range map[string]string{
		`null`:          `null`,
		`{"data":null}`: `null`,
		`{"is_playing":true,"progress_ms":5,"fetched_at":"2026-11-06T19:00:00+07:00","extra":1,"track":{"id":"t","name":"N","uri":"x","duration_ms":9,"explicit":false}}`: `{"is_playing":true,"progress_ms":5,"fetched_at":"2026-11-06T12:00:00Z","track":{"id":"t","name":"N","duration_ms":9,"explicit":false,"artists":[]}}`,
	} {
		got, err := trimNowPlaying([]byte(in))
		if err != nil || string(got) != want {
			t.Errorf("%s: got %s %v", in, got, err)
		}
	}
	if _, err := trimNowPlaying([]byte(`{`)); err == nil {
		t.Error("bad JSON accepted")
	}
}

// sseUpstream is a fake spotify-tracker stream endpoint.
type sseUpstream struct {
	mu       sync.Mutex
	status   []int // per-connection status overrides, consumed in order
	silent   bool  // send headers, then nothing (no snapshot, no keep-alive)
	snapshot string
	active   int
	total    int
	events   chan string
	kill     chan struct{}
}

func newSSEUpstream(snapshot string) *sseUpstream {
	return &sseUpstream{snapshot: snapshot, events: make(chan string, 16), kill: make(chan struct{}, 1)}
}

func (u *sseUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != upstreamStreamPath {
		w.WriteHeader(404)
		return
	}
	u.mu.Lock()
	u.total++
	status := 200
	if len(u.status) > 0 {
		status, u.status = u.status[0], u.status[1:]
	}
	snap, silent := u.snapshot, u.silent
	if status == 200 {
		u.active++
	}
	u.mu.Unlock()
	if status != 200 {
		w.WriteHeader(status)
		return
	}
	defer func() { u.mu.Lock(); u.active--; u.mu.Unlock() }()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(200)
	if !silent {
		_, _ = fmt.Fprintf(w, "retry: 10\n\nevent: now-playing\ndata: %s\n\n", snap)
	}
	w.(http.Flusher).Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-u.kill:
			return
		case ev := <-u.events:
			_, _ = fmt.Fprintf(w, ": keep-alive\n\nevent: now-playing\ndata: %s\n\n", ev)
			w.(http.Flusher).Flush()
		}
	}
}

func (u *sseUpstream) counts() (active, total int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.active, u.total
}

func np(id string) string {
	return `{"is_playing":true,"progress_ms":1,"fetched_at":"2026-11-06T12:00:00Z","track":{"id":"` + id + `","name":"N","duration_ms":9,"explicit":false,"artists":null}}`
}

func fastHub(url string) *hub {
	h := newHub(url, nil, slog.New(slog.DiscardHandler))
	h.backoffMin, h.backoffMax = 5*time.Millisecond, 20*time.Millisecond
	h.idleClose, h.readIdle = 100*time.Millisecond, 300*time.Millisecond
	return h
}

func recv(t *testing.T, s *subscriber) string {
	t.Helper()
	select {
	case b := <-s.ch:
		return string(b)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for an update")
		return ""
	}
}

func wantTrack(t *testing.T, s *subscriber, id string) {
	t.Helper()
	if got := recv(t, s); !strings.Contains(got, `"id":"`+id+`"`) || !strings.Contains(got, `"artists":[]`) {
		t.Fatalf("want track %s, got %s", id, got)
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHub(t *testing.T) {
	u := newSSEUpstream(np("a"))
	srv := httptest.NewServer(u)
	defer srv.Close()
	h := fastHub(srv.URL)

	// Fan-out of the snapshot and of pushes over a single upstream connection.
	s1, un1 := h.subscribe()
	s2, un2 := h.subscribe()
	wantTrack(t, s1, "a")
	wantTrack(t, s2, "a")
	u.events <- np("b")
	wantTrack(t, s1, "b")
	wantTrack(t, s2, "b")
	if _, total := u.counts(); total != 1 {
		t.Fatalf("upstream connections: %d", total)
	}
	if b, ok := h.snapshot(); !ok || !strings.Contains(string(b), `"id":"b"`) {
		t.Fatalf("snapshot %s %v", b, ok)
	}

	// A new client gets the cached value first, without a new upstream call.
	s3, un3 := h.subscribe()
	wantTrack(t, s3, "b")
	un3()

	// Upstream drops and restarts: the hub reconnects.
	u.mu.Lock()
	u.snapshot = np("c")
	u.mu.Unlock()
	u.kill <- struct{}{}
	wantTrack(t, s1, "c")
	wantTrack(t, s2, "c")
	if _, total := u.counts(); total != 2 {
		t.Fatalf("upstream connections after reconnect: %d", total)
	}

	// Slow client: s1 does not read while three updates go out; it then
	// sees only the latest.
	for _, id := range []string{"d", "e", "f"} {
		u.events <- np(id)
		wantTrack(t, s2, id)
	}
	wantTrack(t, s1, "f")
	select {
	case b := <-s1.ch:
		t.Fatalf("slow client got an extra update: %s", b)
	default:
	}

	// Last client leaves: the upstream connection closes after idleClose.
	un1()
	un2()
	un2() // idempotent
	if !h.running() {
		t.Fatal("stopped before idleClose")
	}
	eventually(t, "upstream closed after idle", func() bool {
		active, _ := u.counts()
		return active == 0 && !h.running()
	})
	if _, ok := h.snapshot(); ok {
		t.Fatal("snapshot still live after stop")
	}

	// Subscribing again reopens it.
	s4, un4 := h.subscribe()
	defer un4()
	wantTrack(t, s4, "c")
}

func TestHubResubscribeCancelsIdleClose(t *testing.T) {
	u := newSSEUpstream(np("a"))
	srv := httptest.NewServer(u)
	defer srv.Close()
	h := fastHub(srv.URL)
	s, un := h.subscribe()
	wantTrack(t, s, "a")
	un()
	s2, un2 := h.subscribe() // within idleClose
	defer un2()
	wantTrack(t, s2, "a")
	time.Sleep(200 * time.Millisecond)
	if active, total := u.counts(); active != 1 || total != 1 {
		t.Fatalf("active %d total %d", active, total)
	}
}

func TestHub5xxAndWatchdog(t *testing.T) {
	u := newSSEUpstream(np("a"))
	u.status = []int{503, 502}
	u.silent = true
	srv := httptest.NewServer(u)
	defer srv.Close()
	h := fastHub(srv.URL)
	s, un := h.subscribe()
	defer un()

	// Two 5xx, then a connection that stays silent: the watchdog must cut it.
	eventually(t, "5xx retried", func() bool { _, total := u.counts(); return total >= 3 })
	time.Sleep(50 * time.Millisecond)
	u.mu.Lock()
	u.silent = false
	u.mu.Unlock()
	wantTrack(t, s, "a") // only after the read-idle watchdog reconnected
	if _, total := u.counts(); total != 4 {
		t.Fatalf("connections: %d", total)
	}
}

func TestBackoff(t *testing.T) {
	h := newHub("x", nil, slog.New(slog.DiscardHandler))
	for attempt, hi := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second} {
		for range 20 {
			d := h.backoff(attempt, 0)
			if d < hi/2 || d > hi {
				t.Fatalf("attempt %d: %s not in [%s, %s]", attempt, d, hi/2, hi)
			}
		}
	}
	if d := h.backoff(0, 5*time.Second); d < 2500*time.Millisecond {
		t.Fatalf("retry: ignored: %s", d)
	}
}

// streamServer serves the module through the real router, on a server with
// short Read/WriteTimeouts the stream must survive.
func streamServer(t *testing.T, m *Module) *httptest.Server {
	m.keepAlive = 40 * time.Millisecond
	srv := httptest.NewUnstartedServer(httpapi.New(httpapi.Deps{
		Log: slog.New(slog.DiscardHandler), PublicRPM: 1000, Modules: []httpapi.Module{m},
	}))
	srv.Config.ReadTimeout = 200 * time.Millisecond
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func openStream(t *testing.T, url string) (*http.Response, <-chan string) {
	t.Helper()
	resp, err := http.Get(url + "/api/v1/listening/now-playing/stream")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	lines := make(chan string, 256)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	return resp, lines
}

func readUntil(t *testing.T, lines <-chan string, want string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatalf("stream closed before %q", want)
			}
			if strings.Contains(l, want) {
				return
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %q", want)
		}
	}
}

func TestStreamHandler(t *testing.T) {
	u := newSSEUpstream(np("a"))
	up := httptest.NewServer(u)
	t.Cleanup(up.Close) // runs last, once the hub has let go
	m := New(Deps{BaseURL: up.URL, Log: slog.New(slog.DiscardHandler)})
	m.hub.idleClose = 10 * time.Millisecond
	srv := streamServer(t, m)

	resp, lines := openStream(t, srv.URL)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for k, v := range map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-store", "X-Accel-Buffering": "no"} {
		if got := resp.Header.Get(k); got != v {
			t.Errorf("%s = %q", k, got)
		}
	}
	readUntil(t, lines, "retry: 5000")
	readUntil(t, lines, "event: now-playing")
	readUntil(t, lines, `"id":"a"`)
	u.events <- np("b")
	readUntil(t, lines, `"id":"b"`)

	// Past the 200 ms server Read/WriteTimeout: still alive, keep-alives flow
	// and updates arrive.
	time.Sleep(500 * time.Millisecond)
	readUntil(t, lines, ": keep-alive")
	u.events <- np("c")
	readUntil(t, lines, `"id":"c"`)

	// The JSON endpoint now answers from the hub cache, without a fetch.
	rec := httptest.NewRecorder()
	m.nowPlaying(rec, httptest.NewRequest("GET", "/listening/now-playing", nil))
	if !strings.Contains(rec.Body.String(), `"id":"c"`) {
		t.Fatalf("now-playing from cache: %s", rec.Body)
	}
}

func TestStreamCap(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	m.maxStreams = 1
	srv := streamServer(t, m)
	_, lines := openStream(t, srv.URL)
	readUntil(t, lines, "data: null")
	resp, err := http.Get(srv.URL + "/api/v1/listening/now-playing/stream")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over cap: %d", resp.StatusCode)
	}
}

func TestStreamNoBaseURL(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	srv := streamServer(t, m)
	_, lines := openStream(t, srv.URL)
	readUntil(t, lines, "event: now-playing")
	readUntil(t, lines, "data: null")
	time.Sleep(300 * time.Millisecond)
	readUntil(t, lines, ": keep-alive")
}

func TestStreamPerClientCap(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	m.maxStreamsPerClient = 1
	srv := streamServer(t, m)
	resp1, lines := openStream(t, srv.URL)
	readUntil(t, lines, "data: null")
	resp, err := http.Get(srv.URL + "/api/v1/listening/now-playing/stream")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("over per-client cap: %d", resp.StatusCode)
	}
	// Another client bucket still gets a slot.
	if release, status := m.streams.acquire("192.0.2.1", m.maxStreams, m.maxStreamsPerClient); release == nil {
		t.Fatalf("other client refused: %d", status)
	} else {
		release()
		release() // idempotent
	}
	_ = resp1.Body.Close()
	eventually(t, "slot released", func() bool { return m.streams.open() == 0 })
}

// A client that stops reading must be dropped by the per-write deadline,
// releasing its slot and hub subscription.
func TestStreamStuckClientDropped(t *testing.T) {
	u := newSSEUpstream(np("a"))
	up := httptest.NewServer(u)
	t.Cleanup(up.Close)
	m := New(Deps{BaseURL: up.URL, Log: slog.New(slog.DiscardHandler)})
	m.hub.idleClose = 10 * time.Millisecond
	srv := streamServer(t, m)
	m.writeTimeout = 100 * time.Millisecond

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.(*net.TCPConn).SetReadBuffer(4096)
	if _, err := fmt.Fprint(conn, "GET /api/v1/listening/now-playing/stream HTTP/1.1\r\nHost: x\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "stream open", func() bool { return m.streams.open() == 1 })

	// Large events fill the socket buffers; the client never reads.
	big := strings.Repeat("x", 256<<10)
	ev := `{"is_playing":true,"progress_ms":1,"fetched_at":"2026-11-06T12:00:00Z","track":{"id":"t","name":"` + big + `","duration_ms":9,"explicit":false,"artists":[]}}`
	deadline := time.Now().Add(10 * time.Second)
	for m.streams.open() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("stuck client was not dropped")
		}
		select {
		case u.events <- ev:
		case <-time.After(50 * time.Millisecond):
		}
	}
	eventually(t, "hub unsubscribed and stopped", func() bool { return !m.hub.running() })
}

func TestHubNoStaleSnapshot(t *testing.T) {
	u := newSSEUpstream(np("a"))
	srv := httptest.NewServer(u)
	defer srv.Close()
	h := fastHub(srv.URL)
	h.backoffMin, h.backoffMax = time.Second, time.Second // stay down for a while
	s, un := h.subscribe()
	defer un()
	wantTrack(t, s, "a")
	u.kill <- struct{}{}
	eventually(t, "not live", func() bool { _, ok := h.snapshot(); return !ok })
	s2, un2 := h.subscribe()
	defer un2()
	select {
	case b := <-s2.ch:
		t.Fatalf("new client got a stale snapshot while upstream is down: %s", b)
	default:
	}
}

func TestNilHubSnapshot(t *testing.T) {
	var h *hub
	if _, ok := h.snapshot(); ok {
		t.Fatal("nil hub reported a snapshot")
	}
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	rec := httptest.NewRecorder()
	m.nowPlaying(rec, httptest.NewRequest("GET", "/listening/now-playing", nil)) // must not panic
}

func TestSSEEventCap(t *testing.T) {
	line := "data: " + strings.Repeat("y", 600<<10) + "\n"
	in := line + line + "\n" + "data: ok\n\n"
	p := newSSEReader(strings.NewReader(in), nil)
	ev, err := p.Next()
	if err != nil || ev.Data != "ok" {
		t.Fatalf("oversized event not dropped: %q %v", ev.Data[:min(len(ev.Data), 10)], err)
	}
}
