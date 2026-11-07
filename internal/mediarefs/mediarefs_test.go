package mediarefs

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

const stem = "0123456789abcdef01234567"

func TestStems(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		name, in string
		want     []string
	}{
		{"site path", "![x](/media/" + stem + ".webp)", []string{stem}},
		{"public domain", "https://media.baoopn.com/" + stem + ".png", []string{stem}},
		{"absolute site url", "https://baoopn.com/media/" + stem + ".jpg", []string{stem}},
		{"bare stem", stem, []string{stem}},
		{"punctuation", "(" + stem + "), " + stem + ".", []string{stem}},
		{"query", "/media/" + stem + ".png?w=200", []string{stem}},
		{"fragment", "/media/" + stem + ".pdf#page=2", []string{stem}},
		{"json escaped", `{"u":"\/media\/` + stem + `.svg"}`, []string{stem}},
		{"sha256", sha, nil},
		{"32 hex reply token", strings.Repeat("c", 32), nil},
		{"25 hex", stem + "a", nil},
		{"23 hex", stem[:23], nil},
		{"hex before", "f" + stem, nil},
		{"uppercase", strings.ToUpper(stem), nil},
		{"uppercase neighbour", "A" + stem, nil},
		{"one uppercase inside", stem[:5] + "A" + stem[6:], nil},
		{"two and a duplicate", stem + " /media/" + stem + ".png " + strings.Repeat("9", 24),
			[]string{stem, strings.Repeat("9", 24)}},
		{"empty", "", nil},
	} {
		if got := Stems(tc.in); !slices.Equal(got, tc.want) {
			t.Errorf("%s: Stems(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestSchemaGuard fails when a table is added that is neither scanned for
// references nor explicitly ignored: decide where the new table belongs.
func TestSchemaGuard(t *testing.T) {
	db := testdb.New(t)
	rows, err := db.Query(context.Background(), `SELECT table_name FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`)
	if err != nil {
		t.Fatal(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(names) < 10 {
		t.Fatalf("only %d tables found: %v", len(names), names)
	}
	for _, n := range names {
		if !slices.Contains(Tables, n) && !slices.Contains(Ignored, n) {
			t.Errorf("table %q is not classified: add it to mediarefs.Tables (may name media) or Ignored", n)
		}
	}
	for _, n := range Tables {
		if !slices.Contains(names, n) {
			t.Errorf("scanned table %q does not exist", n)
		}
	}
}

func TestScanAndTouch(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	other := strings.Repeat("e", 24)
	for _, q := range []string{
		`INSERT INTO posts (id, slug, title, cover_url) VALUES (1, 'a', 'A', '/media/` + stem + `.png')`,
		`INSERT INTO settings (key, value) VALUES ('x', '{"u": "https://m.example.com/` + other + `.webp"}')`,
		`INSERT INTO media (filename, content_type, size_bytes, touched_at) VALUES
			('` + stem + `.png', 'image/png', 1, now() - interval '30 days'),
			('` + other + `.webp', 'image/webp', 1, now() - interval '30 days')`,
	} {
		if _, err := db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	where, err := ScanWhere(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if where[stem] != "posts" || where[other] != "settings" || len(where) != 2 {
		t.Fatalf("ScanWhere = %v", where)
	}

	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return Touch(ctx, tx, "nothing here", "/media/"+stem+".webp") })
	if err != nil {
		t.Fatal(err)
	}
	var touched, untouched time.Time
	if err := db.QueryRow(ctx, `SELECT touched_at FROM media WHERE stem = $1`, stem).Scan(&touched); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(ctx, `SELECT touched_at FROM media WHERE stem = $1`, other).Scan(&untouched); err != nil {
		t.Fatal(err)
	}
	if time.Since(touched) > time.Minute || time.Since(untouched) < 29*24*time.Hour {
		t.Fatalf("touched %v, untouched %v", touched, untouched)
	}
	// No stems: still takes the lock, changes nothing.
	if err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error { return Touch(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}
