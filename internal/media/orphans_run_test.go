package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/baoopn/baoopn-2026/internal/mediarefs"
)

type cleanOut struct {
	res OrphanResult
	err error
}

func countRows(t *testing.T, s *Store) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(context.Background(), `SELECT count(*) FROM media`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A save that is about to reference a file (Touch done, reference written,
// not committed) makes the cleanup wait, and the cleanup then keeps it.
func TestOrphanConcurrentSave(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	id := addRow(t, s, 1, rowOpt{age: 30 * day})
	url := "/media/" + stemN(1) + ".webp"

	tx, err := s.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := mediarefs.Touch(ctx, tx, url); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO posts (slug, title, cover_url) VALUES ('a', 'A', $1)`, url); err != nil {
		t.Fatal(err)
	}
	done := make(chan cleanOut, 1)
	go func() {
		res, err := s.CleanOrphans(ctx, TriggerManual, []string{rowKey(id)})
		done <- cleanOut{res, err}
	}()
	select {
	case out := <-done:
		t.Fatalf("cleanup did not wait for the save: %+v %v", out.res, out.err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	out := <-done
	if out.err != nil || out.res.Deleted.Rows != 0 ||
		!slices.Equal(out.res.Skipped, []OrphanSkip{{Key: rowKey(id), Why: "referenced"}}) {
		t.Fatalf("clean = %+v, %v", out.res, out.err)
	}
	if countRows(t, s) != 1 {
		t.Fatal("referenced row was deleted")
	}
}

// The reverse order: the cleanup commits first, then the same bytes are
// uploaded again. The upload must make a new row, not return the deleted one.
func TestOrphanCleanThenDedupe(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	m := mustSave(t, s, "logo.svg", svgDoc, nil)
	exec(t, s, `UPDATE media SET touched_at = now() - interval '30 days' WHERE id = $1`, m.ID)
	if res, err := s.CleanOrphans(ctx, TriggerManual, []string{rowKey(m.ID)}); err != nil || res.Deleted.Rows != 1 {
		t.Fatalf("clean = %+v, %v", res, err)
	}
	again := mustSave(t, s, "logo.svg", svgDoc, nil)
	if again.ID == m.ID || again.Status != StatusReady {
		t.Fatalf("re-upload = %+v", again)
	}
	if _, err := s.Get(ctx, again.ID); err != nil {
		t.Fatal(err)
	}
}

// Retry checks the row, then updates it. A cleanup deleting the row in
// between must not leave a processing row without its original.
func TestOrphanRetryRace(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	id := addRow(t, s, 1, rowOpt{age: 30 * day, status: StatusFailed, job: "failed"})
	writeAged(t, filepath.Join(s.originalsDir(), stemN(1)), time.Hour)

	tx, err := s.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM media WHERE id = $1 FOR UPDATE`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM media WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.Retry(ctx, id)
		done <- err
	}()
	time.Sleep(200 * time.Millisecond) // Retry passes its check and waits on the row lock
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrGone) {
		t.Fatalf("Retry = %v, want ErrGone", err)
	}
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM media WHERE status = 'processing'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("processing rows = %d, %v", n, err)
	}
}

func TestOrphanCap(t *testing.T) {
	s := orphanStore(t, func(d *Deps) { d.OrphanMaxPerPass = 50 })
	ctx := context.Background()
	for n := range 60 {
		addRow(t, s, n, rowOpt{age: 30 * day})
	}
	if p := mustPlan(t, s); p.Totals.Eligible != 60 || p.Totals.WouldDelete != 50 {
		t.Fatalf("totals = %+v", p.Totals)
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil || res.Deleted.Rows != 50 || !res.Capped || res.Remaining != 10 {
		t.Fatalf("first run = %+v, %v", res, err)
	}
	res, err = s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil || res.Deleted.Rows != 10 || res.Capped || res.Remaining != 0 {
		t.Fatalf("second run = %+v, %v", res, err)
	}
	if countRows(t, s) != 0 {
		t.Fatal("rows left")
	}
}

// The plan's eligible keys are exactly what a run deletes, and a second run
// with the same keys finds them all gone.
func TestOrphanDryRunMatchesRun(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	for n := range 3 {
		addRow(t, s, n, rowOpt{age: 30 * day, file: true})
	}
	addRow(t, s, 10, rowOpt{age: day}) // in grace
	writeAged(t, filepath.Join(s.dir, stemN(20)+".png"), 30*day)
	writeAged(t, filepath.Join(s.dir, ".fb-1"), 2*day)
	writeAged(t, filepath.Join(s.originalsDir(), stemN(21)), 2*day)

	p := mustPlan(t, s)
	var eligible []string
	for _, c := range p.Candidates {
		if c.Eligible {
			eligible = append(eligible, c.Key)
		}
	}
	if len(eligible) != 6 || p.Totals.Candidates != 7 {
		t.Fatalf("eligible = %v of %v", eligible, keys(p))
	}
	res, err := s.CleanOrphans(ctx, TriggerManual, nil)
	if err != nil || res.Deleted.total() != len(eligible) || res.Errors != 0 || len(res.Skipped) != 0 {
		t.Fatalf("run = %+v, %v", res, err)
	}
	if left := keys(mustPlan(t, s)); len(left) != 1 || slices.Contains(eligible, left[0]) {
		t.Fatalf("left after run = %v", left)
	}
	res, err = s.CleanOrphans(ctx, TriggerManual, eligible)
	if err != nil || res.Deleted.total() != 0 || res.AlreadyGone != len(eligible) || res.Errors != 0 {
		t.Fatalf("second run = %+v, %v", res, err)
	}
	st, err := s.orphanSettings(ctx)
	if err != nil || st.LastRun == nil || st.LastRun.Deleted != 0 || st.LastRun.Trigger != TriggerManual {
		t.Fatalf("last run = %+v, %v", st.LastRun, err)
	}
}

func TestOrphanBrake(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	var all []string
	for n := range 12 {
		all = append(all, rowKey(addRow(t, s, n, rowOpt{age: 30 * day})))
	}
	res, err := s.CleanOrphans(ctx, TriggerAuto, nil)
	if err != nil || res.Reason != "brake" || res.Deleted.total() != 0 || countRows(t, s) != 12 {
		t.Fatalf("auto run = %+v, %v", res, err)
	}
	// Bao saw the preview: a manual run with keys goes ahead.
	res, err = s.CleanOrphans(ctx, TriggerManual, all)
	if err != nil || res.Deleted.Rows != 12 {
		t.Fatalf("manual run = %+v, %v", res, err)
	}

	// No references at all while files are in use looks like a broken scan.
	s = orphanStore(t, nil)
	for n := range 3 {
		addRow(t, s, n, rowOpt{age: 30 * day})
	}
	if res, err := s.CleanOrphans(ctx, TriggerAuto, nil); err != nil || res.Reason != "brake" || countRows(t, s) != 3 {
		t.Fatalf("auto run without refs = %+v, %v", res, err)
	}
}

// holdRunLock takes the run lock as another API instance would.
func holdRunLock(t *testing.T, s *Store) func() {
	t.Helper()
	ctx := context.Background()
	conn, err := s.db.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, orphanRunLock); err != nil {
		t.Fatal(err)
	}
	return func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, orphanRunLock)
		conn.Release()
	}
}

func TestOrphanBusy(t *testing.T) {
	s := orphanStore(t, nil)
	addRow(t, s, 1, rowOpt{age: 30 * day})
	release := holdRunLock(t, s)
	if _, err := s.CleanOrphans(context.Background(), TriggerManual, nil); !errors.Is(err, ErrBusy) {
		t.Fatalf("Clean while locked = %v", err)
	}
	release()
	if res, err := s.CleanOrphans(context.Background(), TriggerManual, nil); err != nil || res.Deleted.Rows != 1 {
		t.Fatalf("Clean after unlock = %+v, %v", res, err)
	}
}

func TestOrphanJanitor(t *testing.T) {
	s := orphanStore(t, nil)
	ctx := context.Background()
	first := addRow(t, s, 1, rowOpt{age: 30 * day})
	exists := func(id int64) bool {
		_, err := s.Get(ctx, id)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatal(err)
		}
		return err == nil
	}
	if err := s.Tick(ctx); err != nil || !exists(first) {
		t.Fatalf("auto off: tick %v, row kept %v", err, exists(first))
	}
	if err := s.SetOrphanAuto(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx); err != nil || exists(first) {
		t.Fatalf("auto on: tick %v, row kept %v", err, exists(first))
	}
	st, err := s.orphanSettings(ctx)
	if err != nil || !st.Auto || st.LastAutoAt == nil || st.LastRun == nil || st.LastRun.Trigger != TriggerAuto {
		t.Fatalf("settings = %+v, %v", st, err)
	}
	second := addRow(t, s, 2, rowOpt{age: 30 * day})
	if err := s.Tick(ctx); err != nil || !exists(second) {
		t.Fatal("ran again within 24h")
	}
	if err := s.updateOrphanSettings(ctx, func(st *OrphanSettings) {
		at := time.Now().Add(-25 * time.Hour)
		st.LastAutoAt = &at
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Tick(ctx); err != nil || exists(second) {
		t.Fatal("did not run after 24h")
	}
}

func orphanHTTP(t *testing.T, r http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestOrphanHTTP(t *testing.T) {
	s := orphanStore(t, nil)
	r := chi.NewRouter()
	s.Admin(r)
	id := addRow(t, s, 1, rowOpt{age: 30 * day})

	code, out := orphanHTTP(t, r, "GET", "/media/orphans", "")
	data, _ := out["data"].(map[string]any)
	if code != http.StatusOK || data == nil {
		t.Fatalf("GET = %d %v", code, out)
	}
	for _, k := range []string{"enabled", "auto", "grace_seconds", "max_per_pass", "storage", "generated_at",
		"truncated", "candidates", "missing", "kept", "totals", "last_run"} {
		if _, ok := data[k]; !ok {
			t.Errorf("plan has no %q", k)
		}
	}
	if c := data["candidates"].([]any); len(c) != 1 || c[0].(map[string]any)["key"] != rowKey(id) {
		t.Fatalf("candidates = %v", c)
	}

	many := `{"keys":[` + strings.TrimSuffix(strings.Repeat(`"temp:.up-1",`, 501), ",") + `]}`
	for _, body := range []string{`{"keys":["row:../x"]}`, `{"keys":["rows:1"]}`, `{"keys":["row:"]}`,
		`{"nope":1}`, `not json`, many} {
		if code, _ := orphanHTTP(t, r, "POST", "/media/orphans", body); code != http.StatusBadRequest {
			t.Errorf("POST %.40s = %d, want 400", body, code)
		}
	}

	release := holdRunLock(t, s)
	code, out = orphanHTTP(t, r, "POST", "/media/orphans", "")
	release()
	if e, _ := out["error"].(map[string]any); code != http.StatusConflict || e["code"] != "busy" {
		t.Fatalf("busy POST = %d %v", code, out)
	}

	code, out = orphanHTTP(t, r, "POST", "/media/orphans", `{"keys":["row:999999"]}`)
	if data, _ := out["data"].(map[string]any); code != http.StatusOK || data["already_gone"] != 1.0 {
		t.Fatalf("POST unknown key = %d %v", code, out)
	}
	code, out = orphanHTTP(t, r, "POST", "/media/orphans", "")
	if data, _ := out["data"].(map[string]any); code != http.StatusOK || data["deleted"].(map[string]any)["rows"] != 1.0 {
		t.Fatalf("POST all = %d %v", code, out)
	}

	if code, _ := orphanHTTP(t, r, "PUT", "/media/orphans/auto", `{}`); code != http.StatusBadRequest {
		t.Fatalf("PUT without enabled = %d", code)
	}
	code, out = orphanHTTP(t, r, "PUT", "/media/orphans/auto", `{"enabled":true}`)
	if data, _ := out["data"].(map[string]any); code != http.StatusOK || data["auto"] != true || data["last_run"] == nil {
		t.Fatalf("PUT auto = %d %v", code, out)
	}
	if _, out := orphanHTTP(t, r, "GET", "/media/orphans", ""); out["data"].(map[string]any)["auto"] != true {
		t.Fatal("auto not persisted")
	}

	off := orphanStore(t, func(d *Deps) { d.OrphanGrace = -1 })
	ro := chi.NewRouter()
	off.Admin(ro)
	code, out = orphanHTTP(t, ro, "POST", "/media/orphans", `{"keys":[]}`)
	if e, _ := out["error"].(map[string]any); code != http.StatusConflict || e["code"] != "disabled" {
		t.Fatalf("disabled POST = %d %v", code, out)
	}
	if code, out := orphanHTTP(t, ro, "GET", "/media/orphans", ""); code != http.StatusOK ||
		out["data"].(map[string]any)["enabled"] != false {
		t.Fatalf("disabled GET = %d %v", code, out)
	}
	// /media/{id} still works next to the static routes.
	if code, _ := orphanHTTP(t, r, "GET", "/media/orphans/x", ""); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /media/orphans/x = %d", code)
	}
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(map[string]any{"enabled": false})
	if code, _ := orphanHTTP(t, r, "PUT", "/media/orphans/auto", buf.String()); code != http.StatusOK {
		t.Fatalf("PUT auto off = %d", code)
	}
}
