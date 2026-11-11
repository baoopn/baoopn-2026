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
	"strconv"
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

// Stats counts the rows changed. Skipped names the rows ("post_parts:12")
// that kept changing under the rewrite and still hold their old links.
type Stats struct {
	Parts, Posts, Projects int
	Profile                bool
	Skipped                []string
}

// casAttempts is how often a row is read and rewritten when a save keeps
// landing between the read and the compare-and-swap write.
const casAttempts = 3

// Rewrite applies fn to every stored field that may reference media and
// writes back (re-rendered) only the rows that changed.
//
// Each write is a compare-and-swap on the text it read, so an admin save in
// the meantime is never overwritten with the old text. The row is then read
// again and rewritten, up to casAttempts times; a row that still changes is
// left as it is and listed in Stats.Skipped for the caller to report.
func Rewrite(ctx context.Context, db *pgxpool.Pool, fn func(string) string) (Stats, error) {
	var st Stats
	var err error
	if st.Parts, err = rewriteParts(ctx, db, fn, &st.Skipped); err != nil {
		return st, fmt.Errorf("post parts: %w", err)
	}
	if st.Posts, err = rewriteColumn(ctx, db, "posts", "cover_url", fn, &st.Skipped); err != nil {
		return st, fmt.Errorf("post covers: %w", err)
	}
	if st.Projects, err = rewriteProjects(ctx, db, fn, &st.Skipped); err != nil {
		return st, fmt.Errorf("projects: %w", err)
	}
	if st.Profile, err = rewriteProfile(ctx, db, fn, &st.Skipped); err != nil {
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

// casRow rewrites one row read as text. write stores next only if the row
// still holds old; on a miss the row is read again (q, by id) and rewritten,
// up to casAttempts times. It reports whether the row changed; a row that
// kept changing is added to skipped as name:id.
func casRow(ctx context.Context, db *pgxpool.Pool, name, q string, row idText, fn func(string) string,
	write func(old, next string) (bool, error), skipped *[]string) (bool, error) {
	text := row.text
	for attempt := 1; ; attempt++ {
		next := fn(text)
		if next == text {
			return false, nil
		}
		ok, err := write(text, next)
		if err != nil || ok {
			return ok, err
		}
		if attempt == casAttempts {
			*skipped = append(*skipped, name+":"+strconv.FormatInt(row.id, 10))
			return false, nil
		}
		err = db.QueryRow(ctx, q, row.id).Scan(&text)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // deleted meanwhile
		}
		if err != nil {
			return false, err
		}
	}
}

func rewriteParts(ctx context.Context, db *pgxpool.Pool, fn func(string) string, skipped *[]string) (int, error) {
	parts, err := collect(ctx, db, `SELECT id, content_md FROM post_parts ORDER BY id`)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range parts {
		write := func(old, md string) (bool, error) {
			res, err := markdown.Render(md, markdown.Options{Trusted: true})
			if err != nil {
				return false, err
			}
			toc, err := json.Marshal(res.TOC)
			if err != nil {
				return false, err
			}
			tag, err := db.Exec(ctx, `UPDATE post_parts SET content_md = $2, content_html = $3, toc = $4::jsonb
				WHERE id = $1 AND content_md = $5`, p.id, md, res.HTML, string(toc), old)
			return tag.RowsAffected() > 0, err
		}
		ok, err := casRow(ctx, db, "post_parts", `SELECT content_md FROM post_parts WHERE id = $1`, p, fn, write, skipped)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

// rewriteColumn rewrites a plain text column (table and column are constants).
func rewriteColumn(ctx context.Context, db *pgxpool.Pool, table, col string, fn func(string) string, skipped *[]string) (int, error) {
	rows, err := collect(ctx, db, `SELECT id, `+col+` FROM `+table+` WHERE `+col+` <> '' ORDER BY id`)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range rows {
		write := func(old, v string) (bool, error) {
			tag, err := db.Exec(ctx, `UPDATE `+table+` SET `+col+` = $2 WHERE id = $1 AND `+col+` = $3`, r.id, v, old)
			return tag.RowsAffected() > 0, err
		}
		ok, err := casRow(ctx, db, table+"."+col, `SELECT `+col+` FROM `+table+` WHERE id = $1`, r, fn, write, skipped)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

func rewriteProjects(ctx context.Context, db *pgxpool.Pool, fn func(string) string, skipped *[]string) (int, error) {
	n, err := rewriteColumn(ctx, db, "projects", "cover_url", fn, skipped)
	if err != nil {
		return 0, err
	}
	m, err := rewriteColumn(ctx, db, "projects", "logo_url", fn, skipped)
	if err != nil {
		return n, err
	}
	n += m
	bodies, err := collect(ctx, db, `SELECT id, body_md FROM projects WHERE body_md <> '' ORDER BY id`)
	if err != nil {
		return n, err
	}
	for _, b := range bodies {
		write := func(old, md string) (bool, error) {
			res, err := markdown.Render(md, markdown.Options{Trusted: true})
			if err != nil {
				return false, err
			}
			tag, err := db.Exec(ctx, `UPDATE projects SET body_md = $2, body_html = $3 WHERE id = $1 AND body_md = $4`,
				b.id, md, res.HTML, old)
			return tag.RowsAffected() > 0, err
		}
		ok, err := casRow(ctx, db, "projects.body_md", `SELECT body_md FROM projects WHERE id = $1`, b, fn, write, skipped)
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

func rewriteProfile(ctx context.Context, db *pgxpool.Pool, fn func(string) string, skipped *[]string) (bool, error) {
	for range casAttempts {
		done, ok, err := rewriteProfileOnce(ctx, db, fn)
		if err != nil || done {
			return ok, err
		}
	}
	*skipped = append(*skipped, "settings:"+profile.SettingsKey)
	return false, nil
}

// rewriteProfileOnce reports done when nothing is left to try: the profile
// is missing, needs no change, or was written (ok).
func rewriteProfileOnce(ctx context.Context, db *pgxpool.Pool, fn func(string) string) (done, ok bool, err error) {
	var raw string
	err = db.QueryRow(ctx, `SELECT value::text FROM settings WHERE key = $1`, profile.SettingsKey).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, false, nil
	}
	if err != nil {
		return true, false, err
	}
	var p profile.Profile
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return true, false, err
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
		return true, false, nil
	}
	if err := profile.RenderHTML(&p); err != nil {
		return true, false, err
	}
	next, err := json.Marshal(p)
	if err != nil {
		return true, false, err
	}
	// Only if the profile is still the one read above.
	tag, err := db.Exec(ctx, `UPDATE settings SET value = $2::jsonb, updated_at = now()
		WHERE key = $1 AND value = $3::jsonb`, profile.SettingsKey, string(next), raw)
	if err != nil {
		return true, false, err
	}
	return tag.RowsAffected() > 0, tag.RowsAffected() > 0, nil
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
	if len(st.Skipped) > 0 {
		log.Warn("media link rewrite skipped rows that kept changing", append(attrs,
			slog.String("skipped", strings.Join(st.Skipped, ", ")))...)
		return
	}
	log.Info("media link rewrite done", attrs...)
}
