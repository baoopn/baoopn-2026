// Package backupstest creates the backup status tables for tests until
// migration 00009_backup_status.sql is on this branch.
//
// TEMPORARY: once the migration is merged, testdb.New creates these tables
// and EnsureSchema is a no-op. Delete this package and its calls then.
package backupstest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ddl is the Up part of migrations/00009_backup_status.sql (the contract).
const ddl = `
CREATE TABLE backup_runs (
    id            bigserial PRIMARY KEY,
    kind          text NOT NULL CHECK (kind IN ('scheduled','startup','retry','manual','cli')),
    status        text NOT NULL CHECK (status IN ('queued','running','ok','failed')),
    requested_at  timestamptz,
    started_at    timestamptz,
    finished_at   timestamptz,
    name          text,
    size_bytes    bigint,
    sha256        text,
    tables        integer,
    goose_version bigint,
    pruned        integer,
    prune_error   text CHECK (char_length(prune_error) <= 1000),
    error         text CHECK (char_length(error) <= 1000),
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX backup_runs_one_queued ON backup_runs ((true)) WHERE status = 'queued';
CREATE INDEX backup_runs_active_idx ON backup_runs (status) WHERE status IN ('queued','running');

CREATE TABLE backup_agent (
    id                  smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    version             text NOT NULL DEFAULT '',
    started_at          timestamptz NOT NULL,
    last_seen_at        timestamptz NOT NULL,
    state               text NOT NULL CHECK (state IN ('idle','waiting','running','paused')),
    idle_reason         text CHECK (char_length(idle_reason) <= 1000),
    destination         text CHECK (destination IN ('s3','rclone')),
    s3_provider         text,
    s3_bucket           text,
    prefix              text,
    recipient_set       boolean NOT NULL DEFAULT false,
    s3_endpoint_set     boolean NOT NULL DEFAULT false,
    s3_access_key_set   boolean NOT NULL DEFAULT false,
    s3_secret_set       boolean NOT NULL DEFAULT false,
    ping_url_set        boolean NOT NULL DEFAULT false,
    env_backup_time     text,
    env_keep_days       integer,
    env_keep_min        integer,
    env_max_age_hours   integer,
    retry_minutes       integer,
    on_start            boolean NOT NULL DEFAULT false,
    settings_applied_at timestamptz,
    next_run_at         timestamptz,
    first_due_at        timestamptz,
    last_success_at     timestamptz
);`

// EnsureSchema creates backup_runs and backup_agent in the pool's schema
// unless a migration already did.
func EnsureSchema(t testing.TB, db *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var have bool
	if err := db.QueryRow(ctx, `SELECT to_regclass('backup_runs') IS NOT NULL`).Scan(&have); err != nil {
		t.Fatalf("backupstest: probe: %v", err)
	}
	if have {
		return
	}
	if _, err := db.Exec(ctx, ddl); err != nil {
		t.Fatalf("backupstest: create tables: %v", err)
	}
}
