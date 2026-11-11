package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeErrors(t *testing.T) {
	type body struct {
		Name string `json:"name"`
	}
	for _, tc := range []struct {
		name, in, code, msg string
		status              int
		leaks               []string // decoder detail that must not be echoed
	}{
		{"unknown field", `{"secretField":1}`, "bad_request", "invalid JSON body", http.StatusBadRequest,
			[]string{"secretField", "unknown field"}},
		{"bad type", `{"name":5}`, "bad_request", "invalid JSON body", http.StatusBadRequest,
			[]string{"Go struct field", "body.name", "of type string", "number"}},
		{"syntax", `{`, "bad_request", "invalid JSON body", http.StatusBadRequest,
			[]string{"unexpected", "EOF"}},
		{"too large", `{"name":"` + strings.Repeat("x", maxJSONBody) + `"}`, "too_large", "request body too large",
			http.StatusRequestEntityTooLarge, []string{"http: request body too large"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var v body
			if Decode(rec, httptest.NewRequest("POST", "/", strings.NewReader(tc.in)), &v) {
				t.Fatal("decode accepted")
			}
			got := rec.Body.String()
			var b struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
				t.Fatalf("decode %s: %v", got, err)
			}
			if rec.Code != tc.status || b.Error.Code != tc.code || b.Error.Message != tc.msg {
				t.Fatalf("got %d %s", rec.Code, got)
			}
			for _, leak := range append(tc.leaks, "json:") {
				if strings.Contains(got, leak) {
					t.Fatalf("response leaks %q: %s", leak, got)
				}
			}
		})
	}
	rec := httptest.NewRecorder()
	var v body
	if !Decode(rec, httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"ok"}`)), &v) || v.Name != "ok" {
		t.Fatalf("valid body rejected: %d %s", rec.Code, rec.Body)
	}
}

// The detail goes to the default logger at debug (cmd/api's logging.New
// installs the app logger with slog.SetDefault).
func TestDecodeLogsDetail(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	var v struct{}
	Decode(httptest.NewRecorder(), httptest.NewRequest("POST", "/x", strings.NewReader(`{"secretField":1}`)), &v)
	if !strings.Contains(buf.String(), "secretField") || !strings.Contains(buf.String(), "level=DEBUG") {
		t.Fatalf("detail not logged at debug: %s", buf.String())
	}
}
