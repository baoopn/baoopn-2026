package settings

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

func TestPutTouchesOldAndNewMedia(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	oldStem, newStem, other := strings.Repeat("a", 24), strings.Repeat("b", 24), strings.Repeat("c", 24)
	if _, err := db.Exec(ctx, `INSERT INTO media (filename, content_type, size_bytes, touched_at)
		SELECT s || '.webp', 'image/webp', 1, now() - interval '30 days' FROM unnest($1::text[]) s`,
		[]string{oldStem, newStem, other}); err != nil {
		t.Fatal(err)
	}
	if err := Put(ctx, db, "profile", map[string]string{"avatar_url": "/media/" + oldStem + ".webp"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(ctx, `UPDATE media SET touched_at = now() - interval '30 days'`); err != nil {
		t.Fatal(err)
	}
	// The avatar is replaced: the old one (maybe used elsewhere next) and the
	// new one both start a fresh grace period.
	if err := Put(ctx, db, "profile", map[string]string{"avatar_url": "/media/" + newStem + ".webp"}); err != nil {
		t.Fatal(err)
	}
	for st, fresh := range map[string]bool{oldStem: true, newStem: true, other: false} {
		var at time.Time
		if err := db.QueryRow(ctx, `SELECT touched_at FROM media WHERE stem = $1`, st).Scan(&at); err != nil {
			t.Fatal(err)
		}
		if got := time.Since(at) < time.Minute; got != fresh {
			t.Errorf("%s: touched %v ago, want fresh=%v", st[:4], time.Since(at), fresh)
		}
	}
	var v map[string]string
	if ok, err := Get(ctx, db, "profile", &v); !ok || err != nil || !strings.Contains(v["avatar_url"], newStem) {
		t.Fatalf("Get = %v %v %v", v, ok, err)
	}
}
