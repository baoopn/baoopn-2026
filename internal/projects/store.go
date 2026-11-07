package projects

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/baoopn-2026/internal/markdown"
)

var (
	// ErrNotFound is returned when no project matches.
	ErrNotFound = errors.New("projects: not found")
	// ErrSlugTaken is returned when an explicit slug is already in use.
	ErrSlugTaken = errors.New("projects: slug taken")
)

const cols = `id, slug, github_id, repo_full_name, repo_url, gh_homepage, gh_description, gh_language,
	gh_languages, gh_topics, gh_stars, gh_forks, gh_archived, gh_fork, gh_private, gh_pushed_at,
	gh_created_at, readme_html, synced_at, title, summary, body_md, body_html, tech, period, cover_url,
	demo_url, post_slug, visible, featured, sort_order, created_at, updated_at, gh_default_branch`

const order = `ORDER BY featured DESC, sort_order, gh_pushed_at DESC NULLS LAST, id`

func scanProject(row pgx.Row) (project, error) {
	var p project
	err := row.Scan(&p.ID, &p.Slug, &p.GitHubID, &p.RepoFullName, &p.RepoURL, &p.GHHomepage, &p.GHDescription,
		&p.GHLanguage, &p.GHLanguages, &p.GHTopics, &p.GHStars, &p.GHForks, &p.GHArchived, &p.GHFork,
		&p.GHPrivate, &p.GHPushedAt, &p.GHCreatedAt, &p.ReadmeHTML, &p.SyncedAt, &p.Title, &p.Summary,
		&p.BodyMD, &p.BodyHTML, &p.Tech, &p.Period, &p.CoverURL, &p.DemoURL, &p.PostSlug, &p.Visible,
		&p.Featured, &p.SortOrder, &p.CreatedAt, &p.UpdatedAt, &p.GHDefaultBranch)
	return p, err
}

func (m *Module) query(ctx context.Context, where string, args ...any) ([]project, error) {
	rows, err := m.db.Query(ctx, `SELECT `+cols+` FROM projects WHERE `+where+` `+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (m *Module) one(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, where string, args ...any) (project, error) {
	p, err := scanProject(q.QueryRow(ctx, `SELECT `+cols+` FROM projects WHERE `+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// views30d returns page views of /projects/<slug> over 30 days, by slug.
func (m *Module) views30d(ctx context.Context) (map[string]int64, error) {
	rows, err := m.db.Query(ctx, `SELECT substr(path, 11), sum(views)::bigint FROM page_view_stats
		WHERE bucket >= now() - interval '30 days' AND path LIKE '/projects/%' GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var slug string
		var n int64
		if err := rows.Scan(&slug, &n); err != nil {
			return nil, err
		}
		out[slug] = n
	}
	return out, rows.Err()
}

func (m *Module) adminOne(ctx context.Context, p project) (AdminProject, error) {
	views, err := m.views30d(ctx)
	if err != nil {
		return AdminProject{}, err
	}
	return p.admin(views[p.Slug]), nil
}

// createManual inserts a hand-made, hidden project.
func (m *Module) createManual(ctx context.Context, title, slug string) (AdminProject, error) {
	title = strings.TrimSpace(title)
	if n := runes(title); n < 1 || n > 200 {
		return AdminProject{}, invalid("title must be 1–200 characters")
	}
	slug = strings.TrimSpace(slug)
	generated := slug == ""
	if generated {
		slug = markdown.Slugify(title)
		if slug == "" {
			slug = "project"
		}
	} else if !validSlug(slug) {
		return AdminProject{}, invalid("slug must be lowercase letters, digits and single dashes (max 80)")
	}
	for n := 1; n <= 100; n++ {
		s := suffixed(slug, n)
		p, err := scanProject(m.db.QueryRow(ctx, `INSERT INTO projects (slug, title) VALUES ($1, $2)
			ON CONFLICT (slug) DO NOTHING RETURNING `+cols, s, title))
		if errors.Is(err, pgx.ErrNoRows) {
			if !generated {
				return AdminProject{}, ErrSlugTaken
			}
			continue
		}
		if err != nil {
			return AdminProject{}, err
		}
		return p.admin(0), nil
	}
	return AdminProject{}, ErrSlugTaken
}

// Patch is the PATCH body (types.ts ProjectPatch); nil fields are unchanged.
type Patch struct {
	Slug      *string   `json:"slug"`
	Title     *string   `json:"title"`
	Summary   *string   `json:"summary"`
	BodyMD    *string   `json:"body_md"`
	Tech      *[]string `json:"tech"`
	Period    *string   `json:"period"`
	CoverURL  *string   `json:"cover_url"`
	DemoURL   *string   `json:"demo_url"`
	PostSlug  *string   `json:"post_slug"`
	Visible   *bool     `json:"visible"`
	Featured  *bool     `json:"featured"`
	SortOrder *int      `json:"sort_order"`
}

// apply validates in and applies it to p.
func (in Patch) apply(p *project) (bodyChanged bool, err error) {
	if in.Slug != nil {
		s := strings.TrimSpace(*in.Slug)
		if !validSlug(s) {
			return false, invalid("slug must be lowercase letters, digits and single dashes (max 80)")
		}
		p.Slug = s
	}
	if in.Title != nil {
		t := strings.TrimSpace(*in.Title)
		if n := runes(t); n < 1 || n > 200 {
			return false, invalid("title must be 1–200 characters")
		}
		p.Title = t
	}
	if in.Summary != nil {
		s := strings.TrimSpace(*in.Summary)
		if runes(s) > 500 {
			return false, invalid("summary must be at most 500 characters")
		}
		p.Summary = s
	}
	if in.BodyMD != nil {
		if runes(*in.BodyMD) > 100_000 {
			return false, invalid("body_md must be at most 100000 characters")
		}
		bodyChanged = *in.BodyMD != p.BodyMD
		p.BodyMD = *in.BodyMD
	}
	if in.Tech != nil {
		tech := []string{}
		for _, t := range *in.Tech {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if runes(t) > 40 {
				return false, invalid("each tech item must be at most 40 characters")
			}
			tech = append(tech, t)
		}
		if len(tech) > 20 {
			return false, invalid("at most 20 tech items")
		}
		p.Tech = tech
	}
	if in.Period != nil {
		s := strings.TrimSpace(*in.Period)
		if runes(s) > 50 {
			return false, invalid("period must be at most 50 characters")
		}
		p.Period = s
	}
	for _, u := range []struct {
		v    *string
		dst  *string
		name string
	}{{in.CoverURL, &p.CoverURL, "cover_url"}, {in.DemoURL, &p.DemoURL, "demo_url"}} {
		if u.v == nil {
			continue
		}
		s := strings.TrimSpace(*u.v)
		if !validURL(s) {
			return false, invalid(u.name + " must be an http(s) URL or a /path")
		}
		*u.dst = s
	}
	if in.PostSlug != nil {
		s := strings.TrimSpace(*in.PostSlug)
		if s != "" && !validSlug(s) {
			return false, invalid("post_slug must be empty or a slug")
		}
		p.PostSlug = s
	}
	if in.Visible != nil {
		p.Visible = *in.Visible
	}
	if in.Featured != nil {
		p.Featured = *in.Featured
	}
	if in.SortOrder != nil {
		if *in.SortOrder < 0 || *in.SortOrder > 100_000 {
			return false, invalid("sort_order must be 0–100000")
		}
		p.SortOrder = *in.SortOrder
	}
	return bodyChanged, nil
}

// patch applies in to project id. becameVisible reports a hidden → visible flip.
func (m *Module) patch(ctx context.Context, id int64, in Patch) (p project, becameVisible bool, err error) {
	err = pgx.BeginFunc(ctx, m.db, func(tx pgx.Tx) error {
		cur, err := m.one(ctx, tx, `id = $1 FOR UPDATE`, id)
		if err != nil {
			return err
		}
		wasVisible := cur.Visible
		bodyChanged, err := in.apply(&cur)
		if err != nil {
			return err
		}
		if bodyChanged {
			cur.BodyHTML = ""
			if cur.BodyMD != "" {
				res, err := markdown.Render(cur.BodyMD, markdown.Options{Trusted: true})
				if err != nil {
					return err
				}
				cur.BodyHTML = res.HTML
			}
		}
		_, err = tx.Exec(ctx, `UPDATE projects SET slug = $2, title = $3, summary = $4, body_md = $5,
			body_html = $6, tech = $7, period = $8, cover_url = $9, demo_url = $10, post_slug = $11,
			visible = $12, featured = $13, sort_order = $14, updated_at = now() WHERE id = $1`,
			id, cur.Slug, cur.Title, cur.Summary, cur.BodyMD, cur.BodyHTML, orEmpty(cur.Tech), cur.Period,
			cur.CoverURL, cur.DemoURL, cur.PostSlug, cur.Visible, cur.Featured, cur.SortOrder)
		if isUnique(err) {
			return ErrSlugTaken
		}
		if err != nil {
			return err
		}
		becameVisible = !wasVisible && cur.Visible
		p, err = m.one(ctx, tx, `id = $1`, id)
		return err
	})
	return p, becameVisible, err
}

func (m *Module) remove(ctx context.Context, id int64) (bool, error) {
	tag, err := m.db.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id)
	return tag.RowsAffected() > 0, err
}

// reorder sets sort_order to each id's index in ids.
func (m *Module) reorder(ctx context.Context, ids []int64) error {
	_, err := m.db.Exec(ctx, `UPDATE projects p SET sort_order = t.ord - 1, updated_at = now()
		FROM unnest($1::bigint[]) WITH ORDINALITY t(id, ord) WHERE p.id = t.id`, ids)
	return err
}

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

// suffixed returns slug for n == 1 and slug-n otherwise, kept within 80 chars.
func suffixed(slug string, n int) string {
	if n <= 1 {
		return slug
	}
	sfx := "-" + strconv.Itoa(n)
	if len(slug)+len(sfx) > 80 {
		slug = strings.TrimRight(slug[:80-len(sfx)], "-")
	}
	return slug + sfx
}
