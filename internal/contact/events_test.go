package contact

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func recv(t *testing.T, s *eventSub) string {
	t.Helper()
	select {
	case b := <-s.ch:
		return string(b)
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
		return ""
	}
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func TestEventsFanOut(t *testing.T) {
	h := newEvents(4)
	a, unA, err := h.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	b, unB, err := h.subscribe()
	if err != nil {
		t.Fatal(err)
	}
	h.publish(Event{Type: evThreadCreated, ThreadID: 7})
	want := "event: thread.created\ndata: {\"thread_id\":7}\n\n"
	if got := recv(t, a); got != want {
		t.Errorf("a got %q", got)
	}
	if got := recv(t, b); got != want {
		t.Errorf("b got %q", got)
	}

	unA()
	unA() // safe twice
	if !isClosed(a.gone) || h.count() != 1 {
		t.Fatalf("unsubscribe: gone %v, count %d", isClosed(a.gone), h.count())
	}
	h.publish(Event{Type: evThreadDeleted, ThreadID: 7})
	if got := recv(t, b); !strings.HasPrefix(got, "event: thread.deleted\n") {
		t.Errorf("b got %q", got)
	}
	select {
	case x := <-a.ch:
		t.Errorf("unsubscribed got %q", x)
	default:
	}
	unB()
}

// A subscriber that stops reading is dropped; publish never blocks.
func TestEventsDropsFullSubscriber(t *testing.T) {
	h := newEvents(4)
	slow, _, _ := h.subscribe()
	fast, unFast, _ := h.subscribe()
	defer unFast()
	done := make(chan struct{})
	go func() {
		for i := range eventBuffer + 5 {
			h.publish(Event{Type: evMessageUpdated, ThreadID: 1, MessageID: int64(i + 1), Status: "sent"})
			<-fast.ch
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked")
	}
	if !isClosed(slow.gone) {
		t.Error("full subscriber not dropped")
	}
	if isClosed(fast.gone) || h.count() != 1 {
		t.Errorf("fast dropped %v, count %d", isClosed(fast.gone), h.count())
	}
}

func TestEventsLimitAndClose(t *testing.T) {
	h := newEvents(2)
	a, _, _ := h.subscribe()
	b, unB, _ := h.subscribe()
	if _, _, err := h.subscribe(); !errors.Is(err, errTooManyStreams) {
		t.Fatalf("third subscribe: %v", err)
	}
	unB()
	c, _, err := h.subscribe()
	if err != nil {
		t.Fatalf("after unsubscribe: %v", err)
	}
	h.Close()
	if !isClosed(a.gone) || !isClosed(b.gone) || !isClosed(c.gone) {
		t.Error("Close left a stream open")
	}
	if _, _, err := h.subscribe(); !errors.Is(err, errShuttingDown) {
		t.Fatalf("subscribe after Close: %v", err)
	}
	h.publish(Event{Type: evThreadCreated, ThreadID: 1}) // no panic after Close
	h.Close()                                            // twice is fine
}

func TestEventsNilHub(t *testing.T) {
	var h *events
	h.publish(Event{Type: evThreadCreated, ThreadID: 1})
	h.Close()
}

// Events carry ids and kinds only.
func TestEventHasNoContentFields(t *testing.T) {
	allowed := map[string]bool{"thread_id": true, "message_id": true, "direction": true,
		"request_id": true, "status": true, "change": true, "-": true}
	ty := reflect.TypeFor[Event]()
	for i := range ty.NumField() {
		tag, _, _ := strings.Cut(ty.Field(i).Tag.Get("json"), ",")
		if !allowed[tag] {
			t.Errorf("Event field %s (%q) is not an id or kind", ty.Field(i).Name, tag)
		}
		for _, bad := range []string{"body", "name", "email", "subject", "preview"} {
			if strings.Contains(strings.ToLower(tag), bad) {
				t.Errorf("Event field %q looks like content", tag)
			}
		}
	}
}
