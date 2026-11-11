// Package mediaurls rewrites media URLs stored in content: post parts,
// post and project covers, project bodies and the profile. Markdown that
// changes is re-rendered with the same renderer the editors use.
//
// It backs two jobs: the one-time move from /media/<file> links to the
// bucket's public domain (MEDIA_PUBLIC_URL), and following a file that the
// media fallback published under a new name.
package mediaurls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/markdown"
	"github.com/baoopn/baoopn-2026/internal/profile"
)

// legacyRef is a site-relative link to a stored file, exactly as the media
// store names them (24 hex characters and a stored extension).
var legacyRef = regexp.MustCompile(`/media/([0-9a-f]{24}\.(?:png|jpg|gif|webp|avif|svg|pdf))`)

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// bounded reports whether s[i:j] stands alone. Before it: nothing that
// continues a URL (so https://host/media/... is left alone). After it:
// nothing that lengthens the name or path; punctuation, a query or a
// fragment may follow.
func bounded(s string, i, j int) bool {
	if i > 0 && (isAlnum(s[i-1]) || strings.IndexByte("._~:/-%@+", s[i-1]) >= 0) {
		return false
	}
	return j >= len(s) || !(isAlnum(s[j]) || strings.IndexByte("_~/-%", s[j]) >= 0)
}

// LegacyToPublic rewrites standalone /media/<file> references to
// publicURL/<file>. The result never matches again, so it is idempotent.
func LegacyToPublic(publicURL string) func(string) string {
	publicURL = strings.TrimRight(publicURL, "/")
	return func(s string) string {
		if !strings.Contains(s, "/media/") {
			return s
		}
		idx := legacyRef.FindAllStringSubmatchIndex(s, -1)
		var b strings.Builder
		last := 0
		for _, m := range idx {
			if !bounded(s, m[0], m[1]) {
				continue
			}
			b.WriteString(s[last:m[0]])
			b.WriteString(publicURL + "/" + s[m[2]:m[3]])
			last = m[1]
		}
		if last == 0 {
			return s
		}
		b.WriteString(s[last:])
		return b.String()
	}
}

// Rename rewrites standalone links to the stored file oldName to newURL, in
// every form a link to it can take: <base>/<oldName> for each non-empty base
// (the bucket's public domain, the site's own /media URL), then the
// site-relative /media/<oldName>.
func Rename(oldName, newURL string, bases ...string) func(string) string {
	fns := make([]func(string) string, 0, len(bases)+1)
	for _, b := range bases {
		if b = strings.TrimRight(b, "/"); b != "" {
			fns = append(fns, Replace(b+"/"+oldName, newURL))
		}
	}
	fns = append(fns, Replace("/media/"+oldName, newURL))
	return func(s string) string {
		for _, fn := range fns {
			s = fn(s)
		}
		return s
	}
}

// Replace rewrites standalone occurrences of the URL old to new.
func Replace(old, new string) func(string) string {
	return func(s string) string {
		if old == "" || !strings.Contains(s, old) {
			return s
		}
		var b strings.Builder
		last := 0
		for i := 0; ; {
			k := strings.Index(s[i:], old)
			if k < 0 {
				break
			}
			p := i + k
			i = p + len(old)
			if !bounded(s, p, i) {
				continue
			}
			b.WriteString(s[last:p])
			b.WriteString(new)
			last = i
		}
		if last == 0 {
			return s
		}
		b.WriteString(s[last:])
		return b.String()
	}
}

// Stats counts the rows changed.
type Stats struct {
	Parts, Posts, Projects int
	Profile                bool
}

// Rewrite applies fn to every stored field that may reference media and
// writes back (re-rendered) only the rows that changed.
//
// Each write is a compare-and-swap on the text it read: a row an admin saved
// in the meantime is skipped rather than overwritten with the old text. It
// still holds its old links, which the next run (or the /media 301) covers.
func Rewrite(ctx context.Context, db *pgxpool.Pool, fn func(string) string) (Stats, error) {
	var st Stats
	var err error
	if st.Parts, err = rewriteParts(ctx, db, fn); err != nil {
		return st, fmt.Errorf("post parts: %w", err)
	}
	if st.Posts, err = rewriteColumn(ctx, db, "posts", "cover_url", fn); err != nil {
		return st, fmt.Errorf("post covers: %w", err)
	}
	if st.Projects, err = rewriteProjects(ctx, db, fn); err != nil {
		return st, fmt.Errorf("projects: %w", err)
	}
	if st.Profile, err = rewriteProfile(ctx, db, fn); err != nil {
		return st, fmt.Errorf("profile: %w", err)
	}
	return st, nil
}

type idText struct {
	id   int64
	text string
}

func collect(ctx context.Context, db *pgxpool.Pool, q string) ([]idText, error) {
	rows, err := db.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (idText, error) {
		var x idText
		return x, r.Scan(&x.id, &x.text)
	})
}

func rewriteParts(ctx context.Context, db *pgxpool.Pool, fn func(string) string) (int, error) {
	parts, err := collect(ctx, db, `SELECT id, content_md FROM post_parts ORDER BY id`)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range parts {
		md := fn(p.text)
		if md == p.text {
			continue
		}
		res, err := markdown.Render(md, markdown.Options{Trusted: true})
		if err != nil {
			return n, err
		}
		toc, err := json.Marshal(res.TOC)
		if err != nil {
			return n, err
		}
		tag, err := db.Exec(ctx, `UPDATE post_parts SET content_md = $2, content_html = $3, toc = $4::jsonb
			WHERE id = $1 AND content_md = $5`, p.id, md, res.HTML, string(toc), p.text)
		if err != nil {
			return n, err
		}
		n += int(tag.RowsAffected())
	}
	return n, nil
}

// rewriteColumn rewrites a plain text column (table and column are constants).
func rewriteColumn(ctx context.Context, db *pgxpool.Pool, table, col string, fn func(string) string) (int, error) {
	rows, err := collect(ctx, db, `SELECT id, `+col+` FROM `+table+` WHERE `+col+` <> '' ORDER BY id`)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		if v := fn(r.text); v != r.text {
			tag, err := db.Exec(ctx, `UPDATE `+table+` SET `+col+` = $2 WHERE id = $1 AND `+col+` = $3`,
				r.id, v, r.text)
			if err != nil {
				return n, err
			}
			n += int(tag.RowsAffected())
		}
	}
	return n, nil
}

func rewriteProjects(ctx context.Context, db *pgxpool.Pool, fn func(string) string) (int, error) {
	changed := map[int64]bool{}
	n, err := rewriteColumn(ctx, db, "projects", "cover_url", fn)
	if err != nil {
		return 0, err
	}
	m, err := rewriteColumn(ctx, db, "projects", "logo_url", fn)
	if err != nil {
		return n, err
	}
	n += m
	bodies, err := collect(ctx, db, `SELECT id, body_md FROM projects WHERE body_md <> '' ORDER BY id`)
	if err != nil {
		return n, err
	}
	for _, b := range bodies {
		md := fn(b.text)
		if md == b.text {
			continue
		}
		res, err := markdown.Render(md, markdown.Options{Trusted: true})
		if err != nil {
			return n, err
		}
		tag, err := db.Exec(ctx, `UPDATE projects SET body_md = $2, body_html = $3 WHERE id = $1 AND body_md = $4`,
			b.id, md, res.HTML, b.text)
		if err != nil {
			return n, err
		}
		if tag.RowsAffected() > 0 {
			changed[b.id] = true
		}
	}
	return n + len(changed), nil
}

func rewriteProfile(ctx context.Context, db *pgxpool.Pool, fn func(string) string) (bool, error) {
	var raw string
	err := db.QueryRow(ctx, `SELECT value::text FROM settings WHERE key = $1`, profile.SettingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var p profile.Profile
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return false, err
	}
	changed := false
	apply := func(s *string) {
		if v := fn(*s); v != *s {
			*s, changed = v, true
		}
	}
	apply(&p.AvatarURL)
	apply(&p.CoverURL)
	apply(&p.ResumeURL)
	apply(&p.SEO.OGImage)
	apply(&p.BioMD)
	for i := range p.Experience {
		apply(&p.Experience[i].SummaryMD)
	}
	if !changed {
		return false, nil
	}
	if err := profile.RenderHTML(&p); err != nil {
		return false, err
	}
	next, err := json.Marshal(p)
	if err != nil {
		return false, err
	}
	// Only if the profile is still the one read above.
	tag, err := db.Exec(ctx, `UPDATE settings SET value = $2::jsonb, updated_at = now()
		WHERE key = $1 AND value = $3::jsonb`, profile.SettingsKey, string(next), raw)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RunLegacyRewrite is the startup job: with a public media domain, every
// stored /media/<file> link becomes MEDIA_PUBLIC_URL/<file>. It is
// idempotent (rewritten links never match again) and only logs.
func RunLegacyRewrite(ctx context.Context, db *pgxpool.Pool, publicURL string, log *slog.Logger) {
	if publicURL == "" {
		return
	}
	st, err := Rewrite(ctx, db, LegacyToPublic(publicURL))
	attrs := []any{slog.Int("post_parts", st.Parts), slog.Int("post_covers", st.Posts),
		slog.Int("projects", st.Projects), slog.Bool("profile", st.Profile)}
	if err != nil {
		log.Warn("media link rewrite stopped", append(attrs, slog.String("error", err.Error()))...)
		return
	}
	log.Info("media link rewrite done", attrs...)
}
