// Package projects lists portfolio projects: GitHub repos synced into the
// projects table plus hand-made entries, curated from the admin dashboard.
package projects

import (
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Deps are the projects module's dependencies.
type Deps struct {
	DB     *pgxpool.Pool
	Log    *slog.Logger
	Syncer *Syncer
}

// Module implements httpapi.Module for projects.
type Module struct {
	db     *pgxpool.Pool
	log    *slog.Logger
	syncer *Syncer
}

// New returns the projects module. Syncer may be nil (sync disabled).
func New(d Deps) *Module {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Module{db: d.DB, log: d.Log, syncer: d.Syncer}
}

// project is one row of the projects table.
type project struct {
	ID              int64
	Slug            string
	GitHubID        *int64
	RepoFullName    string
	RepoURL         string
	GHHomepage      string
	GHDescription   string
	GHLanguage      string
	GHLanguages     map[string]int64
	GHTopics        []string
	GHStars         int
	GHForks         int
	GHArchived      bool
	GHFork          bool
	GHPrivate       bool
	GHPushedAt      *time.Time
	GHCreatedAt     *time.Time
	ReadmeHTML      string
	SyncedAt        *time.Time
	Title           string
	Summary         string
	BodyMD          string
	BodyHTML        string
	Tech            []string
	Period          string
	CoverURL        string
	DemoURL         string
	PostSlug        string
	Visible         bool
	Featured        bool
	SortOrder       int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	GHDefaultBranch string
}

// ProjectSummary is the public list item (types.ts ProjectSummary).
type ProjectSummary struct {
	Slug     string     `json:"slug"`
	Title    string     `json:"title"`
	Summary  string     `json:"summary"`
	Tech     []string   `json:"tech"`
	Period   string     `json:"period"`
	CoverURL string     `json:"cover_url"`
	DemoURL  string     `json:"demo_url"`
	RepoURL  string     `json:"repo_url"`
	Language string     `json:"language"`
	Stars    int        `json:"stars"`
	Forks    int        `json:"forks"`
	Archived bool       `json:"archived"`
	Featured bool       `json:"featured"`
	PushedAt *time.Time `json:"pushed_at"`
	PostSlug string     `json:"post_slug"`
}

// ProjectDetail is the public project page (types.ts ProjectDetail).
type ProjectDetail struct {
	ProjectSummary
	BodyHTML   string           `json:"body_html"`
	BodySource string           `json:"body_source"`
	Languages  map[string]int64 `json:"languages"`
	Topics     []string         `json:"topics"`
	CreatedAt  *time.Time       `json:"created_at"`
}

// AdminProject is the admin view (types.ts AdminProject).
type AdminProject struct {
	ID            int64            `json:"id"`
	Slug          string           `json:"slug"`
	GitHubID      *int64           `json:"github_id"`
	RepoFullName  string           `json:"repo_full_name"`
	RepoURL       string           `json:"repo_url"`
	GHHomepage    string           `json:"gh_homepage"`
	GHDescription string           `json:"gh_description"`
	GHLanguage    string           `json:"gh_language"`
	GHLanguages   map[string]int64 `json:"gh_languages"`
	GHTopics      []string         `json:"gh_topics"`
	GHStars       int              `json:"gh_stars"`
	GHForks       int              `json:"gh_forks"`
	GHArchived    bool             `json:"gh_archived"`
	GHFork        bool             `json:"gh_fork"`
	GHPrivate     bool             `json:"gh_private"`
	GHPushedAt    *time.Time       `json:"gh_pushed_at"`
	SyncedAt      *time.Time       `json:"synced_at"`
	Title         string           `json:"title"`
	Summary       string           `json:"summary"`
	BodyMD        string           `json:"body_md"`
	Tech          []string         `json:"tech"`
	Period        string           `json:"period"`
	CoverURL      string           `json:"cover_url"`
	DemoURL       string           `json:"demo_url"`
	PostSlug      string           `json:"post_slug"`
	Visible       bool             `json:"visible"`
	Featured      bool             `json:"featured"`
	SortOrder     int              `json:"sort_order"`
	Views30d      int64            `json:"views_30d"`
	CreatedAt     time.Time        `json:"created_at"`
	UpdatedAt     time.Time        `json:"updated_at"`
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (p *project) summary() ProjectSummary {
	sum := p.Summary
	if sum == "" {
		sum = p.GHDescription
	}
	tech := p.Tech
	switch {
	case len(tech) > 0:
	case len(p.GHTopics) > 0:
		tech = p.GHTopics
	case p.GHLanguage != "":
		tech = []string{p.GHLanguage}
	default:
		tech = []string{}
	}
	demo := p.DemoURL
	if demo == "" {
		demo = p.GHHomepage
	}
	return ProjectSummary{
		Slug: p.Slug, Title: p.Title, Summary: sum, Tech: tech, Period: p.Period, CoverURL: p.CoverURL,
		DemoURL: demo, RepoURL: p.RepoURL, Language: p.GHLanguage, Stars: p.GHStars, Forks: p.GHForks,
		Archived: p.GHArchived, Featured: p.Featured, PushedAt: utc(p.GHPushedAt), PostSlug: p.PostSlug,
	}
}

func (p *project) detail() ProjectDetail {
	d := ProjectDetail{ProjectSummary: p.summary(), Languages: p.GHLanguages, Topics: orEmpty(p.GHTopics),
		CreatedAt: utc(p.GHCreatedAt)}
	switch {
	case p.BodyMD != "":
		d.BodyHTML, d.BodySource = p.BodyHTML, "case_study"
	case p.ReadmeHTML != "":
		d.BodyHTML, d.BodySource = p.ReadmeHTML, "readme"
	default:
		d.BodySource = "none"
	}
	if d.Languages == nil {
		d.Languages = map[string]int64{}
	}
	return d
}

func (p *project) admin(views int64) AdminProject {
	langs := p.GHLanguages
	if langs == nil {
		langs = map[string]int64{}
	}
	return AdminProject{
		ID: p.ID, Slug: p.Slug, GitHubID: p.GitHubID, RepoFullName: p.RepoFullName, RepoURL: p.RepoURL,
		GHHomepage: p.GHHomepage, GHDescription: p.GHDescription, GHLanguage: p.GHLanguage, GHLanguages: langs,
		GHTopics: orEmpty(p.GHTopics), GHStars: p.GHStars, GHForks: p.GHForks, GHArchived: p.GHArchived,
		GHFork: p.GHFork, GHPrivate: p.GHPrivate, GHPushedAt: utc(p.GHPushedAt), SyncedAt: utc(p.SyncedAt),
		Title: p.Title, Summary: p.Summary, BodyMD: p.BodyMD, Tech: orEmpty(p.Tech), Period: p.Period,
		CoverURL: p.CoverURL, DemoURL: p.DemoURL, PostSlug: p.PostSlug, Visible: p.Visible, Featured: p.Featured,
		SortOrder: p.SortOrder, Views30d: views, CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
	}
}

var slugRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func validSlug(s string) bool { return len(s) <= 80 && slugRe.MatchString(s) }

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

// ValidationError is a user-facing input error (422 validation).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(msg string) error { return &ValidationError{Msg: msg} }
