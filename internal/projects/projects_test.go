package projects

import (
	"strings"
	"testing"
	"time"
)

func TestPublicMapping(t *testing.T) {
	pushed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("x", 3600))
	p := project{Slug: "a", Title: "A", GHDescription: "gh desc", GHTopics: []string{"go", "web"}, GHLanguage: "Go",
		GHHomepage: "https://a.dev", GHPushedAt: &pushed, ReadmeHTML: "<p>readme</p>"}
	s := p.summary()
	if s.Summary != "gh desc" || strings.Join(s.Tech, ",") != "go,web" || s.DemoURL != "https://a.dev" ||
		s.PushedAt.Location() != time.UTC {
		t.Fatalf("fallbacks %+v", s)
	}
	d := p.detail()
	if d.BodySource != "readme" || d.BodyHTML != "<p>readme</p>" || d.Languages == nil {
		t.Fatalf("readme %+v", d)
	}

	p.Summary, p.Tech, p.DemoURL, p.BodyMD, p.BodyHTML = "mine", []string{"Svelte"}, "https://demo", "x", "<p>case</p>"
	s = p.summary()
	if s.Summary != "mine" || strings.Join(s.Tech, ",") != "Svelte" || s.DemoURL != "https://demo" {
		t.Fatalf("overrides %+v", s)
	}
	if d := p.detail(); d.BodySource != "case_study" || d.BodyHTML != "<p>case</p>" {
		t.Fatalf("case study %+v", d)
	}

	q := project{GHLanguage: "Rust"}
	if strings.Join(q.summary().Tech, ",") != "Rust" {
		t.Fatal("language fallback")
	}
	q.GHLanguage = ""
	if tech := q.summary().Tech; tech == nil || len(tech) != 0 {
		t.Fatal("empty tech must be []")
	}
	if d := q.detail(); d.BodySource != "none" || d.BodyHTML != "" || d.Topics == nil {
		t.Fatalf("none %+v", d)
	}
}

func TestNextLink(t *testing.T) {
	h := `<https://api.github.com/user/1/repos?page=2>; rel="next", <https://api.github.com/user/1/repos?page=5>; rel="last"`
	if got := nextLink(h); got != "https://api.github.com/user/1/repos?page=2" {
		t.Fatalf("got %q", got)
	}
	if nextLink(`<https://x/?page=1>; rel="prev", <https://x/?page=5>; rel="last"`) != "" || nextLink("") != "" || nextLink("garbage") != "" {
		t.Fatal("no next expected")
	}
}

func TestReadmeBases(t *testing.T) {
	l, i := readmeBases("bao/repo", "main")
	if l != "https://github.com/bao/repo/blob/main" || i != "https://raw.githubusercontent.com/bao/repo/main" {
		t.Fatalf("%s %s", l, i)
	}
	if l, _ := readmeBases("bao/repo", ""); l != "https://github.com/bao/repo/blob/HEAD" {
		t.Fatalf("default branch %s", l)
	}
}

func ptr[T any](v T) *T { return &v }

func TestPatchApply(t *testing.T) {
	p := project{Title: "Old", BodyMD: "same"}
	changed, err := Patch{Title: ptr(" New "), BodyMD: ptr("same"), Tech: ptr([]string{" Go ", "", "SQL"}),
		SortOrder: ptr(5), Visible: ptr(true)}.apply(&p)
	if err != nil || changed || p.Title != "New" || strings.Join(p.Tech, ",") != "Go,SQL" || p.SortOrder != 5 || !p.Visible {
		t.Fatalf("apply %v %v %+v", changed, err, p)
	}
	if changed, _ := (Patch{BodyMD: ptr("different")}).apply(&p); !changed {
		t.Fatal("body change not detected")
	}
	bad := []Patch{
		{Title: ptr("")},
		{Slug: ptr("Bad Slug")},
		{Summary: ptr(strings.Repeat("s", 501))},
		{Tech: ptr(make([]string, 0))}, // valid, checked below
		{Tech: ptr([]string{strings.Repeat("t", 41)})},
		{Period: ptr(strings.Repeat("p", 51))},
		{CoverURL: ptr("javascript:x")},
		{DemoURL: ptr("//evil")},
		{PostSlug: ptr("No")},
		{SortOrder: ptr(-1)},
		{SortOrder: ptr(100_001)},
	}
	for i, b := range bad {
		_, err := b.apply(&project{})
		if i == 3 {
			if err != nil {
				t.Errorf("empty tech rejected: %v", err)
			}
			continue
		}
		if err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = "x"
	}
	if _, err := (Patch{Tech: &many}).apply(&project{}); err == nil {
		t.Fatal("21 tech items accepted")
	}
}

func TestUniqueSlug(t *testing.T) {
	taken := map[string]bool{"my-repo": true, "my-repo-2": true}
	if s := uniqueSlug("My_Repo", taken); s != "my-repo-3" {
		t.Fatalf("got %s", s)
	}
	if s := uniqueSlug("___", map[string]bool{}); s != "project" {
		t.Fatalf("got %s", s)
	}
}
