package blog

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Saving or deleting a post bumps media.touched_at for every file named in
// the old or the new text, so the orphan cleanup's grace period restarts.
func TestPostWritesTouchMedia(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	img, cover, other := strings.Repeat("1", 24), strings.Repeat("2", 24), strings.Repeat("3", 24)
	if _, err := h.m.db.Exec(ctx, `INSERT INTO media (filename, content_type, size_bytes)
		SELECT s || '.webp', 'image/webp', 1 FROM unnest($1::text[]) s`, []string{img, cover, other}); err != nil {
		t.Fatal(err)
	}
	age := func() {
		t.Helper()
		if _, err := h.m.db.Exec(ctx, `UPDATE media SET touched_at = now() - interval '30 days'`); err != nil {
			t.Fatal(err)
		}
	}
	fresh := func(st string) bool {
		t.Helper()
		var at time.Time
		if err := h.m.db.QueryRow(ctx, `SELECT touched_at FROM media WHERE stem = $1`, st).Scan(&at); err != nil {
			t.Fatal(err)
		}
		return time.Since(at) < time.Minute
	}

	age()
	a := h.create(PostInput{Title: "A", Status: "draft", CoverURL: "/media/" + cover + ".webp", Parts: one("![x](/media/" + img + ".webp)")})
	if !fresh(img) || !fresh(cover) || fresh(other) {
		t.Fatal("create did not touch the new post's media")
	}

	// Move between posts: the image leaves post A first...
	age()
	in := PostInput{Title: "A", Status: "draft", Slug: a.Slug, Parts: one("no images now")}
	if code := h.do(h.admin, "PUT", "/posts/"+strconv.FormatInt(a.ID, 10), in, nil); code != http.StatusOK {
		t.Fatalf("update: %d", code)
	}
	if !fresh(img) || !fresh(cover) {
		t.Fatal("update did not touch the media removed from the post")
	}

	// ...and a deleted post's media get a full grace period too.
	b := h.create(PostInput{Title: "B", Status: "draft", Parts: one("![y](https://media.example.com/" + other + ".webp)")})
	age()
	if code := h.do(h.admin, "DELETE", "/posts/"+strconv.FormatInt(b.ID, 10), nil, nil); code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if !fresh(other) || fresh(img) {
		t.Fatal("delete did not touch the deleted post's media")
	}
	if code := h.do(h.admin, "DELETE", "/posts/"+strconv.FormatInt(b.ID, 10), nil, nil); code != http.StatusNotFound {
		t.Fatalf("second delete: %d", code)
	}

	age()
	if _, err := h.m.Import(ctx, []PostInput{{Title: "C", Status: "draft", Slug: "c", Parts: one("/media/" + other + ".webp")}}); err != nil {
		t.Fatal(err)
	}
	if !fresh(other) {
		t.Fatal("import did not touch its media")
	}
}
