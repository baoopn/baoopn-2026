package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/baoopn/baoopn-2026/internal/backups"
	"github.com/baoopn/baoopn-2026/internal/httpapi"
)

func backupSetting(v shape) shape {
	return obj(fields{"value": null(v), "env": null(v), "override": null(v), "source": enum("env", "override")})
}

var (
	backupRun = obj(fields{
		"id": num, "kind": enum("scheduled", "startup", "retry", "manual", "cli"),
		"status": enum("queued", "running", "ok", "failed"), "requested_at": null(ts), "started_at": null(ts),
		"finished_at": null(ts), "name": null(str), "size_bytes": null(num), "sha256": null(str),
		"tables": null(num), "goose_version": null(num), "pruned": null(num), "prune_error": null(str),
		"error": null(str),
	})
	backupStatus = obj(fields{
		"health":     enum("ok", "warning", "stale", "waiting", "paused", "not_configured", "offline", "not_reporting"),
		"checked_at": ts,
		"agent": null(obj(fields{"version": str, "started_at": ts, "last_seen_at": ts, "online": boolean,
			"state": enum("idle", "waiting", "running", "paused"), "idle_reason": null(str),
			"next_run_at": null(ts), "settings_applied_at": null(ts)})),
		"destination": obj(fields{"kind": null(enum("s3", "rclone")), "s3_provider": null(str),
			"s3_bucket": null(str), "prefix": null(str)}),
		"secrets": obj(fields{"age_recipient": boolean, "s3_endpoint": boolean, "s3_access_key_id": boolean,
			"s3_secret_access_key": boolean, "ping_url": boolean}),
		"settings": obj(fields{"backup_time": backupSetting(str), "keep_days": backupSetting(num),
			"keep_min": backupSetting(num), "max_age_hours": backupSetting(num),
			"retry_minutes": null(num), "on_start": null(boolean), "paused": boolean, "paused_at": null(ts),
			"updated_at": null(ts), "pending": boolean}),
		"last_success_at": null(ts), "last_run": null(backupRun), "active_run": null(backupRun),
		"runs": arr(backupRun),
	})
)

// withBackups mounts the backups module on the harness router.
func withBackups(t *testing.T) func(*httpapi.Deps) {
	return func(d *httpapi.Deps) {
		d.Modules = append(d.Modules, backups.New(backups.Deps{DB: d.DB,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil))}))
	}
}

func TestContractBackups(t *testing.T) {
	c := newHarness(t, withBackups(t))

	st := c.ok("GET", "/api/admin/backups", nil, 200, backupStatus, false)
	if field(st, "health") != "not_reporting" || field(st, "agent") != nil || len(list(field(st, "runs"))) != 0 {
		t.Errorf("empty status = %v", st)
	}
	c.fail("POST", "/api/admin/backups/run", nil, 409, "agent_offline")

	// A heartbeat with every column set, plus a finished run with every field.
	now := time.Now().UTC()
	ctx := context.Background()
	if _, err := c.db.Exec(ctx, `INSERT INTO backup_agent (id, version, started_at, last_seen_at, state,
		destination, s3_provider, s3_bucket, prefix, recipient_set, s3_endpoint_set, s3_access_key_set,
		s3_secret_set, ping_url_set, env_backup_time, env_keep_days, env_keep_min, env_max_age_hours,
		retry_minutes, on_start, settings_applied_at, next_run_at, first_due_at, last_success_at)
		VALUES (1, 'v1.2.3', $1, $1, 'waiting', 's3', 'Cloudflare', 'baoopn-backups', 'prod/', true, true, true,
		true, false, '03:00', 30, 7, 26, 30, false, $1, $1::timestamptz + interval '1 day', $1, $1::timestamptz - interval '1 hour')`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := c.db.Exec(ctx, `INSERT INTO backup_runs (kind, status, requested_at, started_at, finished_at,
		name, size_bytes, sha256, tables, goose_version, pruned, prune_error, error)
		VALUES ('scheduled', 'ok', NULL, $1::timestamptz - interval '2 hours', $1::timestamptz - interval '2 hours', 'baoopn-x.dump.age',
		12345, 'abc', 30, 9, 1, NULL, NULL)`, now); err != nil {
		t.Fatal(err)
	}

	st = c.ok("PUT", "/api/admin/backups/settings", map[string]any{"keep_days": 14, "backup_time": nil}, 200,
		backupStatus, false)
	if field(st, "health") != "ok" || field(field(field(st, "settings"), "keep_days"), "source") != "override" ||
		field(field(st, "settings"), "pending") != true {
		t.Errorf("after PUT = %v", st)
	}
	c.fail("PUT", "/api/admin/backups/settings", map[string]any{"keep_min": 5000}, 422, "validation")
	c.fail("PUT", "/api/admin/backups/settings", "{nope", 400, "bad_request")
	st = c.ok("PUT", "/api/admin/backups/pause", map[string]any{"paused": true}, 200, backupStatus, false)
	if field(st, "health") != "paused" || field(field(st, "settings"), "paused_at") == nil {
		t.Errorf("after pause = %v", st)
	}
	c.fail("PUT", "/api/admin/backups/pause", map[string]any{}, 400, "bad_request")
	st = c.ok("DELETE", "/api/admin/backups/settings", nil, 200, backupStatus, false)
	if field(field(field(st, "settings"), "keep_days"), "source") != "env" || field(field(st, "settings"), "paused") != true {
		t.Errorf("after DELETE = %v", st)
	}

	run := c.ok("POST", "/api/admin/backups/run", nil, 202, backupRun, false)
	if field(run, "status") != "queued" || field(run, "kind") != "manual" {
		t.Errorf("run = %v", run)
	}
	c.fail("POST", "/api/admin/backups/run", nil, 409, "busy")
	st = c.ok("GET", "/api/admin/backups", nil, 200, backupStatus, false)
	if idOf(field(st, "active_run")) != idOf(run) || field(field(st, "last_run"), "status") != "ok" ||
		len(list(field(st, "runs"))) != 2 {
		t.Errorf("status with queued run = %v", st)
	}
}
