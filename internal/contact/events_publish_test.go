package contact

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// listen subscribes to e's inbox events.
func (e *env) listen() *eventSub {
	e.t.Helper()
	s, unsubscribe, err := e.m.events.subscribe()
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(unsubscribe)
	return s
}

// drain returns the frames published so far, as "type data" lines.
func drain(s *eventSub) []string {
	var out []string
	for {
		select {
		case b := <-s.ch:
			typ, data, _ := strings.Cut(strings.TrimSuffix(string(b), "\n\n"), "\ndata: ")
			out = append(out, strings.TrimPrefix(typ, "event: ")+" "+data)
		default:
			return out
		}
	}
}

func wantEvents(t *testing.T, s *eventSub, want ...string) {
	t.Helper()
	got := drain(s)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestFormPublishesThreadCreated(t *testing.T) {
	e := newEnv(t, false, testDomain)
	s := e.listen()
	if code, raw := e.form("Priya Shah", "priya@example.com", "", "I would like to talk about a project."); code != 201 {
		t.Fatalf("form: %d %s", code, raw)
	}
	e.wait()
	id := e.count(`SELECT id FROM threads`)
	wantEvents(t, s, fmt.Sprintf(`thread.created {"thread_id":%d}`, id))

	// The honeypot stores nothing and publishes nothing.
	e.do("POST", "/contact", `{"name":"Bot","email":"b@example.com","message":"buy now please","website":"x","started_at":1}`, nil)
	wantEvents(t, s)
}

func TestInboundPublishesMessageCreated(t *testing.T) {
	e := newEnv(t, false, testDomain)
	id, tok := e.thread("priya@example.com")
	s := e.listen()
	raw := email("Priya <priya@example.com>", replyAddr(tok), "<in-1@mail.example>", "Monday works for me.")
	if code := e.hook(raw, replyAddr(tok)); code != 204 {
		t.Fatalf("hook: %d", code)
	}
	mid := e.count(`SELECT id FROM thread_messages WHERE source = 'email'`)
	wantEvents(t, s, fmt.Sprintf(`message.created {"thread_id":%d,"message_id":%d,"direction":"inbound"}`, id, mid))

	// The same Message-ID again is a duplicate: nothing new.
	if code := e.hook(raw, replyAddr(tok)); code != 204 {
		t.Fatalf("duplicate hook: %d", code)
	}
	// Mail to no live thread is a reject: nothing either.
	if code := e.hook(raw, replyAddr(strings.Repeat("ab", 16))); code != 204 {
		t.Fatalf("unknown token hook: %d", code)
	}
	wantEvents(t, s)
}

func TestReplyPublishesCreatedThenUpdated(t *testing.T) {
	e := newEnv(t, true, testDomain)
	id, _ := e.thread("priya@example.com")
	s := e.listen()
	path := fmt.Sprintf("/threads/%d/messages", id)
	body := `{"body":"See you Monday.","request_id":"req-events-0001"}`
	if code, raw := e.do("POST", path, body, nil); code != 201 {
		t.Fatalf("reply: %d %s", code, raw)
	}
	mid := e.count(`SELECT id FROM thread_messages WHERE request_id = 'req-events-0001'`)
	wantEvents(t, s,
		fmt.Sprintf(`message.created {"thread_id":%d,"message_id":%d,"direction":"outbound","request_id":"req-events-0001"}`, id, mid),
		fmt.Sprintf(`message.updated {"thread_id":%d,"message_id":%d,"status":"sent"}`, id, mid))

	// A replay answers 200 with the first message and publishes nothing.
	if code, raw := e.do("POST", path, body, nil); code != 200 {
		t.Fatalf("replay: %d %s", code, raw)
	}
	wantEvents(t, s)

	// A failed send, then a retry.
	e.mailer.mu.Lock()
	e.mailer.fail = errors.New("smtp down")
	e.mailer.mu.Unlock()
	if code, raw := e.do("POST", path, `{"body":"Second try","request_id":"req-events-0002"}`, nil); code != 201 {
		t.Fatalf("failed reply: %d %s", code, raw)
	}
	fid := e.count(`SELECT id FROM thread_messages WHERE request_id = 'req-events-0002'`)
	wantEvents(t, s,
		fmt.Sprintf(`message.created {"thread_id":%d,"message_id":%d,"direction":"outbound","request_id":"req-events-0002"}`, id, fid),
		fmt.Sprintf(`message.updated {"thread_id":%d,"message_id":%d,"status":"failed"}`, id, fid))
	e.mailer.mu.Lock()
	e.mailer.fail = nil
	e.mailer.mu.Unlock()
	if code, raw := e.do("POST", fmt.Sprintf("%s/%d/retry", path, fid), "", nil); code != 200 {
		t.Fatalf("retry: %d %s", code, raw)
	}
	wantEvents(t, s, fmt.Sprintf(`message.updated {"thread_id":%d,"message_id":%d,"status":"sent"}`, id, fid))

	// A retry that is refused publishes nothing.
	if code, _ := e.do("POST", fmt.Sprintf("%s/%d/retry", path, fid), "", nil); code != 409 {
		t.Fatalf("second retry: %d", code)
	}
	wantEvents(t, s)
}

func TestThreadChangesPublish(t *testing.T) {
	e := newEnv(t, false, testDomain)
	id, _ := e.thread("priya@example.com")
	s := e.listen()
	base := fmt.Sprintf("/threads/%d", id)
	for _, step := range []struct{ method, path, body string }{
		{"POST", base + "/read", ""},
		{"POST", base + "/unread", ""},
		{"PATCH", base, `{"status":"archived"}`},
		{"DELETE", base, ""},
	} {
		if code, raw := e.do(step.method, step.path, step.body, nil); code/100 != 2 {
			t.Fatalf("%s %s: %d %s", step.method, step.path, code, raw)
		}
	}
	wantEvents(t, s,
		fmt.Sprintf(`thread.updated {"thread_id":%d,"change":"read"}`, id),
		fmt.Sprintf(`thread.updated {"thread_id":%d,"change":"unread"}`, id),
		fmt.Sprintf(`thread.updated {"thread_id":%d,"status":"archived","change":"status"}`, id),
		fmt.Sprintf(`thread.deleted {"thread_id":%d}`, id))

	// The thread is gone: every action is a 404 and publishes nothing.
	for _, step := range []struct{ method, path, body string }{
		{"POST", base + "/read", ""},
		{"POST", base + "/unread", ""},
		{"PATCH", base, `{"status":"open"}`},
		{"DELETE", base, ""},
		{"PATCH", base, `{"status":"bogus"}`},
	} {
		if code, _ := e.do(step.method, step.path, step.body, nil); code != 404 && code != 422 {
			t.Fatalf("%s %s after delete: %d", step.method, step.path, code)
		}
	}
	wantEvents(t, s)
}
