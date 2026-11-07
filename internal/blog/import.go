package blog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Import inserts posts in one transaction. A post whose slug or legacy path
// already exists is skipped, so running the same import twice is a no-op.
func (m *Module) Import(ctx context.Context, posts []ImportPost) (inserted int, err error) {
	type prepared struct {
		in     PostInput
		legacy *string
		r      rendered
	}
	batch := make([]prepared, 0, len(posts))
	for _, p := range posts {
		in := p.PostInput
		if _, err := validate(&in, m.now()); err != nil {
			return 0, fmt.Errorf("blog: import %q: %w", p.Slug, err)
		}
		r, err := render(in.ContentMD)
		if err != nil {
			return 0, fmt.Errorf("blog: import %q: render: %w", in.Slug, err)
		}
		var legacy *string
		if lp := normalizeLegacy(p.LegacyPath); lp != "" {
			legacy = &lp
		}
		batch = append(batch, prepared{in: in, legacy: legacy, r: r})
	}

	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		for _, p := range batch {
			in := p.in
			tag, err := tx.Exec(ctx, `
				INSERT INTO posts (slug, title, excerpt, content_md, content_html, toc, cover_url, tags,
					series, series_title, series_order, status, published_at, reading_minutes, legacy_path)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
				ON CONFLICT DO NOTHING`,
				in.Slug, in.Title, in.Excerpt, in.ContentMD, p.r.html, p.r.toc, in.CoverURL, in.Tags,
				in.Series, in.SeriesTitle, in.SeriesOrder, in.Status, in.PublishedAt, p.r.minutes, p.legacy)
			if err != nil {
				return fmt.Errorf("blog: import %q: %w", in.Slug, err)
			}
			inserted += int(tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return inserted, nil
}
