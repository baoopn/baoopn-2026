// Package profile stores the site owner's profile (settings key "profile"),
// seeded from seed/profile.json and edited from the admin dashboard.
package profile

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/markdown"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

const key = "profile"

// Deps are the profile module's dependencies.
type Deps struct {
	DB          *pgxpool.Pool
	Log         *slog.Logger
	GitHubUser  string
	GitHubToken string
	HTTP        *http.Client
	Seed        fs.FS  // reads profile.json; nil = built-in minimal default
	APIBase     string // GitHub API base, default https://api.github.com
}

// Module implements httpapi.Module for the profile.
type Module struct {
	db      *pgxpool.Pool
	log     *slog.Logger
	ghUser  string
	ghToken string
	http    *http.Client
	apiBase string
	seed    Profile
	now     func() time.Time
}

// SocialLink is one social profile link.
type SocialLink struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	Icon   string `json:"icon"`
	Handle string `json:"handle,omitempty"`
}

// SkillGroup is a named list of skills.
type SkillGroup struct {
	Group string   `json:"group"`
	Items []string `json:"items"`
}

// ExperienceItem is one job or role.
type ExperienceItem struct {
	Role        string `json:"role"`
	Org         string `json:"org"`
	URL         string `json:"url,omitempty"`
	Location    string `json:"location,omitempty"`
	Start       string `json:"start"`
	End         string `json:"end,omitempty"`
	SummaryMD   string `json:"summary_md,omitempty"`
	SummaryHTML string `json:"summary_html,omitempty"`
}

// SEO holds the default meta tags.
type SEO struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	OGImage     string `json:"og_image"`
}

// Profile is types.ts Profile.
type Profile struct {
	Name             string           `json:"name"`
	Headline         string           `json:"headline"`
	Location         string           `json:"location"`
	Pronouns         string           `json:"pronouns,omitempty"`
	Email            string           `json:"email"`
	AvatarURL        string           `json:"avatar_url"`
	CoverURL         string           `json:"cover_url"`
	ResumeURL        string           `json:"resume_url"`
	AvailableForWork bool             `json:"available_for_work"`
	BioMD            string           `json:"bio_md"`
	BioHTML          string           `json:"bio_html"`
	Socials          []SocialLink     `json:"socials"`
	Skills           []SkillGroup     `json:"skills"`
	Experience       []ExperienceItem `json:"experience"`
	SEO              SEO              `json:"seo"`
	UpdatedAt        *time.Time       `json:"updated_at,omitempty"`
}

// New returns the profile module, parsing the seed profile once.
func New(d Deps) *Module {
	if d.Log == nil {
		d.Log = slog.Default()
	}
	if d.HTTP == nil {
		d.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if d.APIBase == "" {
		d.APIBase = "https://api.github.com"
	}
	m := &Module{db: d.DB, log: d.Log, ghUser: d.GitHubUser, ghToken: d.GitHubToken, http: d.HTTP,
		apiBase: strings.TrimRight(d.APIBase, "/"), seed: Profile{Name: "Bao Nguyen"}, now: time.Now}
	if d.Seed != nil {
		raw, err := fs.ReadFile(d.Seed, "profile.json")
		if err == nil {
			var p Profile
			if err = json.Unmarshal(raw, &p); err == nil {
				m.seed = p
			}
		}
		if err != nil {
			d.Log.Warn("profile: seed unreadable, using default", logging.Err(err))
		}
	}
	if err := render(&m.seed); err != nil {
		d.Log.Warn("profile: render seed", logging.Err(err))
	}
	normalize(&m.seed)
	return m
}

// normalize replaces nil slices so they encode as [].
func normalize(p *Profile) {
	if p.Socials == nil {
		p.Socials = []SocialLink{}
	}
	if p.Skills == nil {
		p.Skills = []SkillGroup{}
	}
	for i := range p.Skills {
		if p.Skills[i].Items == nil {
			p.Skills[i].Items = []string{}
		}
	}
	if p.Experience == nil {
		p.Experience = []ExperienceItem{}
	}
	if p.UpdatedAt != nil {
		t := p.UpdatedAt.UTC()
		p.UpdatedAt = &t
	}
}

// render fills the read-only HTML fields from their Markdown.
func render(p *Profile) error {
	res, err := markdown.Render(p.BioMD, markdown.Options{Trusted: true})
	if err != nil {
		return err
	}
	p.BioHTML = res.HTML
	for i := range p.Experience {
		e := &p.Experience[i]
		e.SummaryHTML = ""
		if e.SummaryMD != "" {
			r, err := markdown.Render(e.SummaryMD, markdown.Options{Trusted: true})
			if err != nil {
				return err
			}
			e.SummaryHTML = r.HTML
		}
	}
	return nil
}

// Get returns the stored profile, or the seed when none is stored.
func (m *Module) Get(ctx context.Context) (Profile, error) {
	var p Profile
	ok, err := settings.Get(ctx, m.db, key, &p)
	if err != nil {
		return Profile{}, err
	}
	if !ok {
		p = m.seed
	}
	normalize(&p)
	return p, nil
}

// EnsureSeeded stores the seed profile when no profile is stored yet.
func (m *Module) EnsureSeeded(ctx context.Context) error {
	tag, err := m.db.Exec(ctx, `INSERT INTO settings (key, value, updated_at)
		SELECT $1, $2::jsonb, now() WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key = $1)`, key, m.seedJSON())
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		m.log.Info("profile: seeded")
	}
	return nil
}

func (m *Module) seedJSON() string {
	p := m.seed
	t := m.now().UTC()
	p.UpdatedAt = &t
	b, _ := json.Marshal(p)
	return string(b)
}

// save validates, renders and stores p.
func (m *Module) save(ctx context.Context, p Profile) (Profile, error) {
	if err := validate(&p); err != nil {
		return Profile{}, err
	}
	if err := render(&p); err != nil {
		return Profile{}, err
	}
	t := m.now().UTC()
	p.UpdatedAt = &t
	normalize(&p)
	if err := settings.Put(ctx, m.db, key, p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

// ValidationError is a user-facing input error (422 validation).
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(msg string) error { return &ValidationError{Msg: msg} }

var (
	monthRe = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
	icons   = map[string]bool{"github": true, "linkedin": true, "instagram": true, "email": true, "x": true,
		"youtube": true, "website": true, "rss": true, "spotify": true}
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

func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s && a.Name == ""
}

// socialURL accepts http(s) URLs with a host and mailto: addresses.
func socialURL(s string) bool {
	if len(s) > 500 {
		return false
	}
	if addr, ok := strings.CutPrefix(s, "mailto:"); ok {
		return validEmail(addr)
	}
	return s != "" && !strings.HasPrefix(s, "/") && validURL(s)
}

func maxLen(name, v string, n int) error {
	if runes(v) > n {
		return invalid(name + " must be at most " + strconv.Itoa(n) + " characters")
	}
	return nil
}

// validate trims and checks every field, in place.
func validate(p *Profile) error {
	p.Name = strings.TrimSpace(p.Name)
	if n := runes(p.Name); n < 1 || n > 100 {
		return invalid("name must be 1–100 characters")
	}
	for _, f := range []struct {
		name string
		v    *string
		max  int
	}{{"headline", &p.Headline, 200}, {"location", &p.Location, 100}, {"pronouns", &p.Pronouns, 100}} {
		*f.v = strings.TrimSpace(*f.v)
		if err := maxLen(f.name, *f.v, f.max); err != nil {
			return err
		}
	}
	p.Email = strings.TrimSpace(p.Email)
	if p.Email != "" && !validEmail(p.Email) {
		return invalid("email must be a valid address")
	}
	for _, f := range []struct {
		name string
		v    *string
	}{{"avatar_url", &p.AvatarURL}, {"cover_url", &p.CoverURL}, {"resume_url", &p.ResumeURL}, {"seo.og_image", &p.SEO.OGImage}} {
		*f.v = strings.TrimSpace(*f.v)
		if !validURL(*f.v) {
			return invalid(f.name + " must be an http(s) URL or a /path")
		}
	}
	if err := maxLen("bio_md", p.BioMD, 20_000); err != nil {
		return err
	}
	if len(p.Socials) > 20 {
		return invalid("at most 20 socials")
	}
	for i := range p.Socials {
		s := &p.Socials[i]
		s.Label, s.URL, s.Icon, s.Handle = strings.TrimSpace(s.Label), strings.TrimSpace(s.URL), strings.TrimSpace(s.Icon), strings.TrimSpace(s.Handle)
		if n := runes(s.Label); n < 1 || n > 50 {
			return invalid("social label must be 1–50 characters")
		}
		if !icons[s.Icon] {
			return invalid("social icon " + quote(s.Icon) + " is not supported")
		}
		if !socialURL(s.URL) {
			return invalid("social url must be an http(s) or mailto: URL")
		}
		if err := maxLen("social handle", s.Handle, 100); err != nil {
			return err
		}
	}
	if len(p.Skills) > 20 {
		return invalid("at most 20 skill groups")
	}
	for i := range p.Skills {
		g := &p.Skills[i]
		g.Group = strings.TrimSpace(g.Group)
		if n := runes(g.Group); n < 1 || n > 50 {
			return invalid("skill group name must be 1–50 characters")
		}
		if len(g.Items) > 50 {
			return invalid("at most 50 items per skill group")
		}
		items := []string{}
		for _, it := range g.Items {
			it = strings.TrimSpace(it)
			if it == "" {
				continue
			}
			if runes(it) > 40 {
				return invalid("skill items must be at most 40 characters")
			}
			items = append(items, it)
		}
		g.Items = items
	}
	if len(p.Experience) > 50 {
		return invalid("at most 50 experience items")
	}
	for i := range p.Experience {
		e := &p.Experience[i]
		e.Role, e.Org, e.URL, e.Location = strings.TrimSpace(e.Role), strings.TrimSpace(e.Org), strings.TrimSpace(e.URL), strings.TrimSpace(e.Location)
		e.Start, e.End = strings.TrimSpace(e.Start), strings.TrimSpace(e.End)
		if n := runes(e.Role); n < 1 || n > 200 {
			return invalid("experience role must be 1–200 characters")
		}
		if n := runes(e.Org); n < 1 || n > 200 {
			return invalid("experience org must be 1–200 characters")
		}
		if !validURL(e.URL) {
			return invalid("experience url must be an http(s) URL or a /path")
		}
		if err := maxLen("experience location", e.Location, 100); err != nil {
			return err
		}
		if !monthRe.MatchString(e.Start) || (e.End != "" && !monthRe.MatchString(e.End)) {
			return invalid("experience start/end must be YYYY-MM")
		}
		if e.End != "" && e.End < e.Start {
			return invalid("experience end must not be before start")
		}
		if err := maxLen("experience summary_md", e.SummaryMD, 5000); err != nil {
			return err
		}
	}
	p.SEO.Title, p.SEO.Description = strings.TrimSpace(p.SEO.Title), strings.TrimSpace(p.SEO.Description)
	if err := maxLen("seo.title", p.SEO.Title, 300); err != nil {
		return err
	}
	if err := maxLen("seo.description", p.SEO.Description, 300); err != nil {
		return err
	}
	return maxLen("seo.og_image", p.SEO.OGImage, 300)
}

func quote(s string) string {
	if runes(s) > 30 {
		s = string([]rune(s)[:30]) + "…"
	}
	return `"` + s + `"`
}

// SettingsKey is the settings row holding the profile.
const SettingsKey = key

// RenderHTML fills the read-only HTML fields of p from its Markdown.
func RenderHTML(p *Profile) error { return render(p) }
