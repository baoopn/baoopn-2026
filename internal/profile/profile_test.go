package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/testdb"
	"github.com/baoopn/baoopn-2026/seed"
)

func validProfile() Profile {
	return Profile{
		Name: "Bao Nguyen", Headline: "Dev", Email: "me@baoopn.com", AvatarURL: "/media/a.png",
		CoverURL: "https://cdn.baoopn.com/c.jpg", BioMD: "Hello **world**",
		Socials: []SocialLink{{Label: "GitHub", URL: "https://github.com/baoopn", Icon: "github"},
			{Label: "Email", URL: "mailto:me@baoopn.com", Icon: "email"}},
		Skills:     []SkillGroup{{Group: "Backend", Items: []string{" Go ", "", "SQL"}}},
		Experience: []ExperienceItem{{Role: "Engineer", Org: "Acme", Start: "2024-03", SummaryMD: "Did *things*"}},
		SEO:        SEO{Title: "Bao", Description: "Site"},
	}
}

func TestValidate(t *testing.T) {
	p := validProfile()
	if err := validate(&p); err != nil {
		t.Fatal(err)
	}
	if strings.Join(p.Skills[0].Items, ",") != "Go,SQL" {
		t.Fatalf("items %v", p.Skills[0].Items)
	}
	cases := map[string]func(*Profile){
		"empty name":     func(p *Profile) { p.Name = " " },
		"long headline":  func(p *Profile) { p.Headline = strings.Repeat("h", 201) },
		"bad email":      func(p *Profile) { p.Email = "not-an-email" },
		"named email":    func(p *Profile) { p.Email = "Bao <me@baoopn.com>" },
		"bad avatar":     func(p *Profile) { p.AvatarURL = "javascript:alert(1)" },
		"protocol rel":   func(p *Profile) { p.CoverURL = "//evil.com/x.png" },
		"long bio":       func(p *Profile) { p.BioMD = strings.Repeat("b", 20_001) },
		"bad icon":       func(p *Profile) { p.Socials[0].Icon = "myspace" },
		"social js":      func(p *Profile) { p.Socials[0].URL = "javascript:x" },
		"social path":    func(p *Profile) { p.Socials[0].URL = "/local" },
		"bad mailto":     func(p *Profile) { p.Socials[1].URL = "mailto:nope" },
		"long item":      func(p *Profile) { p.Skills[0].Items = []string{strings.Repeat("s", 41)} },
		"bad start":      func(p *Profile) { p.Experience[0].Start = "2024-13" },
		"bad end":        func(p *Profile) { p.Experience[0].End = "2024" },
		"end < start":    func(p *Profile) { p.Experience[0].End = "2023-01" },
		"missing role":   func(p *Profile) { p.Experience[0].Role = "" },
		"long seo title": func(p *Profile) { p.SEO.Title = strings.Repeat("t", 301) },
		"many socials":   func(p *Profile) { p.Socials = make([]SocialLink, 21) },
	}
	for name, mut := range cases {
		p := validProfile()
		mut(&p)
		if err := validate(&p); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestPartialFromGitHub(t *testing.T) {
	got := partialFromGitHub(ghUser{Login: "baoopn", Name: "Bao", Bio: "Builds things", HTMLURL: "https://github.com/baoopn",
		Avatar: "https://avatars.githubusercontent.com/u/1", Blog: "baoopn.com"})
	b, _ := json.Marshal(got)
	s := string(b)
	for _, want := range []string{`"name":"Bao"`, `"headline":"Builds things"`, `"handle":"@baoopn"`, `"url":"https://baoopn.com"`, `"icon":"website"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in %s", want, s)
		}
	}
	if _, ok := got["email"]; ok {
		t.Error("email should be absent when not public")
	}
}

func TestProfileStoreAndHandlers(t *testing.T) {
	pool := testdb.New(t)
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/baoopn" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"login":"baoopn","name":"Bao Nguyen","bio":"Dev","email":"me@baoopn.com","html_url":"https://github.com/baoopn"}`))
	}))
	t.Cleanup(gh.Close)
	seed := fstest.MapFS{"profile.json": {Data: []byte(`{"name":"Seed Name","headline":"Seeded","bio_md":"Seed *bio*","socials":[]}`)}}
	m := New(Deps{DB: pool, Log: slog.New(slog.DiscardHandler), Seed: seed, GitHubUser: "baoopn", APIBase: gh.URL})
	pub, adm := chi.NewRouter(), chi.NewRouter()
	m.Public(pub)
	m.Admin(adm)
	do := func(r chi.Router, method, path string, body any, out any) int {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, &buf))
		if out != nil {
			_ = json.Unmarshal(rec.Body.Bytes(), out)
		}
		return rec.Code
	}

	// Absent key: falls back to the rendered seed.
	var got struct{ Data Profile }
	if code := do(pub, "GET", "/profile", nil, &got); code != 200 || got.Data.Name != "Seed Name" ||
		!strings.Contains(got.Data.BioHTML, "<em>bio</em>") || got.Data.Experience == nil {
		t.Fatalf("seed fallback %d %+v", code, got.Data)
	}

	ctx := context.Background()
	if err := m.EnsureSeeded(ctx); err != nil {
		t.Fatal(err)
	}
	in := validProfile()
	in.BioHTML = "<script>ignored</script>"
	var saved struct{ Data Profile }
	if code := do(adm, "PUT", "/profile", in, &saved); code != 200 {
		t.Fatalf("put %d", code)
	}
	if !strings.Contains(saved.Data.BioHTML, "<strong>world</strong>") || saved.Data.UpdatedAt == nil ||
		!strings.Contains(saved.Data.Experience[0].SummaryHTML, "<em>things</em>") {
		t.Fatalf("rendered %+v", saved.Data)
	}
	// EnsureSeeded never overwrites an existing profile.
	if err := m.EnsureSeeded(ctx); err != nil {
		t.Fatal(err)
	}
	do(pub, "GET", "/profile", nil, &got)
	if got.Data.Name != "Bao Nguyen" {
		t.Fatalf("overwritten: %+v", got.Data)
	}

	bad := validProfile()
	bad.Name = ""
	if code := do(adm, "PUT", "/profile", bad, nil); code != 422 {
		t.Fatalf("validation %d", code)
	}

	var partial struct{ Data map[string]any }
	if code := do(adm, "POST", "/profile/github", nil, &partial); code != 200 ||
		partial.Data["headline"] != "Dev" || partial.Data["email"] != "me@baoopn.com" {
		t.Fatalf("github %d %+v", code, partial.Data)
	}
	do(pub, "GET", "/profile", nil, &got)
	if got.Data.Headline != "Dev" || got.Data.Name != "Bao Nguyen" {
		t.Fatalf("github import must not save: %+v", got.Data)
	}
}

func TestEnsureSeededFromEmptyDB(t *testing.T) {
	pool := testdb.New(t)
	m := New(Deps{DB: pool, Log: slog.New(slog.DiscardHandler),
		Seed: fstest.MapFS{"profile.json": {Data: []byte(`{"name":"Bao","bio_md":"hi"}`)}}})
	if err := m.EnsureSeeded(context.Background()); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err := pool.QueryRow(context.Background(), `SELECT value FROM settings WHERE key = 'profile'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"bio_html": "<p>hi</p>`)) && !bytes.Contains(raw, []byte(`"bio_html":"<p>hi</p>`)) {
		t.Fatalf("stored %s", raw)
	}
}

func TestRealSeedIsValid(t *testing.T) {
	m := New(Deps{Log: slog.New(slog.DiscardHandler), Seed: seed.FS})
	p := m.seed
	if err := validate(&p); err != nil {
		t.Fatalf("seed/profile.json invalid: %v", err)
	}
	if p.Name != "Bao Nguyen" || len(p.Socials) != 4 || p.BioHTML == "" {
		t.Fatalf("seed %+v", p)
	}
}
