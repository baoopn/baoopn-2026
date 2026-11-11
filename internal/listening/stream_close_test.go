package listening

import (
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

// waitEOF waits for the stream reader to end (the server closed the stream).
func waitEOF(t *testing.T, lines <-chan string) {
	t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-lines:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("stream still open")
		}
	}
}

func TestCloseStreamsEndsStreams(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	srv := streamServer(t, m)
	_, a := openStream(t, srv.URL)
	_, b := openStream(t, srv.URL)
	readUntil(t, a, "data: null")
	readUntil(t, b, "data: null")

	m.CloseStreams()
	m.CloseStreams() // idempotent
	waitEOF(t, a)
	waitEOF(t, b)
	eventually(t, "slots released", func() bool { return m.streams.open() == 0 })

	resp, err := http.Get(srv.URL + "/api/v1/listening/now-playing/stream")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") != "10" ||
		!strings.Contains(string(body), `"shutting_down"`) {
		t.Fatalf("stream after CloseStreams: %d %s", resp.StatusCode, body)
	}
}

func TestStreamMaxAge(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	m.maxAge, m.maxAgeJitter = 100*time.Millisecond, 20*time.Millisecond
	srv := streamServer(t, m)
	_, lines := openStream(t, srv.URL)
	readUntil(t, lines, "data: null")
	waitEOF(t, lines)
	eventually(t, "slot released", func() bool { return m.streams.open() == 0 })
}

func TestStreamLifetimeJitter(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler)})
	for range 50 {
		if d := m.streamLifetime(); d < streamMaxAge || d >= streamMaxAge+streamMaxAgeJitter {
			t.Fatalf("lifetime %s outside [%s, %s)", d, streamMaxAge, streamMaxAge+streamMaxAgeJitter)
		}
	}
}
