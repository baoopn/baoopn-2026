package httpapi

import (
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
		name, in, code string
		status         int
	}{
		{"unknown field", `{"secretField":1}`, `"bad_request"`, http.StatusBadRequest},
		{"bad type", `{"name":5}`, `"bad_request"`, http.StatusBadRequest},
		{"syntax", `{`, `"bad_request"`, http.StatusBadRequest},
		{"too large", `{"name":"` + strings.Repeat("x", maxJSONBody) + `"}`, `"too_large"`, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			var v body
			if Decode(rec, httptest.NewRequest("POST", "/", strings.NewReader(tc.in)), &v) {
				t.Fatal("decode accepted")
			}
			got := rec.Body.String()
			if rec.Code != tc.status || !strings.Contains(got, tc.code) {
				t.Fatalf("got %d %s", rec.Code, got)
			}
			// The decoder's detail must not be echoed.
			for _, leak := range []string{"secretField", "string", "unexpected", "json:"} {
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
