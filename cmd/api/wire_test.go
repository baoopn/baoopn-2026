package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/baoopn/baoopn-2026/internal/config"
	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

func TestAdminConfig(t *testing.T) {
	cfg := config.Config{ClerkPublishableKey: "pk_test_x", GitHubUser: "baoopn",
		InboundDomain: "reply.baoopn.com", InboundSecret: "s"}
	w := httptest.NewRecorder()
	adminConfigHandler(cfg)(w, httptest.NewRequest(http.MethodGet, "/api/admin/config", nil))
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"clerk_publishable_key": "pk_test_x", "version": "dev", "mail_enabled": false,
		"inbound_enabled": true, "github_user": "baoopn"}
	for k, v := range want {
		if env.Data[k] != v {
			t.Errorf("%s = %v, want %v", k, env.Data[k], v)
		}
	}
	if len(env.Data) != len(want) {
		t.Errorf("unexpected fields: %v", env.Data)
	}
}

// TestRouterMounts builds every module against a test pool and mounts them;
// chi panics on duplicate routes.
func TestRouterMounts(t *testing.T) {
	pool := testdb.New(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{PublicURL: "https://baoopn.com", GitHubUser: "baoopn", MediaDir: t.TempDir(), MaxUploadMB: 1}
	mods, err := buildModules(t.Context(), cfg, pool, log)
	if err != nil {
		t.Fatal(err)
	}
	pass := func(next http.Handler) http.Handler { return next }
	h := httpapi.New(httpapi.Deps{Log: log, DB: pool, AdminAuth: pass, Media: mods.media,
		Modules: mods.list, AdminConfig: adminConfigHandler(cfg)})
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"POST", "/api/v1/events", `{"type":"pageview","path":"/"}`, http.StatusAccepted},
		{"GET", "/api/v1/profile", "", http.StatusOK},
		{"GET", "/api/v1/projects", "", http.StatusOK},
		{"GET", "/api/v1/posts", "", http.StatusOK},
		{"GET", "/api/v1/posts/legacy?path=/blog/portfolio-app/1", "", http.StatusNotFound},
		{"GET", "/api/v1/tags", "", http.StatusOK},
		{"GET", "/api/admin/posts", "", http.StatusOK},
		{"GET", "/api/admin/threads", "", http.StatusOK},
		{"GET", "/api/admin/settings", "", http.StatusOK},
		{"GET", "/api/admin/overview", "", http.StatusOK},
		{"GET", "/api/admin/analytics?range=7d&tz=Asia/Ho_Chi_Minh", "", http.StatusOK},
		{"GET", "/api/admin/analytics?range=1y", "", http.StatusBadRequest},
		{"GET", "/api/admin/analytics?tz=Nope/Nope", "", http.StatusBadRequest},
		{"GET", "/api/admin/overview", "", http.StatusOK},
		{"GET", "/api/admin/me", "", http.StatusOK},
		{"GET", "/api/admin/config", "", http.StatusOK},
	} {
		req := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
		req.Header.Set("User-Agent", "Mozilla/5.0 Chrome/141.0")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Errorf("%s %s = %d, want %d: %s", c.method, c.path, w.Code, c.want, w.Body)
		}
	}
}
