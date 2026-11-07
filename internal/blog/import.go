package blog

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Import inserts posts with their parts in one transaction. A post whose
// slug already exists is skipped, so running the same import twice is a
// no-op. It returns the slugs it inserted.
func (m *Module) Import(ctx context.Context, posts []PostInput) (inserted []string, err error) {
	type prepared struct {
		in PostInput
		rp renderedParts
	}
	batch := make([]prepared, 0, len(posts))
	for _, in := range posts {
		if _, err := validate(&in, m.now()); err != nil {
			return nil, fmt.Errorf("blog: import %q: %w", in.Slug, err)
		}
		rp, err := renderParts(in.Parts)
		if err != nil {
			return nil, fmt.Errorf("blog: import %q: render: %w", in.Slug, err)
		}
		batch = append(batch, prepared{in: in, rp: rp})
	}

	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		for _, p := range batch {
			_, ok, err := insertPost(ctx, tx, p.in.Slug, p.in, p.rp)
			if err != nil {
				return fmt.Errorf("blog: import %q: %w", p.in.Slug, err)
			}
			if ok {
				inserted = append(inserted, p.in.Slug)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return inserted, nil
}
