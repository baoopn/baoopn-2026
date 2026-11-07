package blog

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	now := time.Now()
	in := PostInput{Title: "Hello, World! Part 2", Status: "draft", Parts: []PartInput{{}}}
	gen, err := validate(&in, now)
	if err != nil || !gen || in.Slug != "hello-world-part-2" {
		t.Fatalf("generated: %v %v %q", gen, err, in.Slug)
	}
	in = PostInput{Title: "日本語", Status: "draft", Parts: []PartInput{{}}}
	if _, err := validate(&in, now); err != nil || in.Slug != "post" {
		t.Fatalf("fallback slug: %v %q", err, in.Slug)
	}
	for _, bad := range []string{"Upper", "a--b", "-a", "a_b", strings.Repeat("a", 81)} {
		in = PostInput{Title: "x", Slug: bad, Status: "draft", Parts: []PartInput{{}}}
		if _, err := validate(&in, now); err == nil {
			t.Errorf("slug %q accepted", bad)
		}
	}
	in = PostInput{Title: "x", Slug: "ok-slug-1", Status: "draft", Parts: []PartInput{{}}}
	if gen, err := validate(&in, now); err != nil || gen {
		t.Fatalf("explicit: %v %v", gen, err)
	}
}

func TestSuffixed(t *testing.T) {
	if suffixed("a", 1) != "a" || suffixed("a", 2) != "a-2" || suffixed("a", 13) != "a-13" {
		t.Fatal("suffix")
	}
	long := strings.Repeat("ab-", 30)[:80]
	if s := suffixed(long, 10); len(s) > 80 || !strings.HasSuffix(s, "-10") || strings.Contains(s, "--") {
		t.Fatalf("long suffix %q", s)
	}
}

func TestNormalizeTags(t *testing.T) {
	got, err := normalizeTags([]string{" Go ", "go", "C#", "node.js", "", "c++"})
	if err != nil || strings.Join(got, ",") != "go,c#,node.js,c++" {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := normalizeTags([]string{"-bad"}); err == nil {
		t.Fatal("leading dash accepted")
	}
	if _, err := normalizeTags([]string{strings.Repeat("a", 31)}); err == nil {
		t.Fatal("long tag accepted")
	}
	many := make([]string, 11)
	for i := range many {
		many[i] = "t" + string(rune('a'+i))
	}
	if _, err := normalizeTags(many); err == nil {
		t.Fatal("11 tags accepted")
	}
	if got, _ := normalizeTags(nil); got == nil {
		t.Fatal("nil tags should become empty slice")
	}
}

func TestStateOf(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	cases := []struct {
		status string
		at     *time.Time
		want   string
	}{
		{"draft", &past, "draft"},
		{"draft", nil, "draft"},
		{"published", &future, "scheduled"},
		{"published", &past, "published"},
		{"published", nil, "published"},
	}
	for _, c := range cases {
		if got := stateOf(c.status, c.at, now); got != c.want {
			t.Errorf("stateOf(%s) = %s, want %s", c.status, got, c.want)
		}
	}
}

func TestValidURL(t *testing.T) {
	good := []string{"", "/media/x.png", "https://cdn.baoopn.com/a.jpg", "http://example.com"}
	bad := []string{"//evil.com/x", "/\\evil.com", "javascript:alert(1)", "ftp://x.com", "https://", "relative.png",
		"https://x.com/" + strings.Repeat("a", 500), "/a b"}
	for _, g := range good {
		if !validURL(g) {
			t.Errorf("%q rejected", g)
		}
	}
	for _, b := range bad {
		if validURL(b) {
			t.Errorf("%q accepted", b)
		}
	}
}

func TestValidatePublishedDefaultsAndLimits(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	in := PostInput{Title: "t", Status: "published", Parts: []PartInput{
		{Title: "  One  ", ContentMD: "First para **bold**.\n\nSecond."}, {ContentMD: "Other part."}}}
	if _, err := validate(&in, now); err != nil {
		t.Fatal(err)
	}
	if in.PublishedAt == nil || !in.PublishedAt.Equal(now) || in.Excerpt != "First para bold." || in.Parts[0].Title != "One" {
		t.Fatalf("defaults: %v %q %q", in.PublishedAt, in.Excerpt, in.Parts[0].Title)
	}
	single := []PartInput{{}}
	tooMany := make([]PartInput, 21)
	checks := []PostInput{
		{Title: "", Status: "draft", Parts: single},
		{Title: "t", Status: "live", Parts: single},
		{Title: "t", Status: "draft"},
		{Title: "t", Status: "draft", Parts: tooMany},
		{Title: "t", Status: "draft", Parts: []PartInput{{Title: strings.Repeat("p", 201)}}},
		{Title: "t", Status: "draft", Parts: []PartInput{{}, {ContentMD: strings.Repeat("c", 200_001)}}},
		{Title: "t", Status: "draft", Parts: single, CoverURL: "//x"},
		{Title: "t", Status: "draft", Parts: single, Excerpt: strings.Repeat("e", 501)},
	}
	for i, c := range checks {
		var ve *ValidationError
		if _, err := validate(&c, now); !errors.As(err, &ve) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestValidateCategory(t *testing.T) {
	now := time.Now()
	for in, want := range map[string]string{"": "tech", " ": "tech", "tech": "tech", "life": "life", " life ": "life"} {
		p := PostInput{Title: "t", Status: "draft", Category: in, Parts: []PartInput{{}}}
		if _, err := validate(&p, now); err != nil || p.Category != want {
			t.Errorf("category %q: %v %q", in, err, p.Category)
		}
	}
	for _, bad := range []string{"Tech", "food", "all"} {
		p := PostInput{Title: "t", Status: "draft", Category: bad, Parts: []PartInput{{}}}
		var ve *ValidationError
		if _, err := validate(&p, now); !errors.As(err, &ve) {
			t.Errorf("category %q accepted: %v", bad, err)
		}
	}
}

func TestEscapeLike(t *testing.T) {
	if got := escapeLike(`50%_off\`); got != `50\%\_off\\` {
		t.Fatalf("escape %q", got)
	}
}
