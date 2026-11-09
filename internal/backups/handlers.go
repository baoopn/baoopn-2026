package backups

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
	"github.com/baoopn/baoopn-2026/internal/settings"
)

// Public mounts nothing.
func (m *Module) Public(chi.Router) {}

// Hooks mounts nothing.
func (m *Module) Hooks(chi.Router) {}

// Admin mounts /backups.
func (m *Module) Admin(r chi.Router) {
	r.Get("/backups", m.handleGet)
	r.Put("/backups/settings", m.handlePutSettings)
	r.Delete("/backups/settings", m.handleResetSettings)
	r.Put("/backups/pause", m.handlePause)
	r.Post("/backups/run", m.handleRun)
}

func (m *Module) internal(w http.ResponseWriter, what string, err error) {
	m.d.Log.Error("backups: "+what, logging.Err(err))
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
}

func (m *Module) respondStatus(w http.ResponseWriter, r *http.Request) {
	st, err := m.Status(r.Context())
	if err != nil {
		m.internal(w, "status", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, st)
}

func (m *Module) handleGet(w http.ResponseWriter, r *http.Request) {
	m.respondStatus(w, r)
}

// update applies change to the stored document under the mutex.
func (m *Module) update(ctx context.Context, change func(*Doc)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	doc, _, err := loadDoc(ctx, m.d.DB)
	if err != nil {
		return err
	}
	change(&doc)
	return settings.Put(ctx, m.d.DB, Key, doc)
}

func (m *Module) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in SettingsInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	in, problem := in.validate()
	if problem != "" {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", problem)
		return
	}
	if err := m.update(r.Context(), func(d *Doc) {
		d.BackupTime, d.KeepDays, d.KeepMin, d.MaxAgeHours = in.BackupTime, in.KeepDays, in.KeepMin, in.MaxAgeHours
	}); err != nil {
		m.internal(w, "put settings", err)
		return
	}
	m.respondStatus(w, r)
}

// handleResetSettings clears the four overrides; paused stays as it is.
func (m *Module) handleResetSettings(w http.ResponseWriter, r *http.Request) {
	if err := m.update(r.Context(), func(d *Doc) {
		d.BackupTime, d.KeepDays, d.KeepMin, d.MaxAgeHours = nil, nil, nil, nil
	}); err != nil {
		m.internal(w, "reset settings", err)
		return
	}
	m.respondStatus(w, r)
}

type pauseInput struct {
	Paused *bool `json:"paused"`
}

func (m *Module) handlePause(w http.ResponseWriter, r *http.Request) {
	var in pauseInput
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if in.Paused == nil {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "paused (true or false) is required")
		return
	}
	now := m.d.Now().UTC()
	if err := m.update(r.Context(), func(d *Doc) {
		switch {
		case !*in.Paused:
			d.Paused, d.PausedAt = false, nil
		case !d.Paused || d.PausedAt == nil:
			d.Paused, d.PausedAt = true, &now
		}
	}); err != nil {
		m.internal(w, "pause", err)
		return
	}
	m.respondStatus(w, r)
}

// errConflict is a 409 with its code and message.
type errConflict struct{ code, msg string }

func (e errConflict) Error() string { return e.msg }

// handleRun queues a manual backup for the sidecar to pick up.
func (m *Module) handleRun(w http.ResponseWriter, r *http.Request) {
	run, err := m.queueRun(r.Context())
	var c errConflict
	switch {
	case errors.As(err, &c):
		httpapi.Error(w, http.StatusConflict, c.code, c.msg)
	case err != nil:
		m.internal(w, "queue run", err)
	default:
		httpapi.JSON(w, http.StatusAccepted, run)
	}
}

var (
	errBusy    = errConflict{"busy", "a backup is already queued or running"}
	errOffline = errConflict{"agent_offline",
		"the backup service is not running or has not checked in for 3 minutes"}
)

func (m *Module) queueRun(ctx context.Context) (Run, error) {
	now := m.d.Now().UTC()
	var run Run
	err := pgx.BeginFunc(ctx, m.d.DB, func(tx pgx.Tx) error {
		if err := expireQueued(ctx, tx, now); err != nil {
			return err
		}
		var state string
		var reason *string
		var seen time.Time
		err := tx.QueryRow(ctx, `SELECT state, idle_reason, last_seen_at FROM backup_agent WHERE id = 1`).
			Scan(&state, &reason, &seen)
		if errors.Is(err, pgx.ErrNoRows) {
			return errOffline
		}
		if err != nil {
			return err
		}
		if now.Sub(seen) >= onlineWindow {
			return errOffline
		}
		if state == "idle" {
			msg := "backups are not configured"
			if reason != nil && *reason != "" {
				msg += ": " + *reason
			}
			return errConflict{"not_configured", msg}
		}
		var active bool
		// The agent is online here, so a left-over running row does not block.
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM backup_runs WHERE `+activeWhere+`)`,
			activeArgs(state, seen, true)...).Scan(&active); err != nil {
			return err
		}
		if active {
			return errBusy
		}
		run, err = scanRun(tx.QueryRow(ctx, `INSERT INTO backup_runs (kind, status, requested_at)
			VALUES ('manual', 'queued', $1) RETURNING `+runCols, now))
		return err
	})
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return Run{}, errBusy
	}
	return run, err
}
