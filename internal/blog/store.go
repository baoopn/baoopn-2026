package blog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/baoopn-2026/internal/markdown"
)

const (
	visible   = `p.status = 'published' AND p.published_at <= now()`
	viewsJoin = `LEFT JOIN LATERAL (SELECT coalesce(sum(views), 0) AS views
		FROM page_view_stats WHERE path = '/blog/' || p.slug) v ON true`
	summaryCols = `p.slug, p.title, p.excerpt, p.cover_url, p.tags, p.series, p.series_title,
		p.series_order, p.published_at, p.updated_at, p.reading_minutes, v.views`
	adminCols = `p.id, p.slug, p.title, p.excerpt, p.content_md, p.cover_url, p.tags, p.series,
		p.series_title, p.series_order, p.status, p.published_at, p.reading_minutes, v.views,
		p.legacy_path, p.created_at, p.updated_at`
)

type scanner interface{ Scan(dest ...any) error }

func scanSummary(row scanner, extra ...any) (PostSummary, error) {
	var s PostSummary
	dest := append([]any{&s.Slug, &s.Title, &s.Excerpt, &s.CoverURL, &s.Tags, &s.Series, &s.SeriesTitle,
		&s.SeriesOrder, &s.PublishedAt, &s.UpdatedAt, &s.ReadingMinutes, &s.Views}, extra...)
	if err := row.Scan(dest...); err != nil {
		return s, err
	}
	s.PublishedAt, s.UpdatedAt = s.PublishedAt.UTC(), s.UpdatedAt.UTC()
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return s, nil
}

func (m *Module) scanAdmin(row scanner) (AdminPost, error) {
	var p AdminPost
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Excerpt, &p.ContentMD, &p.CoverURL, &p.Tags, &p.Series,
		&p.SeriesTitle, &p.SeriesOrder, &p.Status, &p.PublishedAt, &p.ReadingMinutes, &p.Views,
		&p.LegacyPath, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	if p.Tags == nil {
		p.Tags = []string{}
	}
	if p.PublishedAt != nil {
		t := p.PublishedAt.UTC()
		p.PublishedAt = &t
	}
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	p.State = stateOf(p.Status, p.PublishedAt, m.now())
	return p, nil
}

type publicFilter struct {
	Tag, Series, Q string
	Page, Limit    int
}

func (m *Module) listPublic(ctx context.Context, f publicFilter) ([]PostSummary, int64, error) {
	where := []string{visible}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "$?", "$"+strconv.Itoa(len(args))))
	}
	if f.Tag != "" {
		add(`$? = ANY(p.tags)`, f.Tag)
	}
	if f.Series != "" {
		add(`p.series = $?`, f.Series)
	}
	if f.Q != "" {
		add(`(p.title ILIKE $? ESCAPE '\' OR p.excerpt ILIKE $? ESCAPE '\' OR p.content_md ILIKE $? ESCAPE '\')`,
			"%"+escapeLike(f.Q)+"%")
	}
	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	q := fmt.Sprintf(`SELECT %s, count(*) OVER () FROM posts p %s WHERE %s
		ORDER BY p.published_at DESC, p.id DESC LIMIT $%d OFFSET $%d`,
		summaryCols, viewsJoin, strings.Join(where, " AND "), len(args)-1, len(args))
	rows, err := m.db.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []PostSummary{}
	var total int64
	for rows.Next() {
		s, err := scanSummary(rows, &total)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if len(out) == 0 && f.Page > 1 {
		cq := fmt.Sprintf(`SELECT count(*) FROM posts p WHERE %s`, strings.Join(where, " AND "))
		if err := m.db.QueryRow(ctx, cq, args[:len(args)-2]...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	return out, total, nil
}

func (m *Module) detail(ctx context.Context, slug string) (PostDetail, error) {
	var d PostDetail
	var toc []byte
	row := m.db.QueryRow(ctx, `SELECT `+summaryCols+`, p.id, p.content_html, p.toc
		FROM posts p `+viewsJoin+` WHERE p.slug = $1 AND `+visible, slug)
	s, err := scanSummary(row, &d.id, &d.ContentHTML, &toc)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.PostSummary = s
	d.TOC = json.RawMessage(toc)
	if len(d.TOC) == 0 || string(d.TOC) == "null" {
		d.TOC = json.RawMessage("[]")
	}
	d.SeriesPosts = []PostLink{}

	if s.Series != "" {
		rows, err := m.db.Query(ctx, `SELECT p.slug, p.title, p.series_order FROM posts p
			WHERE p.series = $1 AND `+visible+` ORDER BY p.series_order, p.published_at, p.id`, s.Series)
		if err != nil {
			return d, err
		}
		links, err := pgx.CollectRows(rows, scanLink)
		if err != nil {
			return d, err
		}
		d.SeriesPosts = links
		for i, l := range links {
			if l.Slug != s.Slug {
				continue
			}
			if i > 0 {
				d.Prev = &links[i-1]
			}
			if i < len(links)-1 {
				d.Next = &links[i+1]
			}
		}
		return d, nil
	}

	if d.Prev, err = m.neighbour(ctx, `(p.published_at, p.id) < ($1, $2)`, `DESC`, s.PublishedAt, d.id); err != nil {
		return d, err
	}
	if d.Next, err = m.neighbour(ctx, `(p.published_at, p.id) > ($1, $2)`, `ASC`, s.PublishedAt, d.id); err != nil {
		return d, err
	}
	return d, nil
}

func scanLink(row pgx.CollectableRow) (PostLink, error) {
	var l PostLink
	err := row.Scan(&l.Slug, &l.Title, &l.SeriesOrder)
	return l, err
}

func (m *Module) neighbour(ctx context.Context, cond, dir string, at any, id int64) (*PostLink, error) {
	var l PostLink
	err := m.db.QueryRow(ctx, `SELECT p.slug, p.title, p.series_order FROM posts p
		WHERE `+visible+` AND `+cond+` ORDER BY p.published_at `+dir+`, p.id `+dir+` LIMIT 1`, at, id).
		Scan(&l.Slug, &l.Title, &l.SeriesOrder)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (m *Module) legacySlug(ctx context.Context, path string) (string, error) {
	var slug string
	err := m.db.QueryRow(ctx, `SELECT p.slug FROM posts p WHERE p.legacy_path = $1 AND `+visible, path).Scan(&slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return slug, err
}

func (m *Module) tags(ctx context.Context) ([]TagCount, error) {
	rows, err := m.db.Query(ctx, `SELECT t, count(*) FROM posts p, unnest(p.tags) t
		WHERE `+visible+` GROUP BY t ORDER BY 2 DESC, 1`)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TagCount, error) {
		var t TagCount
		err := r.Scan(&t.Tag, &t.Count)
		return t, err
	})
	if out == nil {
		out = []TagCount{}
	}
	return out, err
}

var stateSQL = map[string]string{
	"":          `true`,
	"draft":     `p.status = 'draft'`,
	"scheduled": `p.status = 'published' AND p.published_at > now()`,
	"published": visible,
}

func (m *Module) listAdmin(ctx context.Context, state, q string) ([]AdminPost, error) {
	where := stateSQL[state]
	var args []any
	if q != "" {
		args = append(args, "%"+escapeLike(q)+"%")
		where += ` AND (p.title ILIKE $1 ESCAPE '\' OR p.slug ILIKE $1 ESCAPE '\' OR p.excerpt ILIKE $1 ESCAPE '\')`
	}
	rows, err := m.db.Query(ctx, `SELECT `+adminCols+` FROM posts p `+viewsJoin+
		` WHERE `+where+` ORDER BY p.updated_at DESC, p.id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AdminPost{}
	for rows.Next() {
		p, err := m.scanAdmin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (m *Module) getAdmin(ctx context.Context, q pgxQuerier, id int64) (AdminPost, error) {
	p, err := m.scanAdmin(q.QueryRow(ctx, `SELECT `+adminCols+` FROM posts p `+viewsJoin+` WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

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

// create validates and inserts a post, suffixing a generated slug on collision.
func (m *Module) create(ctx context.Context, in PostInput) (AdminPost, error) {
	generated, err := validate(&in, m.now())
	if err != nil {
		return AdminPost{}, err
	}
	r, err := render(in.ContentMD)
	if err != nil {
		return AdminPost{}, err
	}
	base := in.Slug
	for n := 1; n <= 100; n++ {
		slug := suffixed(base, n)
		var id int64
		err := m.db.QueryRow(ctx, `
			INSERT INTO posts (slug, title, excerpt, content_md, content_html, toc, cover_url, tags,
				series, series_title, series_order, status, published_at, reading_minutes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (slug) DO NOTHING RETURNING id`,
			slug, in.Title, in.Excerpt, in.ContentMD, r.html, r.toc, in.CoverURL, in.Tags,
			in.Series, in.SeriesTitle, in.SeriesOrder, in.Status, in.PublishedAt, r.minutes).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			if !generated {
				return AdminPost{}, ErrSlugTaken
			}
			continue
		}
		if err != nil {
			return AdminPost{}, err
		}
		return m.getAdmin(ctx, m.db, id)
	}
	return AdminPost{}, ErrSlugTaken
}

// update replaces every input field. An empty slug keeps the current one.
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
	r, err := render(in.ContentMD)
	if err != nil {
		return AdminPost{}, err
	}
	tag, err := m.db.Exec(ctx, `
		UPDATE posts SET slug = $2, title = $3, excerpt = $4, content_md = $5, content_html = $6, toc = $7,
			cover_url = $8, tags = $9, series = $10, series_title = $11, series_order = $12, status = $13,
			published_at = $14, reading_minutes = $15, updated_at = now()
		WHERE id = $1`,
		id, in.Slug, in.Title, in.Excerpt, in.ContentMD, r.html, r.toc, in.CoverURL, in.Tags,
		in.Series, in.SeriesTitle, in.SeriesOrder, in.Status, in.PublishedAt, r.minutes)
	if isUnique(err, "posts_slug_key") {
		return AdminPost{}, ErrSlugTaken
	}
	if err != nil {
		return AdminPost{}, err
	}
	if tag.RowsAffected() == 0 {
		return AdminPost{}, ErrNotFound
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
