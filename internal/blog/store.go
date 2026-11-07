package blog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	visible = `p.status = 'published' AND p.published_at <= now()`
	// viewsJoin sums the views of /blog/<slug> and every /blog/<slug>/<n>.
	viewsJoin = `LEFT JOIN LATERAL (SELECT coalesce(sum(s.views), 0) AS views
		FROM page_view_stats s WHERE s.path = ANY (ARRAY(
			SELECT '/blog/' || p.slug || CASE WHEN n = 0 THEN '' ELSE '/' || n END
			FROM generate_series(0, 20) n))) v ON true`
	partsJoin   = `LEFT JOIN LATERAL (SELECT count(*)::int AS n FROM post_parts pp WHERE pp.post_id = p.id) pc ON true`
	summaryCols = `p.slug, p.title, p.excerpt, p.cover_url, p.tags, pc.n, p.published_at, p.updated_at,
		p.reading_minutes, v.views`
	adminCols = `p.id, p.slug, p.title, p.excerpt, p.cover_url, p.tags, p.status, p.published_at,
		p.reading_minutes, v.views, p.created_at, p.updated_at`
)

type scanner interface{ Scan(dest ...any) error }

func scanSummary(row scanner, extra ...any) (PostSummary, error) {
	var s PostSummary
	dest := append([]any{&s.Slug, &s.Title, &s.Excerpt, &s.CoverURL, &s.Tags, &s.PartCount,
		&s.PublishedAt, &s.UpdatedAt, &s.ReadingMinutes, &s.Views}, extra...)
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
	err := row.Scan(&p.ID, &p.Slug, &p.Title, &p.Excerpt, &p.CoverURL, &p.Tags, &p.Status, &p.PublishedAt,
		&p.ReadingMinutes, &p.Views, &p.CreatedAt, &p.UpdatedAt)
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
	p.Parts = []AdminPostPart{}
	return p, nil
}

type publicFilter struct {
	Tag, Q      string
	Page, Limit int
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
	if f.Q != "" {
		add(`(p.title ILIKE $? ESCAPE '\' OR p.excerpt ILIKE $? ESCAPE '\' OR EXISTS (
			SELECT 1 FROM post_parts q WHERE q.post_id = p.id AND q.content_md ILIKE $? ESCAPE '\'))`,
			"%"+escapeLike(f.Q)+"%")
	}
	args = append(args, f.Limit, (f.Page-1)*f.Limit)
	q := fmt.Sprintf(`SELECT %s, count(*) OVER () FROM posts p %s %s WHERE %s
		ORDER BY p.published_at DESC, p.id DESC LIMIT $%d OFFSET $%d`,
		summaryCols, partsJoin, viewsJoin, strings.Join(where, " AND "), len(args)-1, len(args))
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

// detail loads a visible post with part no (1-based). ErrNotFound covers a
// missing post and a part out of range.
func (m *Module) detail(ctx context.Context, slug string, no int) (PostDetail, error) {
	var d PostDetail
	var id int64
	row := m.db.QueryRow(ctx, `SELECT `+summaryCols+`, p.id FROM posts p `+partsJoin+` `+viewsJoin+
		` WHERE p.slug = $1 AND `+visible, slug)
	s, err := scanSummary(row, &id)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.PostSummary = s

	rows, err := m.db.Query(ctx, `SELECT part_no, title FROM post_parts WHERE post_id = $1 ORDER BY part_no`, id)
	if err != nil {
		return d, err
	}
	d.Parts, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (PartRef, error) {
		var p PartRef
		err := r.Scan(&p.No, &p.Title)
		return p, err
	})
	if err != nil {
		return d, err
	}
	idx := -1
	for i, p := range d.Parts {
		if p.No == no {
			idx = i
		}
	}
	if idx < 0 {
		return d, ErrNotFound
	}
	if idx > 0 {
		d.Prev = &d.Parts[idx-1]
	}
	if idx < len(d.Parts)-1 {
		d.Next = &d.Parts[idx+1]
	}

	var toc []byte
	d.Part.PartRef = d.Parts[idx]
	err = m.db.QueryRow(ctx, `SELECT content_html, toc, reading_minutes FROM post_parts
		WHERE post_id = $1 AND part_no = $2`, id, no).Scan(&d.Part.ContentHTML, &toc, &d.Part.ReadingMinutes)
	if err != nil {
		return d, err
	}
	d.Part.TOC = json.RawMessage(toc)
	if len(d.Part.TOC) == 0 || string(d.Part.TOC) == "null" {
		d.Part.TOC = json.RawMessage("[]")
	}
	return d, nil
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, m.loadParts(ctx, m.db, out)
}

func (m *Module) getAdmin(ctx context.Context, q pgxQuerier, id int64) (AdminPost, error) {
	p, err := m.scanAdmin(q.QueryRow(ctx, `SELECT `+adminCols+` FROM posts p `+viewsJoin+` WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	posts := []AdminPost{p}
	err = m.loadParts(ctx, q, posts)
	return posts[0], err
}

// loadParts fills Parts of every post with one query.
func (m *Module) loadParts(ctx context.Context, q pgxQuerier, posts []AdminPost) error {
	if len(posts) == 0 {
		return nil
	}
	ids := make([]int64, len(posts))
	at := make(map[int64]int, len(posts))
	for i, p := range posts {
		ids[i], at[p.ID] = p.ID, i
	}
	rows, err := q.Query(ctx, `SELECT post_id, part_no, title, content_md, reading_minutes FROM post_parts
		WHERE post_id = ANY($1) ORDER BY post_id, part_no`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var pt AdminPostPart
		if err := rows.Scan(&id, &pt.No, &pt.Title, &pt.ContentMD, &pt.ReadingMinutes); err != nil {
			return err
		}
		posts[at[id]].Parts = append(posts[at[id]].Parts, pt)
	}
	return rows.Err()
}

type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}
