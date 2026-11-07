package overview

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/analytics"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

func TestOverviewCounts(t *testing.T) {
	pool := testdb.New(t)
	ctx := t.Context()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, q := range []string{
		`INSERT INTO threads (reply_token, name, email, subject, read_at) VALUES ('t1','A','a@x.io','one',NULL)`,
		`INSERT INTO threads (reply_token, name, email, subject, read_at) VALUES ('t2','B','b@x.io','two',now())`,
		`INSERT INTO threads (reply_token, name, email, subject, status) VALUES ('t3','C','c@x.io','three','archived')`,
		`INSERT INTO posts (slug, title, status, published_at) VALUES ('p1','P1','published', now() - interval '1 day')`,
		`INSERT INTO posts (slug, title, status, published_at) VALUES ('p2','P2','published', now() + interval '1 day')`,
		`INSERT INTO posts (slug, title) VALUES ('p3','P3')`,
		`INSERT INTO projects (slug, title, visible, synced_at) VALUES ('a','A',true, '2026-11-01T00:00:00Z')`,
		`INSERT INTO projects (slug, title) VALUES ('b','B')`,
		`INSERT INTO page_view_stats (bucket, path, device, browser, country, referrer, views)
		 VALUES (date_trunc('hour', now()), '/', 'desktop', 'Chrome', '', '', 6),
		        (date_trunc('hour', now()) - interval '8 days', '/', 'desktop', 'Chrome', '', '', 50)`,
		`INSERT INTO page_visitor_hours (hour, visitor) VALUES (date_trunc('hour', now()), 'v1'),
		        (date_trunc('hour', now()) - interval '1 hour', 'v1'), (date_trunc('hour', now()), 'v2')`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	rec := analytics.New(analytics.Deps{DB: pool, Log: log, Salt: "s"})
	m := New(Deps{DB: pool, Log: log, Analytics: rec})

	w := httptest.NewRecorder()
	m.handle(w, httptest.NewRequest(http.MethodGet, "/api/admin/overview", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"unread_threads", "open_threads", "posts", "projects", "views_7d",
		"visitors_7d", "live_now", "last_sync_at", "recent_threads", "top_paths_7d"} {
		if _, ok := env.Data[k]; !ok {
			t.Errorf("missing field %q", k)
		}
	}

	ov, err := m.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ov.UnreadThreads != 1 || ov.OpenThreads != 2 {
		t.Errorf("threads unread %d open %d", ov.UnreadThreads, ov.OpenThreads)
	}
	if ov.Posts != (PostCounts{Published: 1, Drafts: 1, Scheduled: 1}) {
		t.Errorf("posts = %+v", ov.Posts)
	}
	if ov.Projects != (ProjectCounts{Visible: 1, Total: 2}) {
		t.Errorf("projects = %+v", ov.Projects)
	}
	if ov.LastSyncAt == nil || !ov.LastSyncAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("last_sync_at = %v", ov.LastSyncAt)
	}
	if ov.Views7d != 6 || ov.Visitors7d != 2 {
		t.Errorf("views %d visitors %d", ov.Views7d, ov.Visitors7d)
	}
	if len(ov.RecentThreads) != 2 || ov.RecentThreads[0].Status != "open" {
		t.Errorf("recent = %+v", ov.RecentThreads)
	}
	if len(ov.TopPaths7d) != 1 || ov.TopPaths7d[0].Views != 6 {
		t.Errorf("top paths = %+v", ov.TopPaths7d)
	}
	if strings.Contains(w.Body.String(), "reply_token") || strings.Contains(w.Body.String(), `"t1"`) {
		t.Error("overview leaks the reply token")
	}
}
