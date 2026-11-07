package seedimport

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/baoopn/baoopn-2026/internal/blog"
	"github.com/baoopn/baoopn-2026/internal/media"
	"github.com/baoopn/baoopn-2026/internal/profile"
	"github.com/baoopn/baoopn-2026/internal/settings"
	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/seed"
)

func TestStripPartHeading(t *testing.T) {
	cases := map[string]string{
		"## Part 1: Overview\n\n### Intro\ntext": "### Intro\ntext",
		"# Part 4: Integrating\r\n\r\nBody":      "Body",
		"##Part 12: X\nBody":                     "Body",
		"### Part 1: Too deep\nBody":             "### Part 1: Too deep\nBody",
		"## Intro\n## Part 2: later":             "## Intro\n## Part 2: later",
	}
	for in, want := range cases {
		if got := stripPartHeading(in); got != want {
			t.Errorf("stripPartHeading(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRewriteImages(t *testing.T) {
	md := `![a](/thumbs/a.png)
![b](/blog-img/x/b.jpg "Title")
![ext](https://cdn.example.com/c.png)
<img class="w" src="/thumbs/a.png" alt="again">
<img src="/missing.png">
` + "```jsx\n<img src={track.cover} />\n```"
	resolve := func(p string) (string, bool) {
		if p == "/missing.png" {
			return "", false
		}
		return "/media/" + strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", "_"), true
	}
	got := rewriteImages(md, resolve)
	for _, want := range []string{
		`![a](/media/thumbs_a.png)`,
		`![b](/media/blog-img_x_b.jpg "Title")`,
		`![ext](https://cdn.example.com/c.png)`,
		`<img class="w" src="/media/thumbs_a.png" alt="again">`,
		`<img src="/missing.png">`,
		`<img src={track.cover} />`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if p := localPaths(md); strings.Join(p, ",") != "/thumbs/a.png,/blog-img/x/b.jpg,/missing.png" {
		t.Errorf("localPaths %v", p)
	}
}

func TestSlugsLegacyAndOrdering(t *testing.T) {
	if s := postSlug("portfolio-app", "project-overview-tech-stack"); s != "portfolio-app-project-overview-tech-stack" {
		t.Fatalf("slug %s", s)
	}
	if p := legacyPath("product-catalog-api", 7); p != "/blog/product-catalog-api/7" {
		t.Fatalf("legacy %s", p)
	}
	a, err := publishedAt("2025-03-05", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := publishedAt("2025-03-05", 2)
	if a.Format("2006-01-02T15:04:05Z07:00") != "2025-03-05T12:01:00Z" || !b.After(a) {
		t.Fatalf("published_at %v %v", a, b)
	}
	if _, err := publishedAt("March 5", 1); err == nil {
		t.Fatal("bad date accepted")
	}
}

func TestRunImportsSeedOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	store, err := media.New(media.Deps{DB: pool, Log: log, Dir: t.TempDir(), MaxBytes: 10 << 20})
	if err != nil {
		t.Fatal(err)
	}
	prof := profile.New(profile.Deps{DB: pool, Log: log, Seed: seed.FS})
	if err := prof.EnsureSeeded(ctx); err != nil {
		t.Fatal(err)
	}
	b := blog.New(blog.Deps{DB: pool, Log: log})
	d := Deps{DB: pool, Log: log, Media: store, Blog: b, FS: seed.FS}
	if err := Run(ctx, d); err != nil {
		t.Fatal(err)
	}

	var posts, legacy int
	var mediaRows int
	_ = pool.QueryRow(ctx, `SELECT count(*), count(legacy_path) FROM posts`).Scan(&posts, &legacy)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&mediaRows)
	if posts != 11 || legacy != 11 {
		t.Fatalf("posts %d legacy %d", posts, legacy)
	}
	// 3 thumbs + 4 blog images + avatar.
	if mediaRows != 8 {
		t.Fatalf("media rows %d", mediaRows)
	}

	var content, cover, title string
	var order int
	err = pool.QueryRow(ctx, `SELECT content_md, cover_url, title, series_order FROM posts
		WHERE legacy_path = '/blog/portfolio-app/1'`).Scan(&content, &cover, &title, &order)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(content, "## Part") || strings.Contains(content, "](/thumbs/") || strings.Contains(content, "](/blog-img/") {
		t.Fatalf("content not rewritten:\n%.400s", content)
	}
	if !regexp.MustCompile(`^/media/[0-9a-f]{24}\.png$`).MatchString(cover) || title != "Project Overview and Tech Stack" || order != 1 {
		t.Fatalf("cover %q title %q order %d", cover, title, order)
	}
	var tags []string
	_ = pool.QueryRow(ctx, `SELECT tags FROM posts WHERE slug = 'product-catalog-api-caching-strategy'`).Scan(&tags)
	if strings.Join(tags, ",") != "express,nodejs,mongodb,redis" {
		t.Fatalf("tags %v", tags)
	}

	p, err := prof.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.AvatarURL, "/media/") || p.CoverURL != "https://cdn.baoopn.com/data/img/Bao_cover.jpg" {
		t.Fatalf("profile images %q %q", p.AvatarURL, p.CoverURL)
	}

	if ok, _ := settings.Flag(ctx, pool, FlagKey); !ok {
		t.Fatal("flag not set")
	}
	// Second run is a no-op (flag); even without the flag, Import skips existing rows.
	if err := Run(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM settings WHERE key = $1`, FlagKey); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, d); err != nil {
		t.Fatal(err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM posts`).Scan(&posts)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&mediaRows)
	if posts != 11 || mediaRows != 8 {
		t.Fatalf("re-run duplicated: posts %d media %d", posts, mediaRows)
	}
}

func TestRunMissingAssetLeftAsIs(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	store, _ := media.New(media.Deps{DB: pool, Log: log, Dir: t.TempDir()})
	fsys := fstest.MapFS{
		"blog/s/metadata.json": {Data: []byte(`{"name":"S","slug":"s","date":"2025-01-01","pages":[{"title":"One","slug":"one","no":1}]}`)},
		"blog/s/page-1.md":     {Data: []byte("## Part 1: One\n\n![x](/nope.png)\n\n![up](/../../etc/passwd)")},
	}
	if err := Run(ctx, Deps{DB: pool, Log: log, Media: store, Blog: blog.New(blog.Deps{DB: pool, Log: log}), FS: fsys}); err != nil {
		t.Fatal(err)
	}
	var content string
	if err := pool.QueryRow(ctx, `SELECT content_md FROM posts WHERE slug = 's-one'`).Scan(&content); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "![x](/nope.png)") || strings.HasPrefix(content, "## Part") {
		t.Fatalf("content %q", content)
	}
}
