// Package blog serves blog posts: Markdown rendered on save, drafts,
// scheduled publishing, series navigation, tags and legacy v3 redirects.
package blog

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/markdown"
)

// Deps are the blog module's dependencies.
type Deps struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
}

// Module implements httpapi.Module for posts and tags.
type Module struct {
	db  *pgxpool.Pool
	log *slog.Logger
	now func() time.Time
}

// New returns the blog module.
func New(d Deps) *Module {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Module{db: d.DB, log: d.Log, now: time.Now}
}

// PostInput is the POST/PUT body (types.ts PostInput).
type PostInput struct {
	Slug        string     `json:"slug"`
	Title       string     `json:"title"`
	Excerpt     string     `json:"excerpt"`
	ContentMD   string     `json:"content_md"`
	CoverURL    string     `json:"cover_url"`
	Tags        []string   `json:"tags"`
	Series      string     `json:"series"`
	SeriesTitle string     `json:"series_title"`
	SeriesOrder int        `json:"series_order"`
	Status      string     `json:"status"`
	PublishedAt *time.Time `json:"published_at"`
}

// ImportPost is a post imported with its old v3 URL.
type ImportPost struct {
	PostInput
	LegacyPath string
}

// PostLink is a navigation link (types.ts PostLink).
type PostLink struct {
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	SeriesOrder int    `json:"series_order"`
}

// PostSummary is a public list item (types.ts PostSummary).
type PostSummary struct {
	Slug           string    `json:"slug"`
	Title          string    `json:"title"`
	Excerpt        string    `json:"excerpt"`
	CoverURL       string    `json:"cover_url"`
	Tags           []string  `json:"tags"`
	Series         string    `json:"series"`
	SeriesTitle    string    `json:"series_title"`
	SeriesOrder    int       `json:"series_order"`
	PublishedAt    time.Time `json:"published_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ReadingMinutes int       `json:"reading_minutes"`
	Views          int64     `json:"views"`
}

// PostDetail is a full public post (types.ts PostDetail).
type PostDetail struct {
	PostSummary
	ContentHTML string          `json:"content_html"`
	TOC         json.RawMessage `json:"toc"`
	SeriesPosts []PostLink      `json:"series_posts"`
	Prev        *PostLink       `json:"prev"`
	Next        *PostLink       `json:"next"`
	id          int64
}

// AdminPost is the admin view of a post (types.ts AdminPost).
type AdminPost struct {
	ID             int64      `json:"id"`
	Slug           string     `json:"slug"`
	Title          string     `json:"title"`
	Excerpt        string     `json:"excerpt"`
	ContentMD      string     `json:"content_md"`
	CoverURL       string     `json:"cover_url"`
	Tags           []string   `json:"tags"`
	Series         string     `json:"series"`
	SeriesTitle    string     `json:"series_title"`
	SeriesOrder    int        `json:"series_order"`
	Status         string     `json:"status"`
	State          string     `json:"state"`
	PublishedAt    *time.Time `json:"published_at"`
	ReadingMinutes int        `json:"reading_minutes"`
	Views          int64      `json:"views"`
	LegacyPath     *string    `json:"legacy_path"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// TagCount is one tag with its number of visible posts.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int64  `json:"count"`
}

var (
	// ErrSlugTaken is returned when an explicit slug is already in use.
	ErrSlugTaken = errors.New("blog: slug taken")
	// ErrNotFound is returned when no post matches.
	ErrNotFound = errors.New("blog: not found")
)

// ValidationError is a user-facing input error (422 validation).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(msg string) error { return &ValidationError{Msg: msg} }

var (
	slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	tagRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9 .+#-]{0,29}$`)
)

func runes(s string) int { return utf8.RuneCountInString(s) }

// validURL is the shared URL rule: empty, http(s)://host…, or /path (not //).
func validURL(s string) bool {
	if s == "" {
		return true
	}
	if len(s) > 500 || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	if strings.HasPrefix(s, "/") {
		return !strings.HasPrefix(s, "//") && !strings.HasPrefix(s, "/\\")
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// normalizeTags lowercases, trims and dedupes tags, then validates them.
func normalizeTags(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || seen[t] {
			continue
		}
		if !tagRe.MatchString(t) {
			return nil, invalid("tag " + quote(t) + " is invalid (letters, digits, space, . + # -; max 30)")
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) > 10 {
		return nil, invalid("at most 10 tags")
	}
	return out, nil
}

func quote(s string) string {
	if runes(s) > 40 {
		s = string([]rune(s)[:40]) + "…"
	}
	return `"` + s + `"`
}

// validate normalises in place. It reports whether the slug was generated
// (and may therefore be suffixed on collision).
func validate(in *PostInput, now time.Time) (generated bool, err error) {
	in.Title = strings.TrimSpace(in.Title)
	if n := runes(in.Title); n < 1 || n > 200 {
		return false, invalid("title must be 1–200 characters")
	}
	in.Slug = strings.TrimSpace(in.Slug)
	if in.Slug == "" {
		in.Slug = markdown.Slugify(in.Title)
		if in.Slug == "" {
			in.Slug = "post"
		}
		generated = true
	} else if len(in.Slug) > 80 || !slugRe.MatchString(in.Slug) {
		return false, invalid("slug must be lowercase letters, digits and single dashes (max 80)")
	}
	in.Excerpt = strings.TrimSpace(in.Excerpt)
	if runes(in.Excerpt) > 500 {
		return false, invalid("excerpt must be at most 500 characters")
	}
	if runes(in.ContentMD) > 200_000 {
		return false, invalid("content_md must be at most 200000 characters")
	}
	if in.Excerpt == "" {
		in.Excerpt = markdown.Excerpt(in.ContentMD, 200)
	}
	in.CoverURL = strings.TrimSpace(in.CoverURL)
	if !validURL(in.CoverURL) {
		return false, invalid("cover_url must be an http(s) URL or a /path")
	}
	if in.Tags, err = normalizeTags(in.Tags); err != nil {
		return false, err
	}
	in.Series = strings.TrimSpace(in.Series)
	if in.Series != "" && (len(in.Series) > 80 || !slugRe.MatchString(in.Series)) {
		return false, invalid("series must be empty or a slug")
	}
	in.SeriesTitle = strings.TrimSpace(in.SeriesTitle)
	if runes(in.SeriesTitle) > 200 {
		return false, invalid("series_title must be at most 200 characters")
	}
	if in.SeriesOrder < 0 || in.SeriesOrder > 1000 {
		return false, invalid("series_order must be 0–1000")
	}
	switch in.Status {
	case "draft":
	case "published":
		if in.PublishedAt == nil {
			t := now
			in.PublishedAt = &t
		}
	default:
		return false, invalid(`status must be "draft" or "published"`)
	}
	if in.PublishedAt != nil {
		t := in.PublishedAt.UTC()
		in.PublishedAt = &t
	}
	return generated, nil
}

// stateOf derives the admin state from status and published_at.
func stateOf(status string, publishedAt *time.Time, now time.Time) string {
	if status == "draft" {
		return "draft"
	}
	if publishedAt != nil && publishedAt.After(now) {
		return "scheduled"
	}
	return "published"
}

// escapeLike escapes LIKE wildcards for use with ESCAPE '\'.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
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
