package contact

import (
	"encoding/json"
	"errors"
	"sync"
)

// Event is one inbox change sent to admin streams. It carries ids and kinds
// only, never message bodies, names or email addresses.
type Event struct {
	Type      string `json:"-"`
	ThreadID  int64  `json:"thread_id,omitempty"`
	MessageID int64  `json:"message_id,omitempty"`
	Direction string `json:"direction,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Status    string `json:"status,omitempty"`
	Change    string `json:"change,omitempty"`
}

// Event types.
const (
	evThreadCreated  = "thread.created"
	evThreadUpdated  = "thread.updated"
	evThreadDeleted  = "thread.deleted"
	evMessageCreated = "message.created"
	evMessageUpdated = "message.updated"
)

const (
	maxEventStreams = 16
	eventBuffer     = 32
)

var (
	errTooManyStreams = errors.New("contact: too many inbox streams")
	errShuttingDown   = errors.New("contact: inbox streams closed for shutdown")
)

// events fans inbox changes out to the admin streams of this process.
//
// It is in-process, which is correct because exactly one API instance runs.
// With more instances, events would reach only the subscribers connected to
// the instance that made the change; the fix then is Postgres LISTEN/NOTIFY
// feeding each instance's hub.
type events struct {
	mu     sync.Mutex
	subs   map[*eventSub]struct{}
	closed bool
	max    int
}

// eventSub receives SSE frames on ch. gone is closed when the subscriber is
// dropped (its buffer filled up) or the hub closed.
type eventSub struct {
	ch   chan []byte
	gone chan struct{}
}

func newEvents(max int) *events {
	return &events{subs: map[*eventSub]struct{}{}, max: max}
}

// subscribe adds a subscriber. The returned func removes it and is safe to
// call more than once.
func (h *events) subscribe() (*eventSub, func(), error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, nil, errShuttingDown
	}
	if len(h.subs) >= h.max {
		return nil, nil, errTooManyStreams
	}
	s := &eventSub{ch: make(chan []byte, eventBuffer), gone: make(chan struct{})}
	h.subs[s] = struct{}{}
	return s, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.drop(s)
	}, nil
}

// drop removes s and closes its gone channel once. h.mu must be held.
func (h *events) drop(s *eventSub) {
	if _, ok := h.subs[s]; ok {
		delete(h.subs, s)
		close(s.gone)
	}
}

// publish sends e to every subscriber without blocking. A subscriber whose
// buffer is full is dropped; its stream ends and the client resyncs on
// reconnect. Safe on a nil hub.
func (h *events) publish(e Event) {
	if h == nil || e.Type == "" {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	frame := make([]byte, 0, len(e.Type)+len(data)+16)
	frame = append(frame, "event: "...)
	frame = append(frame, e.Type...)
	frame = append(frame, "\ndata: "...)
	frame = append(frame, data...)
	frame = append(frame, "\n\n"...)

	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs {
		select {
		case s.ch <- frame:
		default:
			h.drop(s)
		}
	}
}

// Close ends every stream and refuses new ones.
func (h *events) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for s := range h.subs {
		h.drop(s)
	}
}

func (h *events) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
