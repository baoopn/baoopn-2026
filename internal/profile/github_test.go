package profile

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A GitHub failure must answer a JSON 503, not a 502: the proxy replaces 502
// bodies with an HTML page, which would hide the github_unavailable code.
func TestGitHubUnavailableIs503(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer gh.Close()
	m := New(Deps{Log: slog.New(slog.DiscardHandler), GitHubUser: "baoopn", APIBase: gh.URL})
	rec := httptest.NewRecorder()
	m.github(rec, httptest.NewRequest("POST", "/profile/github", nil))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), `"github_unavailable"`) {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}
