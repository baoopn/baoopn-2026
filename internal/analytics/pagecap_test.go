package analytics

import (
	"strconv"
	"testing"
	"time"
)

func TestPageKeyCap(t *testing.T) {
	now := time.Now()
	r := testRecorder(&now)
	base := pageKey{bucket: now.Truncate(time.Hour), device: "desktop", browser: "Chrome"}
	// Fill the map to the cap; the first key is /blog/a with no referrer.
	for i := range maxPageKeys {
		k := base
		k.path = "/blog/p" + strconv.Itoa(i)
		if i == 0 {
			k.path = "/blog/a"
		}
		r.observePage(k, 1, 0)
	}

	// New key whose referrer-less row exists: folded into it.
	folded := base
	folded.path, folded.referrer = "/blog/a", "news.example"
	r.observePage(folded, 1, 0)
	// New key with no such row: dropped.
	dropped := base
	dropped.path = "/blog/zzz"
	r.observePage(dropped, 1, 0)
	if n := len(r.pages); n != maxPageKeys {
		t.Fatalf("pending rows = %d, want %d", n, maxPageKeys)
	}
	a := base
	a.path = "/blog/a"
	r.observePage(a, 0, 500) // existing key: still counted
	if v := r.pages[a]; v == nil || v.views != 2 || v.visibleMS != 500 {
		t.Fatalf("folded row = %+v, want 2 views", v)
	}
	if r.pages[dropped] != nil || r.dropped != 1 {
		t.Fatalf("dropped = %d, row %v", r.dropped, r.pages[dropped])
	}

	// A failed flush re-queues under the same cap; the drop count is reset.
	pending := map[pageKey]*pageVal{dropped: {views: 3}, a: {views: 1}}
	_ = r.Flush(t.Context()) // no database: everything is re-queued
	if r.dropped != 0 {
		t.Fatalf("drop count not reset: %d", r.dropped)
	}
	r.requeuePages(pending)
	if n := len(r.pages); n != maxPageKeys {
		t.Fatalf("pending rows after requeue = %d", n)
	}
	if v := r.pages[a]; v.views != 3 {
		t.Fatalf("requeued row = %d views, want 3", v.views)
	}
}
