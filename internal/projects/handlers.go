package projects

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Public mounts GET /projects and GET /projects/{slug}.
func (m *Module) Public(r chi.Router) {
	r.Get("/projects", m.publicList)
	r.Get("/projects/{slug}", m.publicDetail)
}

// Hooks implements httpapi.Module (no hooks).
func (m *Module) Hooks(chi.Router) {}

// Admin mounts the project curation routes.
func (m *Module) Admin(r chi.Router) {
	r.Get("/projects", m.adminList)
	r.Post("/projects", m.adminCreate)
	r.Post("/projects/sync", m.adminSync)
	r.Post("/projects/reorder", m.adminReorder)
	r.Get("/projects/{id}", m.adminGet)
	r.Patch("/projects/{id}", m.adminPatch)
	r.Delete("/projects/{id}", m.adminDelete)
}

func (m *Module) publicList(w http.ResponseWriter, r *http.Request) {
	where := `visible`
	switch r.URL.Query().Get("featured") {
	case "", "0", "false":
	case "1", "true":
		where += ` AND featured`
	default:
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "featured must be 1 or 0")
		return
	}
	rows, err := m.query(r.Context(), where)
	if err != nil {
		m.internal(w, r, "projects: list", err)
		return
	}
	out := make([]ProjectSummary, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].summary())
	}
	httpapi.JSON(w, http.StatusOK, out)
}

func (m *Module) publicDetail(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if !validSlug(slug) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	p, err := m.one(r.Context(), m.db, `slug = $1 AND visible`, slug)
	if errors.Is(err, ErrNotFound) {
		httpapi.Error(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	if err != nil {
		m.internal(w, r, "projects: detail", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, p.detail())
}

func (m *Module) adminList(w http.ResponseWriter, r *http.Request) {
	rows, err := m.query(r.Context(), `true`)
	if err != nil {
		m.internal(w, r, "projects: admin list", err)
		return
	}
	views, err := m.views30d(r.Context())
	if err != nil {
		m.internal(w, r, "projects: views", err)
		return
	}
	out := make([]AdminProject, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].admin(views[rows[i].Slug]))
	}
	httpapi.JSON(w, http.StatusOK, out)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpapi.Error(w, http.StatusBadRequest, "bad_request", "invalid id")
		return 0, false
	}
	return id, true
}

func (m *Module) adminGet(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := m.one(r.Context(), m.db, `id = $1`, id)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	a, err := m.adminOne(r.Context(), p)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, a)
}

func (m *Module) adminCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title string `json:"title"`
		Slug  string `json:"slug"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	a, err := m.createManual(r.Context(), in.Title, in.Slug)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusCreated, a)
}

func (m *Module) adminPatch(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in Patch
	if !httpapi.Decode(w, r, &in) {
		return
	}
	p, becameVisible, err := m.patch(r.Context(), id, in)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	if becameVisible && p.GitHubID != nil {
		m.syncer.Kick(p.ID)
	}
	a, err := m.adminOne(r.Context(), p)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	httpapi.JSON(w, http.StatusOK, a)
}

func (m *Module) adminDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	found, err := m.remove(r.Context(), id)
	if err != nil {
		m.fail(w, r, err)
		return
	}
	if !found {
		httpapi.Error(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (m *Module) adminReorder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if len(in.IDs) > 500 {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", "at most 500 ids")
		return
	}
	if in.IDs == nil {
		in.IDs = []int64{}
	}
	if err := m.reorder(r.Context(), in.IDs); err != nil {
		m.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

const (
	// adminSyncTimeout bounds an admin-triggered GitHub sync.
	adminSyncTimeout = 5 * time.Minute
	// adminSyncWait is how long the request waits for it: below the 60 s
	// admin request timeout, the 2 min WriteTimeout and Cloudflare's ~100 s.
	adminSyncWait = 50 * time.Second
)

type syncOutcome struct {
	res SyncResult
	err error
}

func (m *Module) adminSync(w http.ResponseWriter, r *http.Request) {
	if m.syncer == nil || m.syncer.user == "" {
		httpapi.Error(w, http.StatusServiceUnavailable, "github_disabled", "GITHUB_USER is not configured")
		return
	}
	if !m.syncer.mu.TryLock() {
		httpapi.Error(w, http.StatusConflict, "sync_running", "a sync is already running")
		return
	}
	// The sync outlives the request timeout and a client that goes away:
	// cancelling it mid-way would roll back the upsert and leave details
	// half done. It runs in the background under its own, longer bound and
	// holds the lock until it ends, so syncs never overlap.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), adminSyncTimeout)
	done := make(chan syncOutcome, 1)
	go func() {
		defer cancel()
		defer m.syncer.mu.Unlock()
		res, err := m.syncer.syncLocked(ctx)
		done <- syncOutcome{res, err}
	}()
	wait := time.NewTimer(m.syncWait)
	defer wait.Stop()
	select {
	case o := <-done:
		if o.err != nil {
			m.fail(w, r, o.err)
			return
		}
		httpapi.JSON(w, http.StatusOK, o.res)
	case <-wait.C:
		m.syncDetached(w, done)
	case <-r.Context().Done():
		m.syncDetached(w, done)
	}
}

// syncDetached answers 202 while the sync goes on, and logs its outcome.
func (m *Module) syncDetached(w http.ResponseWriter, done <-chan syncOutcome) {
	httpapi.JSON(w, http.StatusAccepted, map[string]string{"status": "running"})
	log := m.log
	go func() {
		o := <-done
		if o.err != nil {
			log.Warn("projects: background github sync failed", logging.Err(o.err))
			return
		}
		log.Info("projects: background github sync done", slog.Int("added", o.res.Added),
			slog.Int("updated", o.res.Updated), slog.Int("total", o.res.Total))
	}()
}

// fail maps errors to the shared error codes.
func (m *Module) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve *ValidationError
	var ge *GitHubError
	switch {
	case errors.As(err, &ve):
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", ve.Msg)
	case errors.Is(err, ErrNotFound):
		httpapi.Error(w, http.StatusNotFound, "not_found", "project not found")
	case errors.Is(err, ErrSlugTaken):
		httpapi.Error(w, http.StatusConflict, "slug_taken", "that slug is already used by another project")
	case errors.As(err, &ge) && ge.RateLimited:
		httpapi.Error(w, http.StatusServiceUnavailable, "github_rate_limited",
			"GitHub rate limit reached; resets at "+ge.Reset.UTC().Format(time.RFC3339))
	case errors.As(err, &ge):
		m.log.WarnContext(r.Context(), "projects: github", logging.Err(err))
		httpapi.Error(w, http.StatusServiceUnavailable, "github_unavailable", "GitHub is unavailable, try again later")
	default:
		m.internal(w, r, "projects", err)
	}
}

func (m *Module) internal(w http.ResponseWriter, r *http.Request, msg string, err error) {
	m.log.ErrorContext(r.Context(), msg, logging.Err(err))
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
}
