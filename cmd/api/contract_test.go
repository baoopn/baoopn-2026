package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Contract tests: every endpoint of docs/api.md through the real router, with
// response key sets and value types checked against web/src/lib/api/types.ts.

func TestContractProfileAndProjects(t *testing.T) {
	c := newHarness(t)

	c.ok("GET", "/api/v1/profile", nil, 200, profileShape, false)
	prof := c.ok("GET", "/api/admin/profile", nil, 200, profileShape, false)
	// The admin form sends back what it loaded (read-only fields included).
	c.ok("PUT", "/api/admin/profile", prof, 200, profileShape, false)
	c.ok("POST", "/api/admin/profile/github", nil, 200, partial(profileFields), false)

	res := c.ok("POST", "/api/admin/projects/sync", nil, 200, syncResult, false)
	if field(res, "added") != float64(2) {
		t.Errorf("sync added = %v, want 2 (private repo skipped)", field(res, "added"))
	}
	all := list(c.ok("GET", "/api/admin/projects", nil, 200, arr(adminProject), false))
	var tracker, old any
	for _, p := range all {
		switch field(p, "slug") {
		case "spotify-tracker":
			tracker = p
		case "old-site":
			old = p
		}
		if field(p, "visible") != false {
			t.Errorf("synced repo %v is visible, want hidden", field(p, "slug"))
		}
	}
	if tracker == nil || old == nil {
		t.Fatalf("synced projects missing: %v", all)
	}
	// Hidden projects are not public.
	c.fail("GET", "/api/v1/projects/spotify-tracker", nil, 404, "not_found")
	if n := len(list(c.ok("GET", "/api/v1/projects", nil, 200, arr(projectSummary), false))); n != 0 {
		t.Errorf("public projects before publishing = %d, want 0", n)
	}

	c.ok("PATCH", "/api/admin/projects/"+idOf(tracker), map[string]any{"visible": true, "featured": true,
		"period": "2025 – now", "tech": []string{"Go", "Svelte"}, "body_md": "## Why\n\nBecause."}, 200, adminProject, false)
	manual := c.ok("POST", "/api/admin/projects", map[string]any{"title": "Hand Made"}, 201, adminProject, false)
	if field(manual, "github_id") != nil || field(manual, "slug") != "hand-made" {
		t.Errorf("manual project = %v", manual)
	}
	c.ok("GET", "/api/admin/projects/"+idOf(manual), nil, 200, adminProject, false)
	c.ok("PATCH", "/api/admin/projects/"+idOf(manual), map[string]any{"visible": true}, 200, adminProject, false)
	c.empty("POST", "/api/admin/projects/reorder", map[string]any{"ids": []any{field(manual, "id"), field(tracker, "id")}}, 204)
	c.fail("PATCH", "/api/admin/projects/"+idOf(manual), map[string]any{"slug": "spotify-tracker"}, 409, "slug_taken")
	c.fail("GET", "/api/admin/projects/999999", nil, 404, "not_found")

	pub := list(c.ok("GET", "/api/v1/projects", nil, 200, arr(projectSummary), false))
	// Featured first, then sort_order.
	if len(pub) != 2 || field(pub[0], "slug") != "spotify-tracker" || field(pub[1], "slug") != "hand-made" {
		t.Errorf("public projects order = %v", pub)
	}
	feat := list(c.ok("GET", "/api/v1/projects?featured=1", nil, 200, arr(projectSummary), false))
	if len(feat) != 1 || field(feat[0], "slug") != "spotify-tracker" {
		t.Errorf("featured = %v", feat)
	}
	d := c.ok("GET", "/api/v1/projects/spotify-tracker", nil, 200, projectDetail, false)
	if field(d, "body_source") != "case_study" {
		t.Errorf("body_source = %v, want case_study", field(d, "body_source"))
	}
	c.fail("GET", "/api/v1/projects/old-site", nil, 404, "not_found")
	c.fail("GET", "/api/v1/projects?featured=maybe", nil, 400, "bad_request")

	c.empty("DELETE", "/api/admin/projects/"+idOf(manual), nil, 204)
	c.fail("GET", "/api/v1/projects/hand-made", nil, 404, "not_found")
}

func TestContractPosts(t *testing.T) {
	c := newHarness(t)
	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	input := func(title, status string, at any) map[string]any {
		return map[string]any{"title": title, "excerpt": "", "cover_url": "/media/cover.png",
			"tags": []string{"Go", "Testing"}, "status": status, "published_at": at,
			"parts": []map[string]string{
				{"title": "Intro", "content_md": "## Intro\n\nHello **world**.\n\n### Detail\n\nMore."},
				{"title": "", "content_md": "## Second\n\nPart two."},
			}}
	}

	pub := c.ok("POST", "/api/admin/posts", input("Contract Testing in Go", "published", past), 201, adminPost, false)
	sched := c.ok("POST", "/api/admin/posts", input("Coming Soon", "published", future), 201, adminPost, false)
	draft := c.ok("POST", "/api/admin/posts", input("Half Written", "draft", nil), 201, adminPost, false)
	now := c.ok("POST", "/api/admin/posts", input("Right Now", "published", nil), 201, adminPost, false)
	if field(sched, "state") != "scheduled" || field(draft, "state") != "draft" || field(pub, "state") != "published" {
		t.Errorf("states: %v %v %v", field(sched, "state"), field(draft, "state"), field(pub, "state"))
	}
	if field(now, "published_at") == nil {
		t.Error("published with null published_at must default to now")
	}
	if field(pub, "category") != "tech" {
		t.Errorf("missing category = %v, want tech", field(pub, "category"))
	}
	lifeIn := input("A Day Off", "published", past)
	lifeIn["category"], lifeIn["tags"] = "life", []string{"Travel"}
	if got := c.ok("POST", "/api/admin/posts", lifeIn, 201, adminPost, false); field(got, "category") != "life" {
		t.Errorf("life category = %v", field(got, "category"))
	}
	badCat := input("Bad Category", "draft", nil)
	badCat["category"] = "food"
	c.fail("POST", "/api/admin/posts", badCat, 422, "validation")
	c.fail("POST", "/api/admin/posts", map[string]any{"title": "x", "slug": "contract-testing-in-go", "status": "draft",
		"excerpt": "", "cover_url": "", "tags": []string{}, "published_at": nil,
		"parts": []map[string]string{{"title": "", "content_md": ""}}}, 409, "slug_taken")
	c.fail("POST", "/api/admin/posts", input("", "draft", nil), 422, "validation")
	noParts := input("No Parts", "draft", nil)
	noParts["parts"] = []map[string]string{}
	c.fail("POST", "/api/admin/posts", noParts, 422, "validation")
	if parts := list(field(pub, "parts")); len(parts) != 2 || field(parts[1], "no") != float64(2) {
		t.Errorf("admin parts = %v", parts)
	}

	c.ok("GET", "/api/admin/posts", nil, 200, arr(adminPost), false)
	if s := list(c.ok("GET", "/api/admin/posts?state=scheduled", nil, 200, arr(adminPost), false)); len(s) != 1 {
		t.Errorf("scheduled filter = %d posts", len(s))
	}
	c.ok("GET", "/api/admin/posts?q=contract", nil, 200, arr(adminPost), false)
	c.ok("GET", "/api/admin/posts/"+idOf(pub), nil, 200, adminPost, false)
	c.ok("PUT", "/api/admin/posts/"+idOf(draft), input("Half Written", "draft", nil), 200, adminPost, false)
	c.ok("POST", "/api/admin/posts/preview", map[string]string{"markdown": "## A\n\ntext"}, 200, previewResult, false)

	// Public: our published posts, one entry per post.
	posts := list(c.ok("GET", "/api/v1/posts?limit=50", nil, 200, arr(postSummary), true))
	for _, p := range posts {
		if s := field(p, "slug"); s == field(sched, "slug") || s == field(draft, "slug") {
			t.Errorf("hidden post %v listed publicly", s)
		}
	}
	c.ok("GET", "/api/v1/posts?tag=go&page=1&limit=1", nil, 200, arr(postSummary), true)
	if s := list(c.ok("GET", "/api/v1/posts?category=life", nil, 200, arr(postSummary), true)); len(s) != 1 {
		t.Errorf("life posts = %d, want 1", len(s))
	}
	c.ok("GET", "/api/v1/posts?category=tech", nil, 200, arr(postSummary), true)
	c.fail("GET", "/api/v1/posts?category=food", nil, 400, "bad_request")
	c.ok("GET", "/api/v1/tags?category=life", nil, 200, arr(tagCount), false)
	c.fail("GET", "/api/v1/tags?category=food", nil, 400, "bad_request")
	c.ok("GET", "/api/admin/posts?category=tech", nil, 200, arr(adminPost), false)
	c.fail("GET", "/api/admin/posts?category=food", nil, 400, "bad_request")
	c.ok("GET", "/api/v1/posts?q=hello", nil, 200, arr(postSummary), true)
	c.fail("GET", "/api/v1/posts?limit=51", nil, 400, "bad_request")
	pubSlug := field(pub, "slug").(string)
	d1 := c.ok("GET", "/api/v1/posts/"+pubSlug, nil, 200, postDetail, false)
	if field(d1, "part_count") != float64(2) || field(d1, "prev") != nil || field(field(d1, "next"), "no") != float64(2) ||
		field(field(d1, "part"), "title") != "Intro" {
		t.Errorf("part 1 detail = %v", d1)
	}
	d2 := c.ok("GET", "/api/v1/posts/"+pubSlug+"?part=2", nil, 200, postDetail, false)
	if field(field(d2, "prev"), "no") != float64(1) || field(d2, "next") != nil || len(list(field(d2, "parts"))) != 2 {
		t.Errorf("part 2 detail = %v", d2)
	}
	c.fail("GET", "/api/v1/posts/"+pubSlug+"?part=3", nil, 404, "not_found")
	c.fail("GET", "/api/v1/posts/"+pubSlug+"?part=x", nil, 404, "not_found")
	c.fail("GET", "/api/v1/posts/"+field(sched, "slug").(string), nil, 404, "not_found")
	c.fail("GET", "/api/v1/posts/"+field(draft, "slug").(string), nil, 404, "not_found")
	tags := list(c.ok("GET", "/api/v1/tags", nil, 200, arr(tagCount), false))
	for _, tg := range tags {
		if field(tg, "tag") == "go" && field(tg, "count") != float64(2) {
			t.Errorf("tag go count = %v, want 2 (scheduled and draft excluded)", field(tg, "count"))
		}
	}

	// The scheduled post appears once its time has come.
	if _, err := c.db.Exec(t.Context(), `UPDATE posts SET published_at = now() - interval '1 minute' WHERE id = $1`,
		field(sched, "id")); err != nil {
		t.Fatal(err)
	}
	c.ok("GET", "/api/v1/posts/"+field(sched, "slug").(string), nil, 200, postDetail, false)
	if got := field(c.ok("GET", "/api/admin/posts/"+idOf(sched), nil, 200, adminPost, false), "state"); got != "published" {
		t.Errorf("state after its time = %v", got)
	}

	c.empty("DELETE", "/api/admin/posts/"+idOf(draft), nil, 204)
	c.fail("GET", "/api/admin/posts/"+idOf(draft), nil, 404, "not_found")
}

func pngBytes() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func (c *harness) upload(name string, content []byte) *httptest.ResponseRecorder {
	c.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	fw, _ := mw.CreateFormFile("file", name)
	_, _ = fw.Write(content)
	_ = mw.Close()
	return c.raw("POST", "/api/admin/media", &b, map[string]string{"Content-Type": mw.FormDataContentType()})
}

func TestContractMedia(t *testing.T) {
	c := newHarness(t)

	// A raster image is queued for WebP conversion (no worker here: 202).
	w := c.upload("cover.png", pngBytes())
	if w.Code != 202 {
		t.Fatalf("upload png = %d %s", w.Code, w.Body)
	}
	img := decodeData(t, w, mediaShape)
	if u, _ := field(img, "url").(string); !strings.HasSuffix(u, ".webp") || field(img, "status") != "processing" ||
		field(img, "content_type") != "image/webp" {
		t.Errorf("queued media = %v", img)
	}
	c.ok("GET", "/api/admin/media/"+idOf(img), nil, 200, mediaShape, false)
	c.fail("GET", "/api/admin/media/999999", nil, 404, "not_found")
	c.fail("POST", "/api/admin/media/"+idOf(img)+"/retry", nil, 409, "conflict")

	// A PDF is stored as is and ready at once.
	w = c.upload("doc.pdf", []byte("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n1 0 obj\n"))
	if w.Code != 201 {
		t.Fatalf("upload pdf = %d %s", w.Code, w.Body)
	}
	m := decodeData(t, w, mediaShape)
	url, _ := field(m, "url").(string)
	if !strings.HasPrefix(url, "/media/") || field(m, "content_type") != "application/pdf" ||
		field(m, "status") != "ready" || field(m, "converted") != false {
		t.Errorf("media = %v", m)
	}
	c.ok("GET", "/api/admin/media?page=1&limit=10", nil, 200, arr(mediaShape), true)

	got := c.raw("GET", url, nil, nil)
	if got.Code != 200 || got.Header().Get("Content-Type") != "application/pdf" ||
		got.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("serve pdf = %d %v", got.Code, got.Header())
	}

	// HTML is rejected however it is named.
	for name, body := range map[string]string{
		"page.html":   "<!DOCTYPE html><html><body><script>alert(1)</script></body></html>",
		"logo.svg":    "<html><svg><script>alert(1)</script></svg></html>",
		"comment.svg": "<!-- x --><svg xmlns=\"http://www.w3.org/2000/svg\"><script>alert(1)</script></svg>",
		"photo.png":   "  <script>alert(1)</script>",
	} {
		c.failRaw(c.upload(name, []byte(body)), "upload "+name, http.StatusUnsupportedMediaType, "unsupported_type")
	}

	// An SVG with a script is either refused or served sandboxed.
	svg := `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><script>alert(2)</script></svg>`
	sw := c.upload("evil.svg", []byte(svg))
	switch sw.Code {
	case http.StatusUnsupportedMediaType:
	case http.StatusCreated:
		u, _ := field(decodeData(t, sw, mediaShape), "url").(string)
		g := c.raw("GET", u, nil, nil)
		if g.Code != 200 || g.Header().Get("Content-Type") != "image/svg+xml" ||
			g.Header().Get("Content-Security-Policy") != "default-src 'none'; style-src 'unsafe-inline'; sandbox" ||
			g.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("svg served = %d %v", g.Code, g.Header())
		}
	default:
		t.Errorf("svg upload = %d %s", sw.Code, sw.Body)
	}

	c.empty("DELETE", "/api/admin/media/"+idOf(m), nil, 204)
	c.fail("DELETE", "/api/admin/media/"+idOf(m), nil, 404, "not_found")
	if g := c.raw("GET", url, nil, nil); g.Code != 404 {
		t.Errorf("deleted file still served: %d", g.Code)
	}
}
