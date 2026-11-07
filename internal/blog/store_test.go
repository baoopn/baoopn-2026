package blog

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

type harness struct {
	t      *testing.T
	m      *Module
	public chi.Router
	admin  chi.Router
}

func newHarness(t *testing.T) *harness {
	pool := testdb.New(t)
	m := New(Deps{DB: pool, Log: slog.New(slog.DiscardHandler)})
	pub, adm := chi.NewRouter(), chi.NewRouter()
	m.Public(pub)
	m.Admin(adm)
	return &harness{t: t, m: m, public: pub, admin: adm}
}

func (h *harness) do(r chi.Router, method, path string, body any, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
	if out != nil && rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			h.t.Fatalf("%s %s: decode %v: %s", method, path, err, rec.Body)
		}
	}
	return rec.Code
}

func (h *harness) create(in PostInput) AdminPost {
	h.t.Helper()
	var out struct{ Data AdminPost }
	if code := h.do(h.admin, "POST", "/posts", in, &out); code != http.StatusCreated {
		h.t.Fatalf("create %q: %d", in.Title, code)
	}
	return out.Data
}

func at(d time.Duration) *time.Time {
	t := time.Now().Add(d).UTC().Truncate(time.Second)
	return &t
}

func TestPostCRUDAndVisibility(t *testing.T) {
	h := newHarness(t)
	pub := h.create(PostInput{Title: "Hello World", ContentMD: "## Intro\n\nBody text here.", Status: "published",
		Tags: []string{"Go", "web"}, PublishedAt: at(-2 * time.Hour)})
	if pub.Slug != "hello-world" || pub.State != "published" || pub.ReadingMinutes != 1 || pub.Excerpt != "Body text here." {
		t.Fatalf("created %+v", pub)
	}
	dup := h.create(PostInput{Title: "Hello World", Status: "draft"})
	if dup.Slug != "hello-world-2" || dup.State != "draft" {
		t.Fatalf("suffix %+v", dup)
	}
	if code := h.do(h.admin, "POST", "/posts", PostInput{Title: "x", Slug: "hello-world", Status: "draft"}, nil); code != http.StatusConflict {
		t.Fatalf("explicit taken: %d", code)
	}
	if code := h.do(h.admin, "POST", "/posts", PostInput{Title: "", Status: "draft"}, nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("validation: %d", code)
	}
	if code := h.do(h.admin, "POST", "/posts", map[string]any{"title": "x", "bogus": 1}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", code)
	}
	sched := h.create(PostInput{Title: "Future", Status: "published", PublishedAt: at(time.Hour)})
	if sched.State != "scheduled" {
		t.Fatalf("scheduled state %s", sched.State)
	}

	for _, slug := range []string{dup.Slug, sched.Slug} {
		if code := h.do(h.public, "GET", "/posts/"+slug, nil, nil); code != http.StatusNotFound {
			t.Fatalf("hidden %s visible: %d", slug, code)
		}
	}
	var detail struct{ Data PostDetail }
	if code := h.do(h.public, "GET", "/posts/hello-world", nil, &detail); code != 200 {
		t.Fatalf("detail %d", code)
	}
	if detail.Data.ContentHTML == "" || !bytes.Contains(detail.Data.TOC, []byte(`"intro"`)) || detail.Data.Prev != nil {
		t.Fatalf("detail %+v", detail.Data)
	}

	var states struct{ Data []AdminPost }
	h.do(h.admin, "GET", "/posts?state=scheduled", nil, &states)
	if len(states.Data) != 1 || states.Data[0].ID != sched.ID {
		t.Fatalf("scheduled filter %+v", states.Data)
	}
	h.do(h.admin, "GET", "/posts", nil, &states)
	if len(states.Data) != 3 {
		t.Fatalf("admin all %d", len(states.Data))
	}
	if code := h.do(h.admin, "GET", "/posts?state=bogus", nil, nil); code != 400 {
		t.Fatalf("bad state %d", code)
	}

	// PUT replaces the fields; an empty slug keeps the current one.
	var upd struct{ Data AdminPost }
	code := h.do(h.admin, "PUT", "/posts/"+strconv.FormatInt(dup.ID, 10),
		PostInput{Title: "Renamed", Status: "published", ContentMD: "new"}, &upd)
	if code != 200 || upd.Data.Slug != "hello-world-2" || upd.Data.Title != "Renamed" || upd.Data.PublishedAt == nil {
		t.Fatalf("put %d %+v", code, upd.Data)
	}
	code = h.do(h.admin, "PUT", "/posts/"+strconv.FormatInt(dup.ID, 10),
		PostInput{Title: "Renamed", Slug: "hello-world", Status: "draft"}, nil)
	if code != http.StatusConflict {
		t.Fatalf("put slug taken %d", code)
	}
	if code := h.do(h.admin, "PUT", "/posts/99999", PostInput{Title: "x", Status: "draft"}, nil); code != 404 {
		t.Fatalf("put missing %d", code)
	}
	if code := h.do(h.admin, "DELETE", "/posts/"+strconv.FormatInt(dup.ID, 10), nil, nil); code != 204 {
		t.Fatalf("delete %d", code)
	}
	if code := h.do(h.admin, "GET", "/posts/"+strconv.FormatInt(dup.ID, 10), nil, nil); code != 404 {
		t.Fatalf("get deleted %d", code)
	}

	var prev struct{ Data struct{ HTML string } }
	if code := h.do(h.admin, "POST", "/posts/preview", map[string]string{"markdown": "# Hi"}, &prev); code != 200 || prev.Data.HTML == "" {
		t.Fatalf("preview %d", code)
	}
}

func TestPublicListFiltersPagingViews(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.create(PostInput{Title: "Alpha", ContentMD: "100% done_ok", Status: "published", Tags: []string{"go"}, PublishedAt: at(-3 * time.Hour)})
	h.create(PostInput{Title: "Beta", ContentMD: "100 percent", Status: "published", Tags: []string{"go", "sql"}, PublishedAt: at(-2 * time.Hour)})
	h.create(PostInput{Title: "Gamma", ContentMD: "x", Status: "published", Series: "s1", PublishedAt: at(-1 * time.Hour)})
	h.create(PostInput{Title: "Draft", ContentMD: "100%", Status: "draft", Tags: []string{"go"}})
	_, err := h.m.db.Exec(ctx, `INSERT INTO page_view_stats (bucket, path, device, browser, country, referrer, views)
		VALUES (now(), '/blog/beta', 'desktop', 'chrome', '', '', 5), (now() - interval '1 hour', '/blog/beta', 'mobile', 'chrome', '', '', 2)`)
	if err != nil {
		t.Fatal(err)
	}

	type list struct {
		Data []PostSummary
		Meta struct{ Total, Page, Limit int }
	}
	var l list
	h.do(h.public, "GET", "/posts?limit=2", nil, &l)
	if len(l.Data) != 2 || l.Meta.Total != 3 || l.Meta.Limit != 2 || l.Data[0].Slug != "gamma" || l.Data[1].Views != 7 {
		t.Fatalf("page1 %+v", l)
	}
	h.do(h.public, "GET", "/posts?limit=2&page=2", nil, &l)
	if len(l.Data) != 1 || l.Data[0].Slug != "alpha" || l.Meta.Page != 2 || l.Meta.Total != 3 {
		t.Fatalf("page2 %+v", l)
	}
	h.do(h.public, "GET", "/posts?page=9", nil, &l)
	if len(l.Data) != 0 || l.Meta.Total != 3 {
		t.Fatalf("past end %+v", l)
	}
	h.do(h.public, "GET", "/posts?tag=go", nil, &l)
	if len(l.Data) != 2 {
		t.Fatalf("tag %+v", l.Data)
	}
	h.do(h.public, "GET", "/posts?series=s1", nil, &l)
	if len(l.Data) != 1 || l.Data[0].Slug != "gamma" {
		t.Fatalf("series %+v", l.Data)
	}
	h.do(h.public, "GET", "/posts?q="+url.QueryEscape("100%"), nil, &l)
	if len(l.Data) != 1 || l.Data[0].Slug != "alpha" {
		t.Fatalf("q escaping %+v", l.Data)
	}
	h.do(h.public, "GET", "/posts?q="+url.QueryEscape("done_ok"), nil, &l)
	if len(l.Data) != 1 {
		t.Fatalf("q underscore %+v", l.Data)
	}
	if code := h.do(h.public, "GET", "/posts?limit=51", nil, nil); code != 400 {
		t.Fatalf("limit 51 %d", code)
	}

	var tags struct{ Data []TagCount }
	h.do(h.public, "GET", "/tags", nil, &tags)
	if len(tags.Data) != 2 || tags.Data[0] != (TagCount{"go", 2}) || tags.Data[1] != (TagCount{"sql", 1}) {
		t.Fatalf("tags %+v", tags.Data)
	}

	// Date neighbours for posts outside a series.
	var d struct{ Data PostDetail }
	h.do(h.public, "GET", "/posts/beta", nil, &d)
	if d.Data.Prev == nil || d.Data.Prev.Slug != "alpha" || d.Data.Next == nil || d.Data.Next.Slug != "gamma" || d.Data.Views != 7 {
		t.Fatalf("neighbours %+v %+v", d.Data.Prev, d.Data.Next)
	}
}

func TestSeriesNavImportAndLegacy(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	base := time.Date(2025, 3, 5, 12, 0, 0, 0, time.UTC)
	var posts []ImportPost
	for i := 1; i <= 3; i++ {
		pa := base.Add(time.Duration(i) * time.Minute)
		posts = append(posts, ImportPost{
			PostInput: PostInput{Slug: "series-part-" + strconv.Itoa(i), Title: "Part " + strconv.Itoa(i), ContentMD: "text",
				Series: "my-series", SeriesTitle: "My Series", SeriesOrder: i, Status: "published", PublishedAt: &pa},
			LegacyPath: "/blog/my-series/" + strconv.Itoa(i),
		})
	}
	n, err := h.m.Import(ctx, posts)
	if err != nil || n != 3 {
		t.Fatalf("import: %d %v", n, err)
	}
	if n, err := h.m.Import(ctx, posts); err != nil || n != 0 {
		t.Fatalf("re-import: %d %v", n, err)
	}

	var d struct{ Data PostDetail }
	h.do(h.public, "GET", "/posts/series-part-1", nil, &d)
	if len(d.Data.SeriesPosts) != 3 || d.Data.Prev != nil || d.Data.Next == nil || d.Data.Next.Slug != "series-part-2" {
		t.Fatalf("first %+v", d.Data)
	}
	h.do(h.public, "GET", "/posts/series-part-3", nil, &d)
	if d.Data.Next != nil || d.Data.Prev == nil || d.Data.Prev.SeriesOrder != 2 {
		t.Fatalf("last %+v %+v", d.Data.Prev, d.Data.Next)
	}

	var lg struct{ Data struct{ Slug string } }
	if code := h.do(h.public, "GET", "/posts/legacy?path=/Blog/My-Series/2/", nil, &lg); code != 200 || lg.Data.Slug != "series-part-2" {
		t.Fatalf("legacy %d %+v", code, lg)
	}
	if code := h.do(h.public, "GET", "/posts/legacy?path=/blog/none/1", nil, nil); code != 404 {
		t.Fatalf("legacy missing %d", code)
	}
	if code := h.do(h.public, "GET", "/posts/legacy", nil, nil); code != 400 {
		t.Fatalf("legacy empty %d", code)
	}

	// The legacy path survives a PUT.
	var a struct{ Data []AdminPost }
	h.do(h.admin, "GET", "/posts?q=part-2", nil, &a)
	if len(a.Data) != 1 || a.Data[0].LegacyPath == nil {
		t.Fatalf("admin q %+v", a.Data)
	}
	var upd struct{ Data AdminPost }
	h.do(h.admin, "PUT", "/posts/"+strconv.FormatInt(a.Data[0].ID, 10),
		PostInput{Title: "Changed", Status: "published", PublishedAt: a.Data[0].PublishedAt}, &upd)
	if upd.Data.LegacyPath == nil || *upd.Data.LegacyPath != "/blog/my-series/2" {
		t.Fatalf("legacy kept %+v", upd.Data)
	}
}
