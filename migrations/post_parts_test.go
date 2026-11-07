package migrations_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/migrations"
)

func provider(t *testing.T, pool *pgxpool.Pool) *goose.Provider {
	t.Helper()
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = sqlDB.Close() })
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPostPartsMigration converts one-page posts into posts with part 1,
// drops the imported v3 posts, and the Down joins parts back.
func TestPostPartsMigration(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := context.Background()
	p := provider(t, pool)
	if _, err := p.UpTo(ctx, 2); err != nil {
		t.Fatal(err)
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO posts (slug, title, content_md, content_html, toc, series, series_order, status,
			published_at, reading_minutes, legacy_path)
		VALUES ('mine', 'Mine', '## Hi', '<h2 id="hi">Hi</h2>', '[{"id":"hi","text":"Hi","level":2}]', '', 0,
				'published', now(), 4, NULL),
			('draft', 'Draft', 'draft body', '<p>draft body</p>', '[]', 'my-series', 2, 'draft', NULL, 1, NULL),
			('old-v3', 'Old', 'old', '<p>old</p>', '[]', 'portfolio-app', 1, 'published', now(), 2,
				'/blog/portfolio-app/1')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}

	var slugs []string
	if err := pool.QueryRow(ctx, `SELECT array_agg(slug ORDER BY slug) FROM posts`).Scan(&slugs); err != nil {
		t.Fatal(err)
	}
	if len(slugs) != 2 || slugs[0] != "draft" || slugs[1] != "mine" {
		t.Fatalf("posts after up: %v", slugs)
	}
	var no, minutes, total int
	var title, md, html, toc string
	err = pool.QueryRow(ctx, `SELECT pp.part_no, pp.title, pp.content_md, pp.content_html, pp.toc::text,
			pp.reading_minutes, p.reading_minutes
		FROM post_parts pp JOIN posts p ON p.id = pp.post_id WHERE p.slug = 'mine'`).
		Scan(&no, &title, &md, &html, &toc, &minutes, &total)
	if err != nil {
		t.Fatal(err)
	}
	if no != 1 || title != "" || md != "## Hi" || html != `<h2 id="hi">Hi</h2>` || minutes != 4 || total != 4 ||
		toc != `[{"id": "hi", "text": "Hi", "level": 2}]` {
		t.Fatalf("part: %d %q %q %q %s %d %d", no, title, md, html, toc, minutes, total)
	}
	var parts, cols int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM post_parts`).Scan(&parts)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema = current_schema()
		AND table_name = 'posts' AND column_name IN ('content_md', 'content_html', 'toc', 'series', 'series_title',
		'series_order', 'legacy_path')`).Scan(&cols)
	if parts != 2 || cols != 0 {
		t.Fatalf("parts %d, old columns left %d", parts, cols)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO post_parts (post_id, part_no) SELECT id, 21 FROM posts LIMIT 1`); err == nil {
		t.Fatal("part_no 21 accepted")
	}

	// Down: a second part is joined back under its title.
	_, err = pool.Exec(ctx, `INSERT INTO post_parts (post_id, part_no, title, content_md, content_html, toc)
		SELECT id, 2, 'Second', 'more', '<p>more</p>', '[{"id":"m","text":"M","level":2}]' FROM posts WHERE slug = 'mine'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.DownTo(ctx, 2); err != nil {
		t.Fatal(err)
	}
	err = pool.QueryRow(ctx, `SELECT content_md, content_html, jsonb_array_length(toc) FROM posts WHERE slug = 'mine'`).
		Scan(&md, &html, &no)
	if err != nil {
		t.Fatal(err)
	}
	if md != "## Hi\n\n## Second\n\nmore" || html != "<h2 id=\"hi\">Hi</h2>\n<p>more</p>" || no != 2 {
		t.Fatalf("down: %q %q %d", md, html, no)
	}
	if _, err := p.Up(ctx); err != nil {
		t.Fatalf("re-up: %v", err)
	}
}
