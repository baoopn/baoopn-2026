package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/mediaurls"
)

const day = 24 * time.Hour

// stemN is a distinct valid stem per n.
func stemN(n int) string { return fmt.Sprintf("%024x", 0xabc000+n) }

type rowOpt struct {
	ext, status string
	age         time.Duration // touched_at and created_at this long ago
	job         string        // media_jobs.status; "" = no job
	origDeleted bool
	file        bool // write the stored file to MEDIA_DIR
}

// addRow inserts a media row for stemN(n) directly.
func addRow(t *testing.T, s *Store, n int, o rowOpt) int64 {
	t.Helper()
	if o.ext == "" {
		o.ext = ".webp"
	}
	if o.status == "" {
		o.status = StatusReady
	}
	name := stemN(n) + o.ext
	var id int64
	err := s.db.QueryRow(context.Background(), `INSERT INTO media (filename, original, content_type, size_bytes,
		sha256, status, created_at, touched_at) VALUES ($1, 'pic.jpg', 'image/webp', 10, $2, $3,
		now() - make_interval(secs => $4), now() - make_interval(secs => $4)) RETURNING id`,
		name, fmt.Sprintf("sha-%d", n), o.status, o.age.Seconds()).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if o.job != "" {
		exec(t, s, `INSERT INTO media_jobs (media_id, status, original_content_type, original_size, original_deleted)
			VALUES ($1, $2, 'image/jpeg', 10, $3)`, id, o.job, o.origDeleted)
	}
	if o.file {
		writeAged(t, filepath.Join(s.dir, name), o.age)
	}
	return id
}

// writeAged writes a small file last modified age ago.
func writeAged(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Now().Add(-age)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

func mustPlan(t *testing.T, s *Store) OrphanPlan {
	t.Helper()
	p, err := s.PlanOrphans(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func candidate(p OrphanPlan, key string) *OrphanCandidate {
	for i := range p.Candidates {
		if p.Candidates[i].Key == key {
			return &p.Candidates[i]
		}
	}
	return nil
}

func keys(p OrphanPlan) []string {
	var out []string
	for _, c := range p.Candidates {
		out = append(out, c.Key)
	}
	return out
}

func rowKey(id int64) string { return fmt.Sprintf("row:%d", id) }

func orphanStore(t *testing.T, edit func(*Deps)) *Store {
	t.Helper()
	s, _ := newQueueStore(t, edit)
	return s
}

// Every place content can name a file keeps its row, in any URL form.
func TestOrphanReferenceLocations(t *testing.T) {
	s := orphanStore(t, nil)
	u := func(n int, ext string) string { return "/media/" + stemN(n) + ext }
	pub := func(n int) string { return "https://media.example.com/" + stemN(n) + ".webp" }
	site := func(n int) string { return "https://baoopn.com/media/" + stemN(n) + ".webp" }
	refs := map[int]string{}
	for n, where := range []string{"part md", "part html only", "toc", "post cover", "draft post",
		"project cover", "project body_md", "project body_html", "project readme_html", "project demo_url",
		"avatar", "profile cover", "resume", "og_image", "bio_md", "experience", "social",
		"site settings", "other settings key", "seed.posts_2026"} {
		refs[n+1] = where
		addRow(t, s, n+1, rowOpt{age: 30 * day})
	}
	control := addRow(t, s, 99, rowOpt{age: 30 * day})

	exec(t, s, `INSERT INTO posts (id, slug, title, cover_url, status) VALUES
		(1, 'pub', 'P', $1, 'published'), (2, 'draft', 'D', '', 'draft')`, pub(4))
	exec(t, s, `INSERT INTO post_parts (post_id, part_no, content_md, content_html, toc) VALUES
		(1, 1, $1, '', '[]'), (1, 2, '', $2, '[]'), (1, 3, '', '', $3::jsonb), (2, 1, $4, '', '[]')`,
		"![a]("+u(1, ".webp")+")", `<img src="`+site(2)+`">`, `[{"id":"x","text":"`+u(3, ".png")+`"}]`,
		"draft ![b]("+pub(5)+")")
	exec(t, s, `INSERT INTO projects (slug, title, cover_url, body_md, body_html, readme_html, demo_url)
		VALUES ('p', 'P', $1, $2, $3, $4, $5)`, u(6, ".webp"), "![c]("+pub(7)+")", `<img src="`+u(8, ".png")+`">`,
		`<img src="`+site(9)+`">`, u(10, ".pdf"))
	profile := fmt.Sprintf(`{"avatar_url":%q,"cover_url":%q,"resume_url":%q,"seo":{"og_image":%q},
		"bio_md":%q,"experience":[{"summary_md":%q}],"socials":[{"url":%q}]}`,
		u(11, ".webp"), pub(12), u(13, ".pdf"), site(14), "hi ![](/media/"+stemN(15)+".png)", pub(16), u(17, ".svg"))
	exec(t, s, `INSERT INTO settings (key, value) VALUES ('profile', $1::jsonb), ('site', $2::jsonb),
		('anything', $3::jsonb), ('seed.posts_2026', $4::jsonb)`, profile,
		fmt.Sprintf(`{"logo":%q}`, pub(18)), fmt.Sprintf(`[%q]`, u(19, ".webp")), fmt.Sprintf(`{"cover":%q}`, stemN(20)))

	p := mustPlan(t, s)
	if got := keys(p); len(got) != 1 || got[0] != rowKey(control) {
		t.Fatalf("candidates = %v, want only the control row", got)
	}
	if p.Kept.Referenced != len(refs) {
		t.Fatalf("kept.referenced = %d, want %d", p.Kept.Referenced, len(refs))
	}
	if len(p.Missing) != 0 {
		t.Fatalf("missing = %v", p.Missing)
	}
}

func TestOrphanGrace(t *testing.T) {
	s := orphanStore(t, nil)
	young := addRow(t, s, 1, rowOpt{age: 6 * day})
	old := addRow(t, s, 2, rowOpt{age: 8 * day})
	p := mustPlan(t, s)
	if !p.Enabled || p.GraceSeconds != int64((7*day).Seconds()) || p.Storage != "disk" {
		t.Fatalf("plan header = %+v", p)
	}
	y, o := candidate(p, rowKey(young)), candidate(p, rowKey(old))
	if y == nil || y.Eligible || y.Blocked != BlockedGrace || y.EligibleAt == nil {
		t.Fatalf("young = %+v", y)
	}
	if d := time.Until(*y.EligibleAt); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("young eligible in %v, want about a day", d)
	}
	if o == nil || !o.Eligible || o.Blocked != "" || o.AgeSeconds < int64((8*day).Seconds())-60 {
		t.Fatalf("old = %+v", o)
	}
	if p.Totals.Candidates != 2 || p.Totals.Eligible != 1 || p.Totals.WouldDelete != 1 || p.Kept.InGrace != 1 {
		t.Fatalf("totals %+v kept %+v", p.Totals, p.Kept)
	}

	off := orphanStore(t, func(d *Deps) { d.OrphanGrace = -1 })
	addRow(t, off, 1, rowOpt{age: 30 * day})
	p = mustPlan(t, off)
	if p.Enabled || len(p.Candidates) != 1 || p.Candidates[0].Blocked != BlockedDisabled || p.Candidates[0].Eligible {
		t.Fatalf("disabled plan = %+v", p)
	}
	if _, err := off.CleanOrphans(context.Background(), TriggerManual, nil); err != ErrDisabled {
		t.Fatalf("Clean on disabled store = %v", err)
	}
}

// Uploading the same bytes again hands back the old row: it must start a
// fresh grace period, or "upload, then save" could lose the file.
func TestOrphanDedupeTouches(t *testing.T) {
	s := orphanStore(t, nil)
	m := mustSave(t, s, "logo.svg", svgDoc, nil)
	exec(t, s, `UPDATE media SET touched_at = now() - interval '30 days' WHERE id = $1`, m.ID)
	if c := candidate(mustPlan(t, s), rowKey(m.ID)); c == nil || !c.Eligible {
		t.Fatalf("aged upload = %+v", c)
	}
	if again := mustSave(t, s, "logo.svg", svgDoc, nil); again.ID != m.ID {
		t.Fatalf("dedupe gave %d, want %d", again.ID, m.ID)
	}
	if c := candidate(mustPlan(t, s), rowKey(m.ID)); c == nil || c.Eligible || c.Blocked != BlockedGrace {
		t.Fatalf("after re-upload = %+v", c)
	}
}

func TestOrphanProcessingExcluded(t *testing.T) {
	s := orphanStore(t, nil)
	old := 30 * day
	ids := []int64{
		addRow(t, s, 1, rowOpt{age: old, status: StatusProcessing, job: "running"}),
		addRow(t, s, 2, rowOpt{age: old, status: StatusProcessing}),
		addRow(t, s, 3, rowOpt{age: old, job: "queued"}),
		addRow(t, s, 4, rowOpt{age: old, job: "running"}),
		addRow(t, s, 5, rowOpt{age: old, status: StatusFailed, job: "failed"}),
	}
	gone := addRow(t, s, 6, rowOpt{age: old, status: StatusFailed, job: "failed", origDeleted: true})
	done := addRow(t, s, 7, rowOpt{age: old, job: "done", origDeleted: true})
	p := mustPlan(t, s)
	for _, id := range ids {
		if c := candidate(p, rowKey(id)); c != nil {
			t.Errorf("row %d is a candidate: %+v", id, c)
		}
	}
	for _, id := range []int64{gone, done} {
		if c := candidate(p, rowKey(id)); c == nil || !c.Eligible {
			t.Errorf("row %d = %+v, want eligible", id, c)
		}
	}
	if p.Kept.Processing != 4 || p.Kept.RetainedOriginal != 1 {
		t.Fatalf("kept = %+v", p.Kept)
	}
}

// The fallback renamed stem.webp to stem.png, but a post saved meanwhile
// still says stem.webp (its compare-and-swap rewrite was skipped).
func TestOrphanFallbackRenameKept(t *testing.T) {
	s := orphanStore(t, nil)
	id := addRow(t, s, 1, rowOpt{age: 30 * day, ext: ".png", file: true})
	writeAged(t, filepath.Join(s.dir, stemN(1)+".webp"), 30*day)
	exec(t, s, `INSERT INTO posts (id, slug, title, cover_url) VALUES (1, 'a', 'A', $1)`, "/media/"+stemN(1)+".webp")
	p := mustPlan(t, s)
	if len(p.Candidates) != 0 || p.Kept.Referenced != 1 {
		t.Fatalf("candidates %v, kept %+v (row %d)", keys(p), p.Kept, id)
	}
}

// Links before and after the move to the public bucket domain both count.
func TestOrphanLegacyRewriteKept(t *testing.T) {
	s := orphanStore(t, nil)
	addRow(t, s, 1, rowOpt{age: 30 * day})
	addRow(t, s, 2, rowOpt{age: 30 * day})
	legacy := "![a](/media/" + stemN(1) + ".webp)"
	moved := mediaurls.LegacyToPublic("https://media.example.com")("![b](/media/" + stemN(2) + ".webp)")
	if !strings.Contains(moved, "https://media.example.com/"+stemN(2)) {
		t.Fatalf("rewrite = %q", moved)
	}
	exec(t, s, `INSERT INTO posts (id, slug, title) VALUES (1, 'a', 'A')`)
	exec(t, s, `INSERT INTO post_parts (post_id, part_no, content_md) VALUES (1, 1, $1), (1, 2, $2)`, legacy, moved)
	if p := mustPlan(t, s); len(p.Candidates) != 0 || p.Kept.Referenced != 2 {
		t.Fatalf("candidates %v kept %+v", keys(p), p.Kept)
	}
}

func TestOrphanLocalDisk(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	stray := stemN(1) + ".png"
	writeAged(t, filepath.Join(s.dir, stray), 30*day)
	writeAged(t, filepath.Join(s.dir, stemN(2)+".png"), time.Hour) // fresh
	writeAged(t, filepath.Join(s.dir, ".up-old"), 25*time.Hour)
	writeAged(t, filepath.Join(s.dir, ".rq-new"), time.Hour)
	writeAged(t, filepath.Join(s.dir, "notes.txt"), 30*day) // foreign
	writeAged(t, filepath.Join(s.dir, "other", "x"), 30*day)
	writeAged(t, filepath.Join(s.originalsDir(), stemN(3)), 25*time.Hour) // no row
	addRow(t, s, 4, rowOpt{age: 30 * day, status: StatusFailed, job: "failed"})
	writeAged(t, filepath.Join(s.originalsDir(), stemN(4)), 30*day) // kept for a retry
	exec(t, s, `INSERT INTO posts (id, slug, title, cover_url) VALUES (1, 'a', 'A', $1)`, "/media/"+stemN(5)+".png")
	writeAged(t, filepath.Join(s.dir, stemN(5)+".png"), 30*day) // referenced, no row

	p := mustPlan(t, s)
	want := map[string]bool{"local:" + stray: true, "local:" + stemN(2) + ".png": false, "temp:.up-old": true,
		"temp:.rq-new": false, "original:" + stemN(3): true}
	if len(p.Candidates) != len(want) {
		t.Fatalf("candidates = %v", keys(p))
	}
	for k, eligible := range want {
		if c := candidate(p, k); c == nil || c.Eligible != eligible {
			t.Errorf("%s = %+v, want eligible=%v", k, c, eligible)
		}
	}
	if p.Kept.Foreign != 2 || p.Kept.Referenced != 1 || p.Kept.RetainedOriginal != 1 {
		t.Fatalf("kept = %+v", p.Kept)
	}

	res, err := s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted.Local != 1 || res.Deleted.Temp != 1 || res.Deleted.Originals != 1 || res.Errors != 0 || res.Bytes != 30 {
		t.Fatalf("result = %+v", res)
	}
	for name, exists := range map[string]bool{stray: false, ".up-old": false, ".rq-new": true, "notes.txt": true,
		stemN(5) + ".png": true, "originals/" + stemN(3): false, "originals/" + stemN(4): true} {
		if _, err := os.Stat(filepath.Join(s.dir, name)); (err == nil) != exists {
			t.Errorf("%s exists=%v, want %v", name, err == nil, exists)
		}
	}
}

// A deleted row takes its stored file, any sibling with the same stem and
// its original with it.
func TestOrphanRowFilesRemoved(t *testing.T) {
	s := orphanStore(t, nil)
	id := addRow(t, s, 1, rowOpt{age: 30 * day, ext: ".png", file: true, status: StatusFailed, job: "failed", origDeleted: true})
	writeAged(t, filepath.Join(s.dir, stemN(1)+".webp"), 30*day)
	writeAged(t, filepath.Join(s.originalsDir(), stemN(1)), time.Hour)
	res, err := s.CleanOrphans(context.Background(), TriggerManual, []string{rowKey(id)})
	if err != nil || res.Deleted.Rows != 1 || res.Errors != 0 {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	for _, name := range []string{stemN(1) + ".png", stemN(1) + ".webp", "originals/" + stemN(1)} {
		if _, err := os.Stat(filepath.Join(s.dir, name)); err == nil {
			t.Errorf("%s still exists", name)
		}
	}
	var n int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM media_jobs`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("jobs left = %d, %v", n, err)
	}
}

// A percent-encoded URL (a share link, an image proxy) puts an uppercase hex
// digit right before the stem. It still names the file.
func TestOrphanPercentEncodedReference(t *testing.T) {
	s := orphanStore(t, nil)
	id := addRow(t, s, 1, rowOpt{age: 30 * day})
	exec(t, s, `INSERT INTO posts (slug, title, excerpt) VALUES ('a', 'A', $1)`,
		"https://x.example/share?u=https%3A%2F%2Fbaoopn.com%2Fmedia%2F"+stemN(1)+".webp")
	if c := candidate(mustPlan(t, s), rowKey(id)); c != nil {
		t.Fatalf("percent-encoded reference not seen: %+v", c)
	}
}
