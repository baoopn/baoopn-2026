package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"
)

// allKeys returns every candidate key plus a row key for each id, so a run
// is asked to delete referenced rows too and must refuse.
func allKeys(p OrphanPlan, ids ...int64) []string {
	out := keys(p)
	for _, id := range ids {
		out = append(out, rowKey(id))
	}
	return out
}

// Referenced files survive a real run, whatever URL form names them and
// even when a run asks for them by key.
func TestOrphanRunNeverDeletesReferenced(t *testing.T) {
	s, f := newOrphanS3Store(t)
	ctx := context.Background()
	old := time.Now().Add(-30 * day)
	forms := []string{
		"/media/%s.webp",
		"/media/%s.webp?w=640&h=480",
		"/media/%s.webp#top",
		"https://media.example.com/%s.webp",
		"https://baoopn.com/media/%s.webp",
		"//baoopn.com/media/%s.webp",
		"media/%s.webp",
		"%s.webp",
		"/media/%s.WEBP",
		`<img srcset="/media/x.webp 1x, /media/%s.webp 2x">`,
		"![alt](/media/%s.webp \"title\")",
		"url('/media/%s.webp')",
		"[%s]",
		"/media/%s.png", // fallback renamed .webp to .png; content keeps the old name
	}
	var ids []int64
	for i, form := range forms {
		n := i + 1
		ids = append(ids, addRow(t, s, n, rowOpt{age: 30 * day, ext: ".webp", file: true}))
		f.putObj(stemN(n)+".webp", []byte("bucket copy differs"), old) // not a local_copy candidate
		exec(t, s, `INSERT INTO posts (slug, title, excerpt) VALUES ($1, 'T', $2)`,
			fmt.Sprintf("p%d", n), fmt.Sprintf(form, stemN(n)))
	}
	// Referenced bucket-only object and local-only file (no row).
	f.putObj(stemN(50)+".webp", []byte("x"), old)
	writeAged(t, filepath.Join(s.dir, stemN(51)+".png"), 30*day)
	exec(t, s, `INSERT INTO settings (key, value) VALUES ('profile', $1::jsonb)`,
		fmt.Sprintf(`{"avatar_url":"/media/%s.webp","cover_url":"/media/%s.png"}`, stemN(50), stemN(51)))
	control := addRow(t, s, 99, rowOpt{age: 30 * day})

	p := mustPlan(t, s)
	if got := keys(p); len(got) != 1 || got[0] != rowKey(control) {
		t.Fatalf("candidates = %v, want only the control row", got)
	}
	for _, trigger := range []string{TriggerAuto, TriggerManual} {
		if _, err := s.CleanOrphans(ctx, trigger, nil); err != nil {
			t.Fatal(err)
		}
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, allKeys(p, ids...))
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted.total() != 0 {
		t.Fatalf("forced run deleted %+v", res.Deleted)
	}
	for i, id := range ids {
		n := i + 1
		if _, err := s.Get(ctx, id); err != nil {
			t.Errorf("form %q: row deleted", forms[i])
		}
		if !f.has(stemN(n) + ".webp") {
			t.Errorf("form %q: object deleted", forms[i])
		}
		if _, err := os.Stat(filepath.Join(s.dir, stemN(n)+".webp")); err != nil {
			t.Errorf("form %q: local file deleted", forms[i])
		}
	}
	if !f.has(stemN(50) + ".webp") {
		t.Error("referenced bucket-only object deleted")
	}
	if _, err := os.Stat(filepath.Join(s.dir, stemN(51)+".png")); err != nil {
		t.Error("referenced local-only file deleted")
	}
	if _, err := s.Get(ctx, control); err == nil {
		t.Error("control row kept: the auto run should have deleted it")
	}
}

// Rows with a job a worker or the janitor still needs are never deleted,
// whether the run picks its own work or is handed their keys.
func TestOrphanRunNeverDeletesProcessing(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	ids := []int64{
		addRow(t, s, 1, rowOpt{age: 30 * day, status: StatusProcessing}),
		addRow(t, s, 2, rowOpt{age: 30 * day, status: StatusProcessing, job: "queued"}),
		addRow(t, s, 3, rowOpt{age: 30 * day, status: StatusProcessing, job: "running"}),
		addRow(t, s, 4, rowOpt{age: 30 * day, status: StatusReady, job: "queued"}), // retry pending
		addRow(t, s, 5, rowOpt{age: 30 * day, status: StatusFailed, job: "failed"}),
	}
	writeAged(t, filepath.Join(s.originalsDir(), stemN(2)), 30*day)
	writeAged(t, filepath.Join(s.originalsDir(), stemN(3)), 30*day)
	writeAged(t, filepath.Join(s.originalsDir(), stemN(5)), 30*day)

	p := mustPlan(t, s)
	if len(p.Candidates) != 0 || p.Kept.Processing != 4 || p.Kept.RetainedOriginal != 1 {
		t.Fatalf("plan = %v, kept %+v", keys(p), p.Kept)
	}
	forced := allKeys(p, ids...)
	for _, n := range []int{2, 3, 5} {
		forced = append(forced, KindOriginal+":"+stemN(n))
	}
	if _, err := s.CleanOrphans(ctx, TriggerManual, nil); err != nil {
		t.Fatal(err)
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, forced)
	if err != nil || res.Deleted.total() != 0 {
		t.Fatalf("forced run = %+v, %v", res, err)
	}
	if countRows(t, s) != len(ids) {
		t.Fatal("a processing row was deleted")
	}
	for _, n := range []int{2, 3, 5} {
		if _, err := os.Stat(filepath.Join(s.originalsDir(), stemN(n))); err != nil {
			t.Errorf("original %d deleted", n)
		}
	}

	// The job starts between the plan and the run: the re-check catches it.
	late := addRow(t, s, 9, rowOpt{age: 30 * day})
	if c := candidate(mustPlan(t, s), rowKey(late)); c == nil || !c.Eligible {
		t.Fatal("late row not eligible before its job")
	}
	exec(t, s, `UPDATE media SET status = 'processing' WHERE id = $1`, late)
	exec(t, s, `INSERT INTO media_jobs (media_id, status, original_content_type, original_size) VALUES ($1, 'queued', 'image/jpeg', 1)`, late)
	res, err = s.CleanOrphans(ctx, TriggerManual, []string{rowKey(late)})
	if err != nil || res.Deleted.Rows != 0 {
		t.Fatalf("late run = %+v, %v", res, err)
	}
}

// Nothing inside the grace period goes, for every kind, by key or not; and a
// file touched after the plan was made is re-checked and kept.
func TestOrphanRunNeverDeletesInGrace(t *testing.T) {
	s, f := newOrphanS3Store(t)
	ctx := context.Background()
	fresh := 6 * day
	id := addRow(t, s, 1, rowOpt{age: fresh, file: true})
	f.putObj(stemN(2)+".webp", []byte("x"), time.Now().Add(-fresh))
	writeAged(t, filepath.Join(s.dir, stemN(3)+".png"), fresh)
	writeAged(t, filepath.Join(s.originalsDir(), stemN(4)), 23*time.Hour)
	writeAged(t, filepath.Join(s.dir, ".up-abc"), 23*time.Hour)

	p := mustPlan(t, s)
	if p.Totals.Candidates != 5 || p.Totals.Eligible != 0 || p.Kept.InGrace != 5 {
		t.Fatalf("plan = %v totals %+v kept %+v", keys(p), p.Totals, p.Kept)
	}
	if _, err := s.CleanOrphans(ctx, TriggerManual, nil); err != nil {
		t.Fatal(err)
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, keys(p))
	if err != nil || res.Deleted.total() != 0 || len(res.Skipped) != 5 {
		t.Fatalf("forced run = %+v, %v", res, err)
	}
	for _, sk := range res.Skipped {
		if sk.Why != BlockedGrace {
			t.Errorf("skip %s why %q", sk.Key, sk.Why)
		}
	}
	if _, err := s.Get(ctx, id); err != nil || !f.has(stemN(2)+".webp") {
		t.Fatal("in-grace row or object deleted")
	}
	for _, path := range []string{filepath.Join(s.dir, stemN(3)+".png"), filepath.Join(s.originalsDir(), stemN(4)),
		filepath.Join(s.dir, ".up-abc")} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s deleted", filepath.Base(path))
		}
	}

	// Eligible when planned, touched (a save removed it from a post) before
	// the run: kept.
	oldID := addRow(t, s, 9, rowOpt{age: 30 * day})
	if c := candidate(mustPlan(t, s), rowKey(oldID)); c == nil || !c.Eligible {
		t.Fatal("old row not eligible")
	}
	exec(t, s, `UPDATE media SET touched_at = now() WHERE id = $1`, oldID)
	res, err = s.CleanOrphans(ctx, TriggerManual, []string{rowKey(oldID)})
	// The run's own plan sees the bump (grace); a bump after that plan is
	// caught by the re-check (touched). Either way the row stays.
	if err != nil || res.Deleted.Rows != 0 || len(res.Skipped) != 1 ||
		(res.Skipped[0].Why != BlockedGrace && res.Skipped[0].Why != "touched") {
		t.Fatalf("touched run = %+v, %v", res, err)
	}
	if _, err := s.Get(ctx, oldID); err != nil {
		t.Fatal("touched row deleted")
	}
}

// The dry run's would_delete and eligible list are exactly what the run
// without keys deletes, with a cap and a bucket local copy in play.
func TestOrphanDryRunExactWithCapAndLocalCopy(t *testing.T) {
	s, f := newOrphanS3Store(t)
	s.orphanMax = 3
	ctx := context.Background()
	old := time.Now().Add(-30 * day)
	for n := 1; n <= 3; n++ {
		addRow(t, s, n, rowOpt{age: time.Duration(40-n) * day})
		f.putObj(stemN(n)+".webp", []byte("0123456789"), old)
	}
	for n := 10; n <= 12; n++ {
		f.putObj(stemN(n)+".webp", []byte("x"), time.Now().Add(-time.Duration(50-n)*day))
	}
	// A referenced ready row whose local copy duplicates the bucket object:
	// the oldest eligible candidate, but a keyless run must skip it.
	addRow(t, s, 20, rowOpt{age: 60 * day, file: true})
	if err := os.Chtimes(filepath.Join(s.dir, stemN(20)+".webp"), old.Add(-60*day), old.Add(-60*day)); err != nil {
		t.Fatal(err)
	}
	f.putObj(stemN(20)+".webp", []byte("0123456789"), old)
	exec(t, s, `INSERT INTO posts (slug, title, cover_url) VALUES ('a', 'A', $1)`, "/media/"+stemN(20)+".webp")

	p := mustPlan(t, s)
	var want []string
	for _, c := range p.Candidates {
		if c.Eligible && c.Kind != KindLocalCopy {
			want = append(want, c.Name)
		}
	}
	want = want[:min(len(want), s.orphanMax)]

	res, err := s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted.total() != p.Totals.WouldDelete {
		t.Errorf("plan would_delete = %d, run deleted %d (%+v)", p.Totals.WouldDelete, res.Deleted.total(), res.Deleted)
	}
	after := mustPlan(t, s)
	var gone []string
	for _, c := range p.Candidates {
		if c.Kind != KindLocalCopy && candidate(after, c.Key) == nil {
			gone = append(gone, c.Name)
		}
	}
	sort.Strings(want)
	sort.Strings(gone)
	if !slices.Equal(want, gone) {
		t.Errorf("deleted %v, want the plan's first %d eligible %v", gone, s.orphanMax, want)
	}
	if _, err := os.Stat(filepath.Join(s.dir, stemN(20)+".webp")); err != nil {
		t.Error("keyless run removed a local copy")
	}
}
