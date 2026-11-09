package migrations_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/baoopn-2026/internal/testdb"
)

// TestBackupStatusMigration applies 00009 on a fresh schema, checks the
// constraints the backup sidecar and the API rely on, and that Down drops
// both tables.
func TestBackupStatusMigration(t *testing.T) {
	pool := testdb.Empty(t)
	ctx := context.Background()
	p := provider(t, pool)
	if _, err := p.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	pgCode := func(err error) string {
		var pe *pgconn.PgError
		if errors.As(err, &pe) {
			return pe.Code
		}
		return ""
	}
	exec := func(q string) error {
		_, err := pool.Exec(ctx, q)
		return err
	}

	if err := exec(`INSERT INTO backup_runs (kind, status, requested_at) VALUES ('manual', 'queued', now())`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`INSERT INTO backup_runs (kind, status, requested_at) VALUES ('manual', 'queued', now())`); pgCode(err) != "23505" {
		t.Fatalf("second queued run: want unique violation, got %v", err)
	}
	// Any number of finished and running rows is fine.
	if err := exec(`INSERT INTO backup_runs (kind, status) VALUES ('scheduled', 'running'), ('cli', 'ok'), ('retry', 'failed'), ('startup', 'ok')`); err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]string{
		"bad kind":    `INSERT INTO backup_runs (kind, status) VALUES ('weekly', 'ok')`,
		"bad status":  `INSERT INTO backup_runs (kind, status) VALUES ('manual', 'done')`,
		"long error":  `INSERT INTO backup_runs (kind, status, error) VALUES ('manual', 'failed', repeat('x', 1001))`,
		"agent id 2":  `INSERT INTO backup_agent (id, started_at, last_seen_at, state) VALUES (2, now(), now(), 'idle')`,
		"agent state": `INSERT INTO backup_agent (started_at, last_seen_at, state) VALUES (now(), now(), 'sleeping')`,
		"agent dest":  `INSERT INTO backup_agent (started_at, last_seen_at, state, destination) VALUES (now(), now(), 'idle', 'ftp')`,
	} {
		if err := exec(q); pgCode(err) != "23514" {
			t.Errorf("%s: want check violation, got %v", name, err)
		}
	}
	if err := exec(`INSERT INTO backup_agent (started_at, last_seen_at, state) VALUES (now(), now(), 'waiting')`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`INSERT INTO backup_agent (started_at, last_seen_at, state) VALUES (now(), now(), 'idle')`); pgCode(err) != "23505" {
		t.Fatalf("second agent row: want unique violation, got %v", err)
	}

	if _, err := p.DownTo(ctx, 8); err != nil {
		t.Fatal(err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('backup_runs') IS NOT NULL OR to_regclass('backup_agent') IS NOT NULL`).Scan(&exists); err != nil || exists {
		t.Fatalf("tables after Down: %v %v", exists, err)
	}
	if _, err := p.UpTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
}
