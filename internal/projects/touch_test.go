package projects

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// Patching or removing a project bumps media.touched_at for the files its
// old and new text name (orphan cleanup grace).
func TestProjectWritesTouchMedia(t *testing.T) {
	pool := testdb.New(t)
	m := New(Deps{DB: pool, Log: slog.New(slog.DiscardHandler)})
	ctx := context.Background()
	oldStem, newStem := strings.Repeat("4", 24), strings.Repeat("5", 24)
	if _, err := pool.Exec(ctx, `INSERT INTO media (filename, content_type, size_bytes)
		SELECT s || '.webp', 'image/webp', 1 FROM unnest($1::text[]) s`, []string{oldStem, newStem}); err != nil {
		t.Fatal(err)
	}
	age := func() {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE media SET touched_at = now() - interval '30 days'`); err != nil {
			t.Fatal(err)
		}
	}
	fresh := func(st string) bool {
		t.Helper()
		var at time.Time
		if err := pool.QueryRow(ctx, `SELECT touched_at FROM media WHERE stem = $1`, st).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return time.Since(at) < time.Minute
	}
	p, err := m.createManual(ctx, "Demo", "")
	if err != nil {
		t.Fatal(err)
	}
	cover := "/media/" + oldStem + ".webp"
	if _, _, err := m.patch(ctx, p.ID, Patch{CoverURL: &cover}); err != nil {
		t.Fatal(err)
	}
	age()
	body := "![x](https://media.example.com/" + newStem + ".webp)"
	empty := ""
	if _, _, err := m.patch(ctx, p.ID, Patch{CoverURL: &empty, BodyMD: &body}); err != nil {
		t.Fatal(err)
	}
	if !fresh(oldStem) || !fresh(newStem) {
		t.Fatal("patch did not touch the old cover and the new body image")
	}
	age()
	if ok, err := m.remove(ctx, p.ID); !ok || err != nil {
		t.Fatalf("remove = %v, %v", ok, err)
	}
	if !fresh(newStem) || fresh(oldStem) {
		t.Fatal("remove did not touch the removed project's media")
	}
	if ok, err := m.remove(ctx, p.ID); ok || err != nil {
		t.Fatalf("second remove = %v, %v", ok, err)
	}
}
