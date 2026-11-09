package backups

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

type fixture struct {
	t   *testing.T
	db  *pgxpool.Pool
	h   http.Handler
	now atomic.Pointer[time.Time]
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testdb.New(t)
	f := &fixture{t: t, db: db}
	start := time.Now().UTC().Truncate(time.Second)
	f.now.Store(&start)
	m := New(Deps{DB: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return *f.now.Load() }})
	r := chi.NewRouter()
	m.Admin(r)
	f.h = r
	return f
}

func (f *fixture) clock() time.Time { return *f.now.Load() }

func (f *fixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.db.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

// agent upserts a heartbeat seen `ago` before the fixture clock.
func (f *fixture) agent(state string, ago time.Duration) {
	f.t.Helper()
	seen := f.clock().Add(-ago)
	f.exec(`INSERT INTO backup_agent (id, version, started_at, last_seen_at, state, idle_reason, destination,
		s3_bucket, recipient_set, env_backup_time, env_keep_days, env_keep_min, env_max_age_hours, retry_minutes,
		settings_applied_at, first_due_at)
		VALUES (1, 'v1', $1, $1, $2, CASE WHEN $2 = 'idle' THEN 'BACKUP_AGE_RECIPIENT is not set' END, 's3',
		'bucket', true, '03:00', 30, 7, 26, 30, $1, $1)
		ON CONFLICT (id) DO UPDATE SET last_seen_at = EXCLUDED.last_seen_at, state = EXCLUDED.state,
		idle_reason = EXCLUDED.idle_reason`, seen, state)
}

type response struct {
	code int
	Data json.RawMessage `json:"data"`
	Err  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (f *fixture) do(method, path, body string) response {
	f.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	var r response
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		f.t.Fatalf("%s %s: not JSON: %q", method, path, w.Body)
	}
	r.code = w.Code
	return r
}

func (f *fixture) status(method, path, body string) Status {
	f.t.Helper()
	r := f.do(method, path, body)
	if r.code != http.StatusOK {
		f.t.Fatalf("%s %s = %d %s", method, path, r.code, r.Err.Message)
	}
	var st Status
	if err := json.Unmarshal(r.Data, &st); err != nil {
		f.t.Fatal(err)
	}
	return st
}

func (f *fixture) wantErr(r response, code int, errCode string) {
	f.t.Helper()
	if r.code != code || r.Err.Code != errCode {
		f.t.Errorf("got %d %q (%s), want %d %q", r.code, r.Err.Code, r.Err.Message, code, errCode)
	}
}

func TestGetEmpty(t *testing.T) {
	f := newFixture(t)
	st := f.status("GET", "/backups", "")
	if st.Health != NotReporting || st.Agent != nil || st.Runs == nil || len(st.Runs) != 0 ||
		st.LastRun != nil || st.ActiveRun != nil || st.Settings.Paused || st.Settings.Pending {
		t.Errorf("empty status = %+v", st)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	f := newFixture(t)
	f.agent("waiting", 0)

	st := f.status("PUT", "/backups/settings", `{"backup_time":"04:15","keep_days":14}`)
	s := st.Settings
	if *s.BackupTime.Value != "04:15" || s.BackupTime.Source != "override" || *s.KeepDays.Value != 14 ||
		s.KeepMin.Source != "env" || *s.KeepMin.Value != 7 || s.MaxAgeHours.Override != nil || !s.Pending {
		t.Errorf("after PUT = %+v", s)
	}
	if st.Destination.Kind == nil || *st.Destination.Kind != "s3" || !st.Secrets.AgeRecipient || st.Secrets.PingURL {
		t.Errorf("destination/secrets = %+v %+v", st.Destination, st.Secrets)
	}

	f.wantErr(f.do("PUT", "/backups/settings", `{"keep_days":4000}`), 422, "validation")
	f.wantErr(f.do("PUT", "/backups/settings", `{"max_age_hours":24}`), 422, "validation")
	f.wantErr(f.do("PUT", "/backups/settings", `{"backup_time":"7am"}`), 422, "validation")
	f.wantErr(f.do("PUT", "/backups/settings", `{"keep_days":1.5}`), 400, "bad_request")
	f.wantErr(f.do("PUT", "/backups/settings", `{"keep":1}`), 400, "bad_request")
	f.wantErr(f.do("PUT", "/backups/settings", `{"paused":true}`), 400, "bad_request")
	if st := f.status("GET", "/backups", ""); *st.Settings.KeepDays.Value != 14 {
		t.Errorf("rejected PUT changed keep_days: %+v", st.Settings.KeepDays)
	}

	st = f.status("PUT", "/backups/pause", `{"paused":true}`)
	if !st.Settings.Paused || st.Settings.PausedAt == nil || st.Health != Paused || *st.Settings.KeepDays.Value != 14 {
		t.Errorf("after pause = %+v health %s", st.Settings, st.Health)
	}
	pausedAt := *st.Settings.PausedAt
	*f.now.Load() = f.clock().Add(time.Minute)
	if st := f.status("PUT", "/backups/pause", `{"paused":true}`); !st.Settings.PausedAt.Equal(pausedAt) {
		t.Errorf("pausing again moved paused_at to %v", st.Settings.PausedAt)
	}
	f.wantErr(f.do("PUT", "/backups/pause", `{}`), 400, "bad_request")
	f.wantErr(f.do("PUT", "/backups/pause", `{"paused":"yes"}`), 400, "bad_request")

	// A full PUT replaces every override (missing = null) and keeps paused.
	st = f.status("PUT", "/backups/settings", `{"keep_min":3}`)
	if st.Settings.KeepDays.Override != nil || st.Settings.BackupTime.Override != nil || *st.Settings.KeepMin.Value != 3 ||
		!st.Settings.Paused {
		t.Errorf("after second PUT = %+v", st.Settings)
	}

	// DELETE clears the overrides and keeps paused.
	f.status("PUT", "/backups/settings", `{"backup_time":"01:00","keep_days":1,"keep_min":2,"max_age_hours":100}`)
	st = f.status("DELETE", "/backups/settings", "")
	s = st.Settings
	for _, src := range []string{s.BackupTime.Source, s.KeepDays.Source, s.KeepMin.Source, s.MaxAgeHours.Source} {
		if src != "env" {
			t.Errorf("after DELETE source = %s", src)
		}
	}
	if *s.KeepDays.Value != 30 || !s.Paused || s.PausedAt == nil {
		t.Errorf("after DELETE = %+v", s)
	}

	st = f.status("PUT", "/backups/pause", `{"paused":false}`)
	if st.Settings.Paused || st.Settings.PausedAt != nil || st.Health == Paused {
		t.Errorf("after resume = %+v", st.Settings)
	}

	// The stored document has every key, as the sidecar reads it.
	var doc map[string]any
	var raw []byte
	if err := f.db.QueryRow(context.Background(), `SELECT value FROM settings WHERE key = 'backup'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &doc)
	for _, k := range []string{"backup_time", "keep_days", "keep_min", "max_age_hours", "paused", "paused_at"} {
		if _, ok := doc[k]; !ok {
			t.Errorf("stored doc lacks %s: %s", k, raw)
		}
	}
}

func TestInvalidStoredOverride(t *testing.T) {
	f := newFixture(t)
	f.agent("waiting", 0)
	f.exec(`INSERT INTO settings (key, value) VALUES ('backup', '{"keep_days":99999,"backup_time":"nope","keep_min":"x","max_age_hours":48}')`)
	s := f.status("GET", "/backups", "").Settings
	if s.KeepDays.Source != "env" || s.KeepDays.Override != nil || *s.KeepDays.Value != 30 ||
		s.BackupTime.Source != "env" || s.KeepMin.Source != "env" || s.MaxAgeHours.Source != "override" {
		t.Errorf("settings = %+v", s)
	}
	// Pausing keeps the valid override.
	if s := f.status("PUT", "/backups/pause", `{"paused":true}`).Settings; *s.MaxAgeHours.Value != 48 {
		t.Errorf("after pause = %+v", s.MaxAgeHours)
	}
}

func TestRuns(t *testing.T) {
	f := newFixture(t)
	now := f.clock()
	f.agent("waiting", 0)
	for i := range 25 {
		status := "ok"
		if i == 24 {
			status = "failed"
		}
		f.exec(`INSERT INTO backup_runs (kind, status, started_at, finished_at, name, size_bytes)
			VALUES ('scheduled', $1, $2, $2, 'n', 10)`, status, now.Add(time.Duration(i-30)*time.Hour))
	}
	f.exec(`INSERT INTO backup_runs (kind, status, started_at) VALUES ('scheduled', 'running', $1)`, now)
	st := f.status("GET", "/backups", "")
	if len(st.Runs) != listRuns || st.Runs[0].Status != "running" {
		t.Fatalf("runs = %d, first %+v", len(st.Runs), st.Runs[0])
	}
	if st.ActiveRun == nil || st.ActiveRun.Status != "running" || st.LastRun == nil || st.LastRun.Status != "failed" {
		t.Errorf("active %+v last %+v", st.ActiveRun, st.LastRun)
	}
	// Newest ok run finished 7 h ago (i = 23).
	if st.LastSuccessAt == nil || !st.LastSuccessAt.Equal(now.Add(-7*time.Hour)) || st.Health != Warning {
		t.Errorf("last success %v health %s", st.LastSuccessAt, st.Health)
	}
	if st.CheckedAt.Location() != time.UTC || st.LastSuccessAt.Location() != time.UTC {
		t.Error("timestamps not UTC")
	}
}

func TestRunNow(t *testing.T) {
	f := newFixture(t)
	f.wantErr(f.do("POST", "/backups/run", ""), 409, "agent_offline")
	f.agent("waiting", 3*time.Minute)
	f.wantErr(f.do("POST", "/backups/run", ""), 409, "agent_offline")
	f.agent("idle", 0)
	r := f.do("POST", "/backups/run", "")
	f.wantErr(r, 409, "not_configured")
	if !strings.Contains(r.Err.Message, "BACKUP_AGE_RECIPIENT") {
		t.Errorf("not_configured message = %q", r.Err.Message)
	}

	f.agent("paused", time.Minute) // paused does not block a manual run
	r = f.do("POST", "/backups/run", "")
	if r.code != http.StatusAccepted {
		t.Fatalf("POST = %d %s", r.code, r.Err.Message)
	}
	var run Run
	_ = json.Unmarshal(r.Data, &run)
	if run.ID == 0 || run.Kind != "manual" || run.Status != "queued" || run.RequestedAt == nil ||
		!run.RequestedAt.Equal(f.clock()) {
		t.Errorf("run = %+v", run)
	}
	f.wantErr(f.do("POST", "/backups/run", ""), 409, "busy")
	if st := f.status("GET", "/backups", ""); st.ActiveRun == nil || st.ActiveRun.ID != run.ID {
		t.Errorf("active run = %+v", st.ActiveRun)
	}

	// The sidecar claims it: still busy while running.
	f.exec(`UPDATE backup_runs SET status = 'running', started_at = $2 WHERE id = $1`, run.ID, f.clock())
	f.wantErr(f.do("POST", "/backups/run", ""), 409, "busy")
	f.exec(`UPDATE backup_runs SET status = 'ok', finished_at = now() WHERE id = $1`, run.ID)

	// A queued run nobody picked up within 10 minutes fails; a new one is accepted.
	r = f.do("POST", "/backups/run", "")
	if r.code != http.StatusAccepted {
		t.Fatalf("second POST = %d", r.code)
	}
	_ = json.Unmarshal(r.Data, &run)
	*f.now.Load() = f.clock().Add(10*time.Minute + time.Second)
	f.agent("waiting", 0)
	if r := f.do("POST", "/backups/run", ""); r.code != http.StatusAccepted {
		t.Fatalf("POST after expiry = %d %s", r.code, r.Err.Message)
	}
	var status, msg string
	if err := f.db.QueryRow(context.Background(), `SELECT status, error FROM backup_runs WHERE id = $1`, run.ID).
		Scan(&status, &msg); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || msg != "Not picked up by the backup service within 10 minutes" {
		t.Errorf("expired run = %s %q", status, msg)
	}
}

func TestRunNowConcurrent(t *testing.T) {
	f := newFixture(t)
	f.agent("waiting", 0)
	const n = 4
	codes := make([]int, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-start
			req := httptest.NewRequest("POST", "/backups/run", nil)
			w := httptest.NewRecorder()
			f.h.ServeHTTP(w, req)
			codes[i] = w.Code
		})
	}
	close(start)
	wg.Wait()
	accepted := 0
	for _, c := range codes {
		switch c {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
		default:
			t.Errorf("unexpected status %d", c)
		}
	}
	var queued int
	_ = f.db.QueryRow(context.Background(), `SELECT count(*) FROM backup_runs`).Scan(&queued)
	if accepted != 1 || queued != 1 {
		t.Errorf("codes %v, %d rows; want exactly one 202 and one row", codes, queued)
	}
}

func TestStatusExpiresQueued(t *testing.T) {
	f := newFixture(t)
	f.agent("waiting", 0)
	f.exec(`INSERT INTO backup_runs (kind, status, requested_at) VALUES ('manual', 'queued', $1)`,
		f.clock().Add(-queuedExpiry-time.Second))
	f.exec(`INSERT INTO backup_runs (kind, status, requested_at) VALUES ('manual', 'failed', $1)`, f.clock())
	st := f.status("GET", "/backups", "")
	if st.ActiveRun != nil {
		t.Errorf("active run = %+v, want none", st.ActiveRun)
	}
	old := st.Runs[1]
	if old.Status != "failed" || old.Error == nil || *old.Error != "Not picked up by the backup service within 10 minutes" ||
		old.FinishedAt == nil {
		t.Errorf("expired run = %+v", old)
	}

	// A queued run younger than 10 minutes stays.
	f.exec(`INSERT INTO backup_runs (kind, status, requested_at) VALUES ('manual', 'queued', $1)`,
		f.clock().Add(-queuedExpiry+time.Minute))
	if st := f.status("GET", "/backups", ""); st.ActiveRun == nil || st.ActiveRun.Status != "queued" {
		t.Errorf("fresh queued run = %+v", st.ActiveRun)
	}
}

func TestLeftOverRunningRow(t *testing.T) {
	for name, tc := range map[string]struct {
		state   string
		seenAgo time.Duration
		started time.Duration // before the fixture clock
		noAgent bool
		active  bool
	}{
		"online, waiting, started long ago":     {state: "waiting", started: time.Hour},
		"online, paused, started long ago":      {state: "paused", started: time.Hour},
		"online, waiting, just claimed":         {state: "waiting", started: 30 * time.Second, active: true},
		"online, running":                       {state: "running", started: time.Hour, active: true},
		"offline agent":                         {state: "waiting", seenAgo: 5 * time.Minute, started: time.Hour, active: true},
		"no agent row":                          {noAgent: true, started: time.Hour, active: true},
		"online, waiting, started 2m1s earlier": {state: "waiting", started: staleRunGrace + time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			if !tc.noAgent {
				f.agent(tc.state, tc.seenAgo)
			}
			f.exec(`INSERT INTO backup_runs (kind, status, started_at) VALUES ('scheduled', 'running', $1)`,
				f.clock().Add(-tc.started))
			st := f.status("GET", "/backups", "")
			if len(st.Runs) != 1 || st.Runs[0].Status != "running" {
				t.Fatalf("runs = %+v", st.Runs)
			}
			if got := st.ActiveRun != nil; got != tc.active {
				t.Errorf("active_run = %+v, want active %v", st.ActiveRun, tc.active)
			}
			r := f.do("POST", "/backups/run", "")
			switch {
			case tc.noAgent || tc.seenAgo > 0:
				f.wantErr(r, 409, "agent_offline")
			case tc.active:
				f.wantErr(r, 409, "busy")
			case r.code != http.StatusAccepted:
				t.Errorf("POST = %d %s, want 202", r.code, r.Err.Message)
			}
		})
	}
}
