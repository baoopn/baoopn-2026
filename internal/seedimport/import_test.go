package seedimport

import (
	"context"
	"io/fs"
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

func TestRewriteScreenshots(t *testing.T) {
	md := "Intro\r\n\r\n![Dash](screenshot:dash \"Title\")\r\n" +
		"Inline ![a](screenshot:gone) text and ![b](screenshot:dash).\n" +
		"  ![only](screenshot:gone)  \n" +
		"![ext](https://cdn.example.com/c.png) ![local](/media/x.png)\n" +
		"End"
	got := rewriteScreenshots(md, func(id string) (string, bool) {
		if id == "dash" {
			return "/media/abc.png", true
		}
		return "", false
	})
	want := "Intro\n\n![Dash](/media/abc.png \"Title\")\n" +
		"Inline  text and ![b](/media/abc.png).\n" +
		"![ext](https://cdn.example.com/c.png) ![local](/media/x.png)\n" +
		"End"
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR placeholder")

func fixture() fstest.MapFS {
	return fstest.MapFS{
		"posts/README.md": {Data: []byte("# Seed posts")},
		"posts/two-parts/post.json": {Data: []byte(`{"slug":"two-parts","title":"Two Parts","excerpt":"",
			"tags":["Go","Testing"],"parts":[{"title":"Setup","file":"part-1.md"},{"title":"","file":"part-2.md"}],
			"screenshots":[{"id":"dash"},{"id":"named","file":"img/other-name.png"},{"id":"gone"}]}`)},
		"posts/two-parts/part-1.md": {Data: []byte("First paragraph.\n\n![Dashboard](screenshot:dash)\n\n" +
			"![Named](screenshot:named)\n\n![Missing](screenshot:gone)\n\nAfter.")},
		"posts/two-parts/part-2.md":          {Data: []byte("## Second\n\n![Again](screenshot:dash)")},
		"posts/two-parts/img/dash.png":       {Data: png},
		"posts/two-parts/img/other-name.png": {Data: append([]byte{}, append(png, '2')...)},
		// Skipped with a warning: a part file is missing.
		"posts/broken/post.json": {Data: []byte(`{"title":"Broken","parts":[{"title":"x","file":"part-1.md"}]}`)},
		// Skipped with a warning: a part path leaves the folder.
		"posts/escape/post.json": {Data: []byte(`{"title":"Escape","parts":[{"file":"../two-parts/part-1.md"}]}`)},
	}
}

func TestRunImportsSeedPostsOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	log := slog.New(slog.DiscardHandler)
	store, err := media.New(media.Deps{DB: pool, Log: log, Dir: t.TempDir(), MaxBytes: 10 << 20})
	if err != nil {
		t.Fatal(err)
	}
	b := blog.New(blog.Deps{DB: pool, Log: log})
	fsys := fixture()
	d := Deps{DB: pool, Log: log, Media: store, Blog: b, FS: fsys}
	if err := Run(ctx, d); err != nil {
		t.Fatal(err)
	}

	var posts, mediaRows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM posts`).Scan(&posts)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&mediaRows)
	if posts != 1 || mediaRows != 2 {
		t.Fatalf("posts %d media %d", posts, mediaRows)
	}
	var status, excerpt string
	var tags []string
	var published *string
	err = pool.QueryRow(ctx, `SELECT status, excerpt, tags, published_at::text FROM posts WHERE slug = 'two-parts'`).
		Scan(&status, &excerpt, &tags, &published)
	if err != nil {
		t.Fatal(err)
	}
	if status != "draft" || published != nil || excerpt != "First paragraph." || strings.Join(tags, ",") != "go,testing" {
		t.Fatalf("post %s %v %q %v", status, published, excerpt, tags)
	}
	rows, err := pool.Query(ctx, `SELECT pp.part_no, pp.title, pp.content_md FROM post_parts pp
		JOIN posts p ON p.id = pp.post_id WHERE p.slug = 'two-parts' ORDER BY pp.part_no`)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	var titles []string
	for rows.Next() {
		var no int
		var title, md string
		if err := rows.Scan(&no, &title, &md); err != nil {
			t.Fatal(err)
		}
		parts, titles = append(parts, md), append(titles, title)
	}
	if len(parts) != 2 || titles[0] != "Setup" || titles[1] != "" {
		t.Fatalf("parts %v %v", titles, parts)
	}
	mediaURL := regexp.MustCompile(`!\[Dashboard\]\((/media/[0-9a-f]{24}\.png)\)`)
	m := mediaURL.FindStringSubmatch(parts[0])
	if m == nil || strings.Contains(parts[0], "screenshot:") || strings.Contains(parts[0], "Missing") ||
		!regexp.MustCompile(`!\[Named\]\(/media/[0-9a-f]{24}\.png\)`).MatchString(parts[0]) ||
		!strings.Contains(parts[0], "After.") {
		t.Fatalf("part 1 not rewritten:\n%s", parts[0])
	}
	if !strings.Contains(parts[1], "![Again]("+m[1]+")") {
		t.Fatalf("part 2 not rewritten:\n%s", parts[1])
	}

	var done []string
	if _, err := settings.Get(ctx, pool, FlagKey, &done); err != nil || strings.Join(done, ",") != "two-parts" {
		t.Fatalf("flag %v %v", done, err)
	}

	// Re-running is a no-op; a deleted post is not imported again; a folder
	// added later is imported; an existing slug is skipped and recorded.
	if err := Run(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM posts WHERE slug = 'two-parts'`); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Import(ctx, []blog.PostInput{{Slug: "taken", Title: "Mine", Status: "published",
		Parts: []blog.PartInput{{ContentMD: "keep me"}}}}); err != nil {
		t.Fatal(err)
	}
	fsys["posts/later/post.json"] = &fstest.MapFile{Data: []byte(`{"title":"Later","parts":[{"title":"Only"}]}`)}
	fsys["posts/later/part-1.md"] = &fstest.MapFile{Data: []byte("Later text")}
	fsys["posts/taken/post.json"] = &fstest.MapFile{Data: []byte(`{"title":"Seed","parts":[{"file":"part-1.md"}]}`)}
	fsys["posts/taken/part-1.md"] = &fstest.MapFile{Data: []byte("seed text")}
	if err := Run(ctx, d); err != nil {
		t.Fatal(err)
	}
	var slugs []string
	_ = pool.QueryRow(ctx, `SELECT array_agg(slug ORDER BY slug) FROM posts`).Scan(&slugs)
	if strings.Join(slugs, ",") != "later,taken" {
		t.Fatalf("slugs after re-run %v", slugs)
	}
	var kept string
	_ = pool.QueryRow(ctx, `SELECT pp.content_md FROM post_parts pp JOIN posts p ON p.id = pp.post_id
		WHERE p.slug = 'taken'`).Scan(&kept)
	if kept != "keep me" {
		t.Fatalf("existing post overwritten: %q", kept)
	}
	done = nil
	_, _ = settings.Get(ctx, pool, FlagKey, &done)
	if strings.Join(done, ",") != "two-parts,later,taken" {
		t.Fatalf("flag after re-run %v", done)
	}
}

func TestRunEmbeddedSeedAndProfile(t *testing.T) {
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
	d := Deps{DB: pool, Log: log, Media: store, Blog: blog.New(blog.Deps{DB: pool, Log: log}), FS: seed.FS}
	for range 2 {
		if err := Run(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	p, err := prof.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.AvatarURL, "/media/") || p.CoverURL != "https://cdn.baoopn.com/data/img/Bao_cover.jpg" {
		t.Fatalf("profile images %q %q", p.AvatarURL, p.CoverURL)
	}
	// One row for the avatar plus one per screenshot shipped in seed/posts,
	// and running the import twice must not add more.
	shots, err := fs.Glob(seed.FS, "posts/*/img/*")
	if err != nil {
		t.Fatal(err)
	}
	var mediaRows int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&mediaRows)
	if want := 1 + len(shots); mediaRows != want {
		t.Fatalf("media rows %d, want %d (avatar + %d screenshots)", mediaRows, want, len(shots))
	}
}
