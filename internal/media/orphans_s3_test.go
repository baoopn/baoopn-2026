package media

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newOrphanS3Store(t *testing.T) (*Store, *fakeS3) {
	t.Helper()
	f, srv := newFakeS3(t, "media-bucket")
	return newS3Store(t, s3Cfg(srv, "https://media.example.com"), true), f
}

func TestOrphanBucket(t *testing.T) {
	s, f := newOrphanS3Store(t)
	ctx := context.Background()
	old := time.Now().Add(-30 * day)
	body := []byte("0123456789")
	stray := stemN(1) + ".webp"
	f.putObj(stray, body, old)                      // no row: deleted
	f.putObj(stemN(2)+".webp", body, time.Now())    // fresh: grace
	f.putObj("v3/"+stemN(3)+".png", body, old)      // under a prefix: foreign
	f.putObj("favicon.ico", body, old)              // not a stored name: foreign
	f.putObj(stemN(4)+".png", body, old)            // has a row
	f.putObj(stemN(5)+".webp", body, old)           // referenced, no row
	addRow(t, s, 4, rowOpt{age: day, ext: ".webp"}) // row (in grace) for stem 4
	exec(t, s, `INSERT INTO posts (slug, title, cover_url) VALUES ('a', 'A', $1)`,
		"https://media.example.com/"+stemN(5)+".webp")

	p := mustPlan(t, s)
	if p.Storage != "bucket" || p.Truncated {
		t.Fatalf("plan = %+v", p)
	}
	if c := candidate(p, "object:"+stray); c == nil || !c.Eligible {
		t.Fatalf("stray = %+v (all %v)", c, keys(p))
	}
	if c := candidate(p, "object:"+stemN(2)+".webp"); c == nil || c.Blocked != BlockedGrace {
		t.Fatalf("fresh = %+v", c)
	}
	if len(p.Candidates) != 3 { // stray, fresh, and stem 4's row (in grace)
		t.Fatalf("candidates = %v", keys(p))
	}
	if p.Kept.Foreign != 2 || p.Kept.ForeignBytes != 20 || p.Kept.Referenced != 1 {
		t.Fatalf("kept = %+v", p.Kept)
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil || res.Deleted.Objects != 1 || res.Errors != 0 {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	for key, want := range map[string]bool{stray: false, stemN(2) + ".webp": true, "v3/" + stemN(3) + ".png": true,
		"favicon.ico": true, stemN(4) + ".png": true, stemN(5) + ".webp": true} {
		if f.has(key) != want {
			t.Errorf("%s in bucket = %v, want %v", key, !want, want)
		}
	}
}

func TestOrphanBucketPagingAndTruncation(t *testing.T) {
	s, f := newOrphanS3Store(t)
	old := time.Now().Add(-30 * day)
	for n := range 5 {
		f.putObj(stemN(n)+".webp", []byte("x"), old)
	}
	f.pageSize = 2
	if p := mustPlan(t, s); p.Truncated || len(p.Candidates) != 5 {
		t.Fatalf("paged plan: truncated %v, %v", p.Truncated, keys(p))
	}
	// A partial listing cannot tell orphans from files it did not see.
	s.listMax = 3
	addRow(t, s, 9, rowOpt{age: 30 * day})
	p := mustPlan(t, s)
	if !p.Truncated || len(p.Candidates) != 1 || p.Candidates[0].Kind != KindRow || len(p.Missing) != 0 {
		t.Fatalf("truncated plan: %v %v", p.Truncated, keys(p))
	}
}

// A ready row's local copy (left by the move to the bucket) is a separate,
// opt-in candidate: the automatic run never deletes it.
func TestOrphanLocalCopy(t *testing.T) {
	s, f := newOrphanS3Store(t)
	ctx := context.Background()
	id := addRow(t, s, 1, rowOpt{age: 30 * day, file: true})
	exec(t, s, `INSERT INTO posts (slug, title, cover_url) VALUES ('a', 'A', $1)`, "/media/"+stemN(1)+".webp")
	name := stemN(1) + ".webp"
	f.putObj(name, []byte("0123456789"), time.Now()) // same size as the local file
	addRow(t, s, 2, rowOpt{age: 30 * day, file: true})
	f.putObj(stemN(2)+".webp", []byte("different size"), time.Now())

	p := mustPlan(t, s)
	c := candidate(p, "local_copy:"+name)
	if c == nil || !c.Eligible || c.MediaID != id {
		t.Fatalf("local copy = %+v (all %v)", c, keys(p))
	}
	if candidate(p, "local_copy:"+stemN(2)+".webp") != nil {
		t.Fatal("a local file whose bucket copy differs is a candidate")
	}
	res, err := s.CleanOrphans(ctx, TriggerAuto, nil)
	if err != nil || res.Deleted.LocalCopies != 0 {
		t.Fatalf("auto = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, name)); err != nil {
		t.Fatal("auto run removed the local copy")
	}
	res, err = s.CleanOrphans(ctx, TriggerManual, []string{"local_copy:" + name})
	if err != nil || res.Deleted.LocalCopies != 1 {
		t.Fatalf("manual = %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, name)); !os.IsNotExist(err) {
		t.Fatal("local copy still there")
	}
	if !f.has(name) {
		t.Fatal("bucket object removed with the local copy")
	}
	if _, err := s.Get(ctx, id); err != nil {
		t.Fatal("row removed with the local copy")
	}
}

// A row whose object was already removed by hand still cleans up fine; its
// local copy and siblings go first.
func TestOrphanBucketRowIdempotent(t *testing.T) {
	s, f := newOrphanS3Store(t)
	ctx := context.Background()
	id := addRow(t, s, 1, rowOpt{age: 30 * day, ext: ".png", file: true})
	f.putObj(stemN(1)+".webp", []byte("sibling"), time.Now().Add(-30*day))
	res, err := s.CleanOrphans(ctx, TriggerManual, []string{rowKey(id)})
	if err != nil || res.Deleted.Rows != 1 || res.Errors != 0 {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	if f.has(stemN(1)+".webp") || f.has(stemN(1)+".png") {
		t.Fatal("sibling object kept")
	}
	if _, err := os.Stat(filepath.Join(s.dir, stemN(1)+".png")); !os.IsNotExist(err) {
		t.Fatal("local copy kept")
	}
	res, err = s.CleanOrphans(ctx, TriggerManual, []string{rowKey(id)})
	if err != nil || res.Deleted.total() != 0 || res.AlreadyGone != 1 {
		t.Fatalf("second clean = %+v, %v", res, err)
	}
}

// A database that does not own the bucket (a fresh or staging database
// pointed at the production bucket) sees every object as an orphan. The
// automatic run must not delete them.
func TestOrphanBrakeForeignBucket(t *testing.T) {
	s, f := newOrphanS3Store(t)
	ctx := context.Background()
	for n := range 20 {
		f.putObj(stemN(n)+".webp", []byte("x"), time.Now().Add(-30*day))
	}
	if p := mustPlan(t, s); p.Totals.Eligible != 20 {
		t.Fatalf("eligible = %d", p.Totals.Eligible)
	}
	res, err := s.CleanOrphans(ctx, TriggerAuto, nil)
	if err != nil || res.Reason != "brake" || res.Deleted.total() != 0 {
		t.Fatalf("auto run = %+v, %v", res, err)
	}
	for n := range 20 {
		if !f.has(stemN(n) + ".webp") {
			t.Fatalf("object %d deleted", n)
		}
	}

	// With rows present, deleting most of the stored files still brakes.
	for n := range 3 {
		addRow(t, s, 100+n, rowOpt{age: day})
	}
	if res, err := s.CleanOrphans(ctx, TriggerAuto, nil); err != nil || res.Reason != "brake" {
		t.Fatalf("auto run with rows = %+v, %v", res, err)
	}
}
