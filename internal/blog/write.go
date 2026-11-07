package blog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/baoopn-2026/internal/markdown"
)

type rendered struct {
	html    string
	toc     string
	minutes int
}

func render(md string) (rendered, error) {
	res, err := markdown.Render(md, markdown.Options{Trusted: true})
	if err != nil {
		return rendered{}, err
	}
	toc, err := json.Marshal(res.TOC)
	if err != nil {
		return rendered{}, err
	}
	return rendered{html: res.HTML, toc: string(toc), minutes: res.ReadingMinutes}, nil
}

// renderedParts holds every part of a validated input, rendered, plus the
// post's total reading time.
type renderedParts struct {
	in      []PartInput
	r       []rendered
	minutes int
}

func renderParts(parts []PartInput) (renderedParts, error) {
	out := renderedParts{in: parts, r: make([]rendered, len(parts))}
	for i, p := range parts {
		r, err := render(p.ContentMD)
		if err != nil {
			return out, fmt.Errorf("part %d: %w", i+1, err)
		}
		out.r[i] = r
		out.minutes += r.minutes
	}
	return out, nil
}

// replaceParts deletes the post's parts and inserts rp as parts 1..n.
func replaceParts(ctx context.Context, tx pgx.Tx, postID int64, rp renderedParts) error {
	if _, err := tx.Exec(ctx, `DELETE FROM post_parts WHERE post_id = $1`, postID); err != nil {
		return err
	}
	n := len(rp.in)
	nos, mins := make([]int32, n), make([]int32, n)
	titles, mds, htmls, tocs := make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	for i, p := range rp.in {
		nos[i], mins[i] = int32(i+1), int32(rp.r[i].minutes)
		titles[i], mds[i], htmls[i], tocs[i] = p.Title, p.ContentMD, rp.r[i].html, rp.r[i].toc
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO post_parts (post_id, part_no, title, content_md, content_html, toc, reading_minutes)
		SELECT $1, u.no, u.title, u.md, u.html, u.toc::jsonb, u.minutes
		FROM unnest($2::int[], $3::text[], $4::text[], $5::text[], $6::text[], $7::int[])
			AS u(no, title, md, html, toc, minutes)`,
		postID, nos, titles, mds, htmls, tocs, mins)
	return err
}

// insertPost inserts the post row and its parts in tx. It reports false when
// the slug is taken.
func insertPost(ctx context.Context, tx pgx.Tx, slug string, in PostInput, rp renderedParts) (int64, bool, error) {
	var id int64
	err := tx.QueryRow(ctx, `
		INSERT INTO posts (slug, title, excerpt, cover_url, tags, status, published_at, reading_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (slug) DO NOTHING RETURNING id`,
		slug, in.Title, in.Excerpt, in.CoverURL, in.Tags, in.Status, in.PublishedAt, rp.minutes).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, replaceParts(ctx, tx, id, rp)
}

// create validates and inserts a post with its parts in one transaction,
// suffixing a generated slug on collision.
func (m *Module) create(ctx context.Context, in PostInput) (AdminPost, error) {
	generated, err := validate(&in, m.now())
	if err != nil {
		return AdminPost{}, err
	}
	rp, err := renderParts(in.Parts)
	if err != nil {
		return AdminPost{}, err
	}
	var id int64
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		for n := 1; n <= 100; n++ {
			newID, ok, err := insertPost(ctx, tx, suffixed(in.Slug, n), in, rp)
			if err != nil {
				return err
			}
			if ok {
				id = newID
				return nil
			}
			if !generated {
				break
			}
		}
		return ErrSlugTaken
	})
	if err != nil {
		return AdminPost{}, err
	}
	return m.getAdmin(ctx, m.db, id)
}

// update replaces every input field and all parts in one transaction. An
// empty slug keeps the current one.
func (m *Module) update(ctx context.Context, id int64, in PostInput) (AdminPost, error) {
	if strings.TrimSpace(in.Slug) == "" {
		var cur string
		err := m.db.QueryRow(ctx, `SELECT slug FROM posts WHERE id = $1`, id).Scan(&cur)
		if errors.Is(err, pgx.ErrNoRows) {
			return AdminPost{}, ErrNotFound
		}
		if err != nil {
			return AdminPost{}, err
		}
		in.Slug = cur
	}
	if _, err := validate(&in, m.now()); err != nil {
		return AdminPost{}, err
	}
	rp, err := renderParts(in.Parts)
	if err != nil {
		return AdminPost{}, err
	}
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE posts SET slug = $2, title = $3, excerpt = $4, cover_url = $5, tags = $6, status = $7,
				published_at = $8, reading_minutes = $9, updated_at = now()
			WHERE id = $1`,
			id, in.Slug, in.Title, in.Excerpt, in.CoverURL, in.Tags, in.Status, in.PublishedAt, rp.minutes)
		if isUnique(err, "posts_slug_key") {
			return ErrSlugTaken
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return replaceParts(ctx, tx, id, rp)
	})
	if err != nil {
		return AdminPost{}, err
	}
	return m.getAdmin(ctx, m.db, id)
}

func (m *Module) delete(ctx context.Context, id int64) (bool, error) {
	tag, err := m.db.Exec(ctx, `DELETE FROM posts WHERE id = $1`, id)
	return tag.RowsAffected() > 0, err
}

func isUnique(err error, constraint string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505" && (constraint == "" || pe.ConstraintName == constraint)
}
