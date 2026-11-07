package blog

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestPostCRUDAndVisibility(t *testing.T) {
	h := newHarness(t)
	pub := h.create(PostInput{Title: "Hello World", Parts: one("## Intro\n\nBody text here."), Status: "published",
		Tags: []string{"Go", "web"}, PublishedAt: at(-2 * time.Hour)})
	if pub.Slug != "hello-world" || pub.State != "published" || pub.ReadingMinutes != 1 || pub.Excerpt != "Body text here." ||
		len(pub.Parts) != 1 || pub.Parts[0].No != 1 || pub.Parts[0].ContentMD == "" {
		t.Fatalf("created %+v", pub)
	}
	dup := h.create(PostInput{Title: "Hello World", Status: "draft", Parts: one("")})
	if dup.Slug != "hello-world-2" || dup.State != "draft" {
		t.Fatalf("suffix %+v", dup)
	}
	taken := PostInput{Title: "x", Slug: "hello-world", Status: "draft", Parts: one("")}
	if code := h.do(h.admin, "POST", "/posts", taken, nil); code != http.StatusConflict {
		t.Fatalf("explicit taken: %d", code)
	}
	for _, bad := range []PostInput{{Title: "", Status: "draft", Parts: one("")}, {Title: "No parts", Status: "draft"}} {
		if code := h.do(h.admin, "POST", "/posts", bad, nil); code != http.StatusUnprocessableEntity {
			t.Fatalf("validation %q: %d", bad.Title, code)
		}
	}
	if code := h.do(h.admin, "POST", "/posts", map[string]any{"title": "x", "bogus": 1}, nil); code != http.StatusBadRequest {
		t.Fatalf("unknown field: %d", code)
	}
	sched := h.create(PostInput{Title: "Future", Status: "published", PublishedAt: at(time.Hour), Parts: one("")})
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
	d := detail.Data
	if d.Part.ContentHTML == "" || !bytes.Contains(d.Part.TOC, []byte(`"intro"`)) || d.Prev != nil || d.Next != nil ||
		d.PartCount != 1 || len(d.Parts) != 1 || d.Part.No != 1 {
		t.Fatalf("detail %+v", d)
	}

	var states struct{ Data []AdminPost }
	h.do(h.admin, "GET", "/posts?state=scheduled", nil, &states)
	if len(states.Data) != 1 || states.Data[0].ID != sched.ID {
		t.Fatalf("scheduled filter %+v", states.Data)
	}
	h.do(h.admin, "GET", "/posts", nil, &states)
	if len(states.Data) != 3 || len(states.Data[0].Parts) != 1 {
		t.Fatalf("admin all %+v", states.Data)
	}
	if code := h.do(h.admin, "GET", "/posts?state=bogus", nil, nil); code != 400 {
		t.Fatalf("bad state %d", code)
	}

	// PUT replaces the fields and every part; an empty slug keeps the current one.
	path := "/posts/" + strconv.FormatInt(dup.ID, 10)
	var upd struct{ Data AdminPost }
	code := h.do(h.admin, "PUT", path, PostInput{Title: "Renamed", Status: "published", Parts: []PartInput{
		{Title: "A", ContentMD: "new"}, {Title: "B", ContentMD: "two"}, {ContentMD: "three"}}}, &upd)
	if code != 200 || upd.Data.Slug != "hello-world-2" || upd.Data.Title != "Renamed" || upd.Data.PublishedAt == nil ||
		len(upd.Data.Parts) != 3 || upd.Data.Parts[1].Title != "B" || upd.Data.Parts[2].No != 3 || upd.Data.ReadingMinutes != 3 {
		t.Fatalf("put %d %+v", code, upd.Data)
	}
	code = h.do(h.admin, "PUT", path, PostInput{Title: "Renamed", Slug: "hello-world", Status: "draft", Parts: one("")}, nil)
	if code != http.StatusConflict {
		t.Fatalf("put slug taken %d", code)
	}
	// The failed PUT changed nothing (one transaction).
	var got struct{ Data AdminPost }
	h.do(h.admin, "GET", path, nil, &got)
	if len(got.Data.Parts) != 3 || got.Data.Status != "published" {
		t.Fatalf("after failed put %+v", got.Data)
	}
	code = h.do(h.admin, "PUT", path, PostInput{Title: "Renamed", Status: "published", Parts: one("only")}, &upd)
	if code != 200 || len(upd.Data.Parts) != 1 || upd.Data.Parts[0].ContentMD != "only" {
		t.Fatalf("shrink parts %d %+v", code, upd.Data.Parts)
	}
	if code := h.do(h.admin, "PUT", "/posts/99999", PostInput{Title: "x", Status: "draft", Parts: one("")}, nil); code != 404 {
		t.Fatalf("put missing %d", code)
	}
	if code := h.do(h.admin, "DELETE", path, nil, nil); code != 204 {
		t.Fatalf("delete %d", code)
	}
	if code := h.do(h.admin, "GET", path, nil, nil); code != 404 {
		t.Fatalf("get deleted %d", code)
	}
	var orphans int
	_ = h.m.db.QueryRow(context.Background(), `SELECT count(*) FROM post_parts WHERE post_id = $1`, dup.ID).Scan(&orphans)
	if orphans != 0 {
		t.Fatalf("parts not cascaded: %d", orphans)
	}

	var prev struct{ Data struct{ HTML string } }
	if code := h.do(h.admin, "POST", "/posts/preview", map[string]string{"markdown": "# Hi"}, &prev); code != 200 || prev.Data.HTML == "" {
		t.Fatalf("preview %d", code)
	}
}

func TestPublicListFiltersPagingViews(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.create(PostInput{Title: "Alpha", Parts: one("100% done_ok"), Status: "published", Tags: []string{"go"},
		PublishedAt: at(-3 * time.Hour)})
	h.create(PostInput{Title: "Beta", Parts: []PartInput{{ContentMD: "100 percent"}, {ContentMD: "needle in part two"}},
		Status: "published", Tags: []string{"go", "sql"}, PublishedAt: at(-2 * time.Hour)})
	h.create(PostInput{Title: "Gamma", Parts: one("x"), Status: "published", PublishedAt: at(-1 * time.Hour)})
	h.create(PostInput{Title: "Draft", Parts: one("100% needle"), Status: "draft", Tags: []string{"go"}})
	_, err := h.m.db.Exec(ctx, `INSERT INTO page_view_stats (bucket, path, device, browser, country, referrer, views)
		VALUES (now(), '/blog/beta', 'desktop', 'chrome', '', '', 5),
		(now() - interval '1 hour', '/blog/beta', 'mobile', 'chrome', '', '', 2),
		(now(), '/blog/beta/2', 'desktop', 'chrome', '', '', 3),
		(now(), '/blog/beta-2', 'desktop', 'chrome', '', '', 100)`)
	if err != nil {
		t.Fatal(err)
	}

	type list struct {
		Data []PostSummary
		Meta struct{ Total, Page, Limit int }
	}
	var l list
	h.do(h.public, "GET", "/posts?limit=2", nil, &l)
	if len(l.Data) != 2 || l.Meta.Total != 3 || l.Meta.Limit != 2 || l.Data[0].Slug != "gamma" || l.Data[1].Views != 10 ||
		l.Data[1].PartCount != 2 || l.Data[1].ReadingMinutes != 2 {
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
	for q, want := range map[string]string{"100%": "alpha", "done_ok": "alpha", "needle": "beta"} {
		h.do(h.public, "GET", "/posts?q="+url.QueryEscape(q), nil, &l)
		if len(l.Data) != 1 || l.Data[0].Slug != want {
			t.Fatalf("q=%s %+v", q, l.Data)
		}
	}
	if code := h.do(h.public, "GET", "/posts?limit=51", nil, nil); code != 400 {
		t.Fatalf("limit 51 %d", code)
	}

	var tags struct{ Data []TagCount }
	h.do(h.public, "GET", "/tags", nil, &tags)
	if len(tags.Data) != 2 || tags.Data[0] != (TagCount{"go", 2}) || tags.Data[1] != (TagCount{"sql", 1}) {
		t.Fatalf("tags %+v", tags.Data)
	}
}

func TestCategories(t *testing.T) {
	h := newHarness(t)
	tech := h.create(PostInput{Title: "Tech One", Parts: one("x"), Status: "published", Tags: []string{"go"},
		PublishedAt: at(-2 * time.Hour)})
	life := h.create(PostInput{Title: "Life One", Category: "life", Parts: one("x"), Status: "published",
		Tags: []string{"travel", "go"}, PublishedAt: at(-time.Hour)})
	h.create(PostInput{Title: "Life Draft", Category: "life", Parts: one("x"), Status: "draft", Tags: []string{"food"}})
	if tech.Category != "tech" || life.Category != "life" {
		t.Fatalf("categories %q %q", tech.Category, life.Category)
	}
	if code := h.do(h.admin, "POST", "/posts", PostInput{Title: "x", Category: "food", Status: "draft", Parts: one("")},
		nil); code != http.StatusUnprocessableEntity {
		t.Fatalf("bad category %d", code)
	}

	var l struct{ Data []PostSummary }
	h.do(h.public, "GET", "/posts", nil, &l)
	if len(l.Data) != 2 || l.Data[0].Category != "life" || l.Data[1].Category != "tech" {
		t.Fatalf("all %+v", l.Data)
	}
	for cat, want := range map[string]string{"tech": "tech-one", "life": "life-one"} {
		h.do(h.public, "GET", "/posts?category="+cat, nil, &l)
		if len(l.Data) != 1 || l.Data[0].Slug != want {
			t.Fatalf("category=%s %+v", cat, l.Data)
		}
	}
	h.do(h.public, "GET", "/posts?category=life&tag=go", nil, &l)
	if len(l.Data) != 1 || l.Data[0].Slug != "life-one" {
		t.Fatalf("life+go %+v", l.Data)
	}
	for _, p := range []string{"/posts?category=Life", "/posts?category=all", "/tags?category=x"} {
		if code := h.do(h.public, "GET", p, nil, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d", p, code)
		}
	}
	var d struct{ Data PostDetail }
	h.do(h.public, "GET", "/posts/life-one", nil, &d)
	if d.Data.Category != "life" {
		t.Fatalf("detail category %q", d.Data.Category)
	}

	var tags struct{ Data []TagCount }
	h.do(h.public, "GET", "/tags?category=life", nil, &tags)
	if len(tags.Data) != 2 || tags.Data[0] != (TagCount{"go", 1}) || tags.Data[1] != (TagCount{"travel", 1}) {
		t.Fatalf("life tags %+v", tags.Data)
	}
	h.do(h.public, "GET", "/tags", nil, &tags)
	if len(tags.Data) != 2 || tags.Data[0] != (TagCount{"go", 2}) {
		t.Fatalf("all tags %+v", tags.Data)
	}

	var adm struct{ Data []AdminPost }
	h.do(h.admin, "GET", "/posts?category=life", nil, &adm)
	if len(adm.Data) != 2 {
		t.Fatalf("admin life %+v", adm.Data)
	}
	h.do(h.admin, "GET", "/posts?category=life&state=draft&q=draft", nil, &adm)
	if len(adm.Data) != 1 || adm.Data[0].Title != "Life Draft" {
		t.Fatalf("admin life drafts %+v", adm.Data)
	}
	if code := h.do(h.admin, "GET", "/posts?category=bogus", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("admin bad category %d", code)
	}

	// PUT can move a post between categories; an empty category means tech.
	var upd struct{ Data AdminPost }
	path := "/posts/" + strconv.FormatInt(life.ID, 10)
	h.do(h.admin, "PUT", path, PostInput{Title: "Life One", Status: "published", Parts: one("x")}, &upd)
	if upd.Data.Category != "tech" {
		t.Fatalf("put default category %q", upd.Data.Category)
	}
}

func TestPartNavigationAndImport(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	pa := time.Date(2025, 3, 5, 12, 0, 0, 0, time.UTC)
	in := PostInput{Slug: "my-series", Title: "My Series", Status: "published", PublishedAt: &pa, Parts: []PartInput{
		{Title: "Setup", ContentMD: "## One\n\ntext"}, {ContentMD: "## Two"}, {Title: "End", ContentMD: "## Three"}}}
	got, err := h.m.Import(ctx, []PostInput{in, {Slug: "draft-one", Title: "Draft", Status: "draft", Parts: one("d")}})
	if err != nil || len(got) != 2 {
		t.Fatalf("import: %v %v", got, err)
	}
	if got, err := h.m.Import(ctx, []PostInput{in}); err != nil || len(got) != 0 {
		t.Fatalf("re-import: %v %v", got, err)
	}
	if _, err := h.m.Import(ctx, []PostInput{{Title: "bad", Status: "draft"}}); err == nil {
		t.Fatal("invalid import accepted")
	}

	var d struct{ Data PostDetail }
	h.do(h.public, "GET", "/posts/my-series", nil, &d)
	if len(d.Data.Parts) != 3 || d.Data.Part.No != 1 || d.Data.Part.Title != "Setup" || d.Data.Prev != nil ||
		d.Data.Next == nil || d.Data.Next.No != 2 || d.Data.Next.Title != "" || d.Data.Excerpt != "text" {
		t.Fatalf("first %+v", d.Data)
	}
	h.do(h.public, "GET", "/posts/my-series?part=2", nil, &d)
	if d.Data.Part.No != 2 || d.Data.Prev == nil || d.Data.Prev.No != 1 || d.Data.Next == nil || d.Data.Next.No != 3 ||
		!bytes.Contains(d.Data.Part.TOC, []byte(`"two"`)) {
		t.Fatalf("middle %+v", d.Data)
	}
	h.do(h.public, "GET", "/posts/my-series?part=3", nil, &d)
	if d.Data.Next != nil || d.Data.Prev == nil || d.Data.Prev.No != 2 || d.Data.Part.Title != "End" {
		t.Fatalf("last %+v %+v", d.Data.Prev, d.Data.Next)
	}
	for _, q := range []string{"4", "0", "-1", "abc", "21", "1.5"} {
		if code := h.do(h.public, "GET", "/posts/my-series?part="+q, nil, nil); code != 404 {
			t.Errorf("part=%s: %d", q, code)
		}
	}
	if code := h.do(h.public, "GET", "/posts/draft-one", nil, nil); code != 404 {
		t.Fatalf("imported draft visible %d", code)
	}
	if code := h.do(h.public, "GET", "/posts/legacy?path=/blog/my-series/1", nil, nil); code != 404 {
		t.Fatalf("legacy route still mounted: %d", code)
	}
}
