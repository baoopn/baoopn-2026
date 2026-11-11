package mediaurls

import (
	"context"
	"reflect"
	"strconv"
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

// A fallback rename follows every link form to the old file, and only it.
func TestRename(t *testing.T) {
	const old, other = "0123456789abcdef01234567.webp", "fedcba9876543210fedcba98.webp"
	newURL := pub + "/0123456789abcdef01234567.png"
	fn := Rename(old, newURL, pub, "https://baoopn.com/media", "")
	in := "![a](" + pub + "/" + old + ") ![b](/media/" + old + ") ![c](https://baoopn.com/media/" + old +
		") ![d](/media/" + other + ") ![e](/media/" + old + "x)"
	want := "![a](" + newURL + ") ![b](" + newURL + ") ![c](" + newURL +
		") ![d](/media/" + other + ") ![e](/media/" + old + "x)"
	if got := fn(in); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
	// Without a public domain the new URL is site-relative.
	if got := Rename(old, "/media/x.png")("[f](/media/" + old + ")"); got != "[f](/media/x.png)" {
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
	q(`INSERT INTO projects (slug, title, cover_url, logo_url, body_md, body_html) VALUES ('x', 'X', $1, $1, $2, 'old')`,
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
	if st.Parts != 1 || st.Posts != 1 || st.Projects != 3 || !st.Profile {
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
	var pcover, plogo, pbody string
	_ = pool.QueryRow(ctx, `SELECT cover_url, logo_url, body_html FROM projects`).Scan(&pcover, &plogo, &pbody)
	if pcover != want || plogo != want || !strings.Contains(pbody, want) {
		t.Fatalf("project: %q %q %q", pcover, plogo, pbody)
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
	if err != nil || !reflect.DeepEqual(st, Stats{}) {
		t.Fatalf("second run %+v %v", st, err)
	}
}

// An admin save that lands between the rewrite's read and its write wins:
// the rewrite skips that row instead of writing back the old text.
func TestRewriteSkipsConcurrentSave(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ref := "/media/" + file
	q := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	q(`INSERT INTO posts (id, slug, title, cover_url) VALUES (1, 'p', 'P', $1)`, ref)
	q(`INSERT INTO post_parts (post_id, part_no, content_md, content_html) VALUES (1, 1, $1, 'old')`, "![a]("+ref+")")
	q(`INSERT INTO projects (slug, title, cover_url, body_md, body_html) VALUES ('x', 'X', '', $1, 'old')`,
		"![b]("+ref+")")
	if err := settings.Put(ctx, pool, profile.SettingsKey, profile.Profile{Name: "Bao", BioMD: "bio ![p](" + ref + ")"}); err != nil {
		t.Fatal(err)
	}

	// The "admin" saves each row the first time the rewrite looks at it.
	saved := map[string]bool{}
	saves := map[string]func(){
		"![a](" + ref + ")": func() { q(`UPDATE post_parts SET content_md = 'admin part'`) },
		ref:                 func() { q(`UPDATE posts SET cover_url = 'https://x.test/admin.png'`) },
		"![b](" + ref + ")": func() { q(`UPDATE projects SET body_md = 'admin body'`) },
		"bio ![p](" + ref + ")": func() {
			if err := settings.Put(ctx, pool, profile.SettingsKey, profile.Profile{Name: "Admin", BioMD: "admin bio"}); err != nil {
				t.Fatal(err)
			}
		},
	}
	fn := func(s string) string {
		if save, ok := saves[s]; ok && !saved[s] {
			saved[s] = true
			save()
		}
		return LegacyToPublic(pub)(s)
	}
	st, err := Rewrite(ctx, pool, fn)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st, Stats{}) {
		t.Fatalf("stats %+v, want nothing written", st)
	}
	var part, cover, body string
	_ = pool.QueryRow(ctx, `SELECT content_md FROM post_parts`).Scan(&part)
	_ = pool.QueryRow(ctx, `SELECT cover_url FROM posts`).Scan(&cover)
	_ = pool.QueryRow(ctx, `SELECT body_md FROM projects`).Scan(&body)
	if part != "admin part" || cover != "https://x.test/admin.png" || body != "admin body" {
		t.Fatalf("admin saves lost: %q %q %q", part, cover, body)
	}
	var got profile.Profile
	_, _ = settings.Get(ctx, pool, profile.SettingsKey, &got)
	if got.Name != "Admin" || got.BioMD != "admin bio" {
		t.Fatalf("profile save lost: %+v", got)
	}
}

// A save that keeps the link is re-read and rewritten: the admin's text and
// the new link both land. A row that changes on every attempt is reported.
func TestRewriteRetriesConcurrentSave(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	ref := "/media/" + file
	q := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	q(`INSERT INTO posts (id, slug, title) VALUES (1, 'p', 'P'), (2, 'q', 'Q')`)
	q(`INSERT INTO post_parts (post_id, part_no, content_md, content_html) VALUES (1, 1, $1, 'old'), (2, 1, $2, 'old')`,
		"![a]("+ref+")", "busy ![b]("+ref+")")
	var busyID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM post_parts WHERE post_id = 2`).Scan(&busyID); err != nil {
		t.Fatal(err)
	}

	saved, busy := false, 0
	fn := func(s string) string {
		switch {
		case s == "![a]("+ref+")" && !saved: // one save between read and write
			saved = true
			q(`UPDATE post_parts SET content_md = $1 WHERE post_id = 1`, "edited ![a]("+ref+")")
		case strings.HasPrefix(s, "busy"): // saved before every write
			busy++
			q(`UPDATE post_parts SET content_md = $1 WHERE post_id = 2`, "busy "+strconv.Itoa(busy)+" ![b]("+ref+")")
		}
		return LegacyToPublic(pub)(s)
	}
	st, err := Rewrite(ctx, pool, fn)
	if err != nil {
		t.Fatal(err)
	}
	if st.Parts != 1 || !reflect.DeepEqual(st.Skipped, []string{"post_parts:" + strconv.FormatInt(busyID, 10)}) {
		t.Fatalf("stats %+v", st)
	}
	if busy != casAttempts {
		t.Fatalf("busy row tried %d times, want %d", busy, casAttempts)
	}
	var md string
	_ = pool.QueryRow(ctx, `SELECT content_md FROM post_parts WHERE post_id = 1`).Scan(&md)
	if md != "edited ![a]("+pub+"/"+file+")" {
		t.Fatalf("part = %q", md)
	}
}
