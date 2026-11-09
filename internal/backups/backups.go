// Package backups shows the off-site backup sidecar (deploy/backup) in the
// admin and stores its settings.
//
// The sidecar writes its heartbeat to backup_agent and its runs to
// backup_runs. The API never talks to the bucket: it only reads those
// tables, writes the settings document "backup" and queues manual runs.
package backups

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// onlineWindow: an agent seen longer ago than this is offline.
	onlineWindow = 3 * time.Minute
	// queuedExpiry: a queued run nobody picked up within this fails.
	queuedExpiry = 10 * time.Minute
	// listRuns is how many runs the status lists.
	listRuns = 20
	// Defaults when neither the agent nor an override gives a value.
	defaultMaxAgeHours  = 26
	defaultRetryMinutes = 30
)

// Deps are the module's dependencies.
type Deps struct {
	DB  *pgxpool.Pool
	Log *slog.Logger
	// Now is the clock (tests); nil means time.Now.
	Now func() time.Time
}

// Module serves /api/admin/backups.
type Module struct {
	d  Deps
	mu sync.Mutex // serialises read-modify-write of the settings document
}

// New builds the module.
func New(d Deps) *Module {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Module{d: d}
}

// Health is BackupHealth in types.ts.
type Health string

// Health codes, in the order they are checked.
const (
	NotReporting  Health = "not_reporting"
	Offline       Health = "offline"
	NotConfigured Health = "not_configured"
	Paused        Health = "paused"
	OK            Health = "ok"
	Warning       Health = "warning"
	Waiting       Health = "waiting"
	Stale         Health = "stale"
)

// Run is BackupRun in types.ts.
type Run struct {
	ID           int64      `json:"id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	RequestedAt  *time.Time `json:"requested_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	Name         *string    `json:"name"`
	SizeBytes    *int64     `json:"size_bytes"`
	SHA256       *string    `json:"sha256"`
	Tables       *int       `json:"tables"`
	GooseVersion *int64     `json:"goose_version"`
	Pruned       *int       `json:"pruned"`
	PruneError   *string    `json:"prune_error"`
	Error        *string    `json:"error"`
}

// Agent is BackupStatus.agent.
type Agent struct {
	Version           string     `json:"version"`
	StartedAt         time.Time  `json:"started_at"`
	LastSeenAt        time.Time  `json:"last_seen_at"`
	Online            bool       `json:"online"`
	State             string     `json:"state"`
	IdleReason        *string    `json:"idle_reason"`
	NextRunAt         *time.Time `json:"next_run_at"`
	SettingsAppliedAt *time.Time `json:"settings_applied_at"`
}

// Destination is BackupStatus.destination. Never holds a secret.
type Destination struct {
	Kind       *string `json:"kind"`
	S3Provider *string `json:"s3_provider"`
	S3Bucket   *string `json:"s3_bucket"`
	Prefix     *string `json:"prefix"`
}

// Secrets says which secrets the sidecar has, never their values.
type Secrets struct {
	AgeRecipient      bool `json:"age_recipient"`
	S3Endpoint        bool `json:"s3_endpoint"`
	S3AccessKeyID     bool `json:"s3_access_key_id"`
	S3SecretAccessKey bool `json:"s3_secret_access_key"`
	PingURL           bool `json:"ping_url"`
}

// Setting is BackupSetting<T>.
type Setting[T any] struct {
	Value    *T     `json:"value"`
	Env      *T     `json:"env"`
	Override *T     `json:"override"`
	Source   string `json:"source"`
}

func newSetting[T any](env, override *T) Setting[T] {
	s := Setting[T]{Value: env, Env: env, Override: override, Source: "env"}
	if override != nil {
		s.Value, s.Source = override, "override"
	}
	return s
}

// Settings is BackupStatus.settings.
type Settings struct {
	BackupTime   Setting[string] `json:"backup_time"`
	KeepDays     Setting[int]    `json:"keep_days"`
	KeepMin      Setting[int]    `json:"keep_min"`
	MaxAgeHours  Setting[int]    `json:"max_age_hours"`
	RetryMinutes *int            `json:"retry_minutes"`
	OnStart      *bool           `json:"on_start"`
	Paused       bool            `json:"paused"`
	PausedAt     *time.Time      `json:"paused_at"`
	UpdatedAt    *time.Time      `json:"updated_at"`
	Pending      bool            `json:"pending"`
}

// Status is BackupStatus in types.ts.
type Status struct {
	Health        Health      `json:"health"`
	CheckedAt     time.Time   `json:"checked_at"`
	Agent         *Agent      `json:"agent"`
	Destination   Destination `json:"destination"`
	Secrets       Secrets     `json:"secrets"`
	Settings      Settings    `json:"settings"`
	LastSuccessAt *time.Time  `json:"last_success_at"`
	LastRun       *Run        `json:"last_run"`
	ActiveRun     *Run        `json:"active_run"`
	Runs          []Run       `json:"runs"`
}

// agentRow is the backup_agent row with what the response leaves out.
type agentRow struct {
	Agent
	dest                      Destination
	secrets                   Secrets
	envTime                   *string
	envKeepDays, envKeepMin   *int
	envMaxAge, retryMinutes   *int
	onStart                   bool
	firstDueAt, lastSuccessAt *time.Time
}

// querier is a pool or a transaction.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const agentSQL = `SELECT version, started_at, last_seen_at, state, idle_reason, next_run_at, settings_applied_at,
	destination, s3_provider, s3_bucket, prefix,
	recipient_set, s3_endpoint_set, s3_access_key_set, s3_secret_set, ping_url_set,
	env_backup_time, env_keep_days, env_keep_min, env_max_age_hours, retry_minutes, on_start,
	first_due_at, last_success_at
	FROM backup_agent WHERE id = 1`

func loadAgent(ctx context.Context, q querier) (*agentRow, error) {
	var a agentRow
	err := q.QueryRow(ctx, agentSQL).Scan(&a.Version, &a.StartedAt, &a.LastSeenAt, &a.State, &a.IdleReason,
		&a.NextRunAt, &a.SettingsAppliedAt, &a.dest.Kind, &a.dest.S3Provider, &a.dest.S3Bucket, &a.dest.Prefix,
		&a.secrets.AgeRecipient, &a.secrets.S3Endpoint, &a.secrets.S3AccessKeyID, &a.secrets.S3SecretAccessKey,
		&a.secrets.PingURL, &a.envTime, &a.envKeepDays, &a.envKeepMin, &a.envMaxAge, &a.retryMinutes,
		&a.onStart, &a.firstDueAt, &a.lastSuccessAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.StartedAt, a.LastSeenAt = a.StartedAt.UTC(), a.LastSeenAt.UTC()
	a.NextRunAt, a.SettingsAppliedAt = utc(a.NextRunAt), utc(a.SettingsAppliedAt)
	a.firstDueAt, a.lastSuccessAt = utc(a.firstDueAt), utc(a.lastSuccessAt)
	return &a, nil
}

const runCols = `id, kind, status, requested_at, started_at, finished_at, name, size_bytes, sha256,
	tables, goose_version, pruned, prune_error, error`

func scanRun(row pgx.Row) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.Kind, &r.Status, &r.RequestedAt, &r.StartedAt, &r.FinishedAt, &r.Name,
		&r.SizeBytes, &r.SHA256, &r.Tables, &r.GooseVersion, &r.Pruned, &r.PruneError, &r.Error)
	r.RequestedAt, r.StartedAt, r.FinishedAt = utc(r.RequestedAt), utc(r.StartedAt), utc(r.FinishedAt)
	return r, err
}

func queryRuns(ctx context.Context, q querier, where string, limit int) ([]Run, error) {
	rows, err := q.Query(ctx, `SELECT `+runCols+` FROM backup_runs WHERE `+where+` LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func firstRun(ctx context.Context, q querier, where string) (*Run, error) {
	rs, err := queryRuns(ctx, q, where, 1)
	if err != nil || len(rs) == 0 {
		return nil, err
	}
	return &rs[0], nil
}

// loadDoc reads the settings document and when it last changed.
func loadDoc(ctx context.Context, q querier) (Doc, *time.Time, error) {
	var raw []byte
	var at time.Time
	err := q.QueryRow(ctx, `SELECT value, updated_at FROM settings WHERE key = $1`, Key).Scan(&raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return Doc{}, nil, nil
	}
	if err != nil {
		return Doc{}, nil, err
	}
	at = at.UTC()
	return parseDoc(raw), &at, nil
}

// Status reads everything the admin page shows.
func (m *Module) Status(ctx context.Context) (Status, error) {
	tx, err := m.d.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Status{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	st := Status{CheckedAt: m.d.Now().UTC()}
	a, err := loadAgent(ctx, tx)
	if err != nil {
		return Status{}, err
	}
	doc, updatedAt, err := loadDoc(ctx, tx)
	if err != nil {
		return Status{}, err
	}
	if st.Runs, err = queryRuns(ctx, tx, `true ORDER BY id DESC`, listRuns); err != nil {
		return Status{}, err
	}
	if st.LastRun, err = firstRun(ctx, tx, `status IN ('ok','failed') ORDER BY id DESC`); err != nil {
		return Status{}, err
	}
	// A running run first: a queued one only waits behind it.
	if st.ActiveRun, err = firstRun(ctx, tx,
		`status IN ('queued','running') ORDER BY (status = 'running') DESC, id DESC`); err != nil {
		return Status{}, err
	}
	var lastOK *time.Time
	if err := tx.QueryRow(ctx, `SELECT max(finished_at) FROM backup_runs WHERE status = 'ok'`).Scan(&lastOK); err != nil {
		return Status{}, err
	}
	st.fill(a, doc, updatedAt, utc(lastOK))
	return st, nil
}

// fill derives the response from the rows read.
func (st *Status) fill(a *agentRow, doc Doc, updatedAt, lastOK *time.Time) {
	var env agentRow
	if a != nil {
		env = *a
		ag := a.Agent
		ag.Online = st.CheckedAt.Sub(a.LastSeenAt) < onlineWindow
		st.Agent, st.Destination, st.Secrets = &ag, a.dest, a.secrets
	}
	s := &st.Settings
	s.BackupTime = newSetting(env.envTime, doc.BackupTime)
	s.KeepDays = newSetting(env.envKeepDays, doc.KeepDays)
	s.KeepMin = newSetting(env.envKeepMin, doc.KeepMin)
	s.MaxAgeHours = newSetting(env.envMaxAge, doc.MaxAgeHours)
	s.Paused, s.PausedAt, s.UpdatedAt = doc.Paused, doc.PausedAt, updatedAt
	if a != nil {
		s.RetryMinutes, s.OnStart = a.retryMinutes, &a.onStart
	}
	s.Pending = updatedAt != nil && (a == nil || a.SettingsAppliedAt == nil || updatedAt.After(*a.SettingsAppliedAt))

	st.LastSuccessAt = later(env.lastSuccessAt, lastOK)
	st.Health = st.health(a)
}

func (st *Status) health(a *agentRow) Health {
	switch {
	case a == nil:
		return NotReporting
	case !st.Agent.Online:
		return Offline
	case a.State == "idle":
		return NotConfigured
	case a.State == "paused" || st.Settings.Paused:
		return Paused
	}
	maxAge := defaultMaxAgeHours
	if v := st.Settings.MaxAgeHours.Value; v != nil {
		maxAge = *v
	}
	now := st.CheckedAt
	if st.LastSuccessAt != nil {
		if now.Sub(*st.LastSuccessAt) <= time.Duration(maxAge)*time.Hour {
			if st.LastRun != nil && st.LastRun.Status == "failed" {
				return Warning
			}
			return OK
		}
		return Stale
	}
	retry := defaultRetryMinutes
	if a.retryMinutes != nil {
		retry = *a.retryMinutes
	}
	// No first_due_at yet: the scheduler has not planned its first run.
	if a.firstDueAt == nil || now.Before(a.firstDueAt.Add(time.Duration(retry+60)*time.Minute)) {
		return Waiting
	}
	return Stale
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func later(a, b *time.Time) *time.Time {
	if a == nil || (b != nil && b.After(*a)) {
		return b
	}
	return a
}
