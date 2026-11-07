package mediaurls

import (
	"context"
	"strings"
	"testing"

	"github.com/baoopn/baoopn-2026/internal/profile"
	"github.com/baoopn/baoopn-2026/internal/settings"
	"github.com/baoopn/baoopn-2026/internal/testdb"
)

const (
	pub  = "https://media.example.com"
	file = "0123456789abcdef01234567.png"
)

func TestLegacyToPublic(t *testing.T) {
	fn := LegacyToPublic(pub + "/")
	for in, want := range map[string]string{
		"/media/" + file:                              "https://media.example.com/" + file,
		"![a](/media/" + file + ")":                   "![a](https://media.example.com/" + file + ")",
		`<img src="/media/` + file + `">`:             `<img src="https://media.example.com/` + file + `">`,
		"see /media/" + file + ". and /media/" + file: "see " + pub + "/" + file + ". and " + pub + "/" + file,
		"/media/" + file + "?v=2#x":                   pub + "/" + file + "?v=2#x",
		"https://baoopn.com/media/" + file:            "https://baoopn.com/media/" + file, // absolute: not ours to touch
		"/blog/media/" + file:                         "/blog/media/" + file,
		"/media/" + file + "x":                        "/media/" + file + "x",
		"/media/" + file + "/more":                    "/media/" + file + "/more",
		"/media/0123456789ABCDEF01234567.png":         "/media/0123456789ABCDEF01234567.png",
		"/media/0123456789abcdef01234567.exe":         "/media/0123456789abcdef01234567.exe",
		"/media/short.png":                            "/media/short.png",
		"plain text about /media/ folders":            "plain text about /media/ folders",
		pub + "/" + file:                              pub + "/" + file, // already rewritten
	} {
		if got := fn(in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
		if got := fn(fn(in)); got != fn(in) {
			t.Errorf("%q: not idempotent", in)
		}
	}
}

func TestReplace(t *testing.T) {
	old := pub + "/0123456789abcdef01234567.webp"
	fn := Replace(old, pub+"/0123456789abcdef01234567.jpg")
	if got := fn("![x](" + old + ") and " + old + "x"); got != "![x]("+pub+"/0123456789abcdef01234567.jpg) and "+old+"x" {
		t.Fatalf("got %q", got)
	}
}

func TestRewriteDB(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ref := "/media/" + file
	q := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	q(`INSERT INTO posts (id, slug, title, cover_url) VALUES (1, 'p', 'P', $1), (2, 'q', 'Q', 'https://x.test/a.png')`, ref)
	q(`INSERT INTO post_parts (post_id, part_no, content_md, content_html) VALUES
		(1, 1, $1, 'old'), (2, 1, 'no media here', 'keep')`, "## Shot\n\n![shot]("+ref+")\n")
	q(`INSERT INTO projects (slug, title, cover_url, body_md, body_html) VALUES ('x', 'X', $1, $2, 'old')`,
		ref, "Body ![b]("+ref+")")
	p := profile.Profile{Name: "Bao", AvatarURL: ref, CoverURL: ref, ResumeURL: "/media/0123456789abcdef01234567.pdf",
		BioMD: "Hi ![me](" + ref + ")", SEO: profile.SEO{OGImage: ref},
		Experience: []profile.ExperienceItem{{Role: "Dev", SummaryMD: "![logo](" + ref + ")"}}}
	if err := settings.Put(ctx, pool, profile.SettingsKey, p); err != nil {
		t.Fatal(err)
	}

	st, err := Rewrite(ctx, pool, LegacyToPublic(pub))
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts != 1 || st.Posts != 1 || st.Projects != 2 || !st.Profile {
		t.Fatalf("stats %+v", st)
	}
	want := pub + "/" + file
	var md, html, toc, cover string
	_ = pool.QueryRow(ctx, `SELECT pp.content_md, pp.content_html, pp.toc::text, p.cover_url FROM post_parts pp
		JOIN posts p ON p.id = pp.post_id WHERE p.id = 1`).Scan(&md, &html, &toc, &cover)
	if !strings.Contains(md, want) || !strings.Contains(html, `src="`+want+`"`) || !strings.Contains(toc, "shot") || cover != want {
		t.Fatalf("post: %q %q %q %q", md, html, toc, cover)
	}
	var keep string
	_ = pool.QueryRow(ctx, `SELECT content_html FROM post_parts WHERE post_id = 2`).Scan(&keep)
	if keep != "keep" {
		t.Fatal("untouched part re-rendered")
	}
	var pcover, pbody string
	_ = pool.QueryRow(ctx, `SELECT cover_url, body_html FROM projects`).Scan(&pcover, &pbody)
	if pcover != want || !strings.Contains(pbody, want) {
		t.Fatalf("project: %q %q", pcover, pbody)
	}
	var got profile.Profile
	_, _ = settings.Get(ctx, pool, profile.SettingsKey, &got)
	if got.AvatarURL != want || got.CoverURL != want || got.SEO.OGImage != want ||
		got.ResumeURL != pub+"/0123456789abcdef01234567.pdf" || !strings.Contains(got.BioHTML, want) ||
		!strings.Contains(got.Experience[0].SummaryHTML, want) {
		t.Fatalf("profile %+v", got)
	}

	// Second run: nothing left to change.
	st, err = Rewrite(ctx, pool, LegacyToPublic(pub))
	if err != nil || st != (Stats{}) {
		t.Fatalf("second run %+v %v", st, err)
	}
}
