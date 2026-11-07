// Package overview serves the admin dashboard summary (GET /api/admin/overview)
// and the signed-in admin (GET /api/admin/me).
package overview

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/baoopn/baoopn-2026/internal/analytics"
	"github.com/baoopn/baoopn-2026/internal/auth"
	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

// Deps are the module's dependencies.
type Deps struct {
	DB        *pgxpool.Pool
	Log       *slog.Logger
	Analytics *analytics.Recorder
}

// Module implements httpapi.Module.
type Module struct {
	db  *pgxpool.Pool
	log *slog.Logger
	rec *analytics.Recorder
	now func() time.Time
}

// New creates the module.
func New(d Deps) *Module {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	return &Module{db: d.DB, log: log, rec: d.Analytics, now: time.Now}
}

// Overview is the dashboard summary (Overview in types.ts).
type Overview struct {
	UnreadThreads int64                 `json:"unread_threads"`
	OpenThreads   int64                 `json:"open_threads"`
	Posts         PostCounts            `json:"posts"`
	Projects      ProjectCounts         `json:"projects"`
	Views7d       int64                 `json:"views_7d"`
	Visitors7d    int64                 `json:"visitors_7d"`
	LiveNow       int                   `json:"live_now"`
	LastSyncAt    *time.Time            `json:"last_sync_at"`
	RecentThreads []threadSummary       `json:"recent_threads"`
	TopPaths7d    []analytics.Breakdown `json:"top_paths_7d"`
}

// PostCounts counts posts by state.
type PostCounts struct {
	Published int64 `json:"published"`
	Drafts    int64 `json:"drafts"`
	Scheduled int64 `json:"scheduled"`
}

// ProjectCounts counts projects.
type ProjectCounts struct {
	Visible int64 `json:"visible"`
	Total   int64 `json:"total"`
}

// threadSummary mirrors ThreadSummary in types.ts (the contact package owns
// the full thread API; this is a read-only copy of its shape).
type threadSummary struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Subject       string    `json:"subject"`
	Status        string    `json:"status"`
	Unread        bool      `json:"unread"`
	LastMessageAt time.Time `json:"last_message_at"`
	LastDirection string    `json:"last_direction"`
	LastPreview   string    `json:"last_preview"`
	MessageCount  int       `json:"message_count"`
	CreatedAt     time.Time `json:"created_at"`
}

// Public mounts nothing.
func (m *Module) Public(chi.Router) {}

// Hooks mounts nothing.
func (m *Module) Hooks(chi.Router) {}

// Admin mounts GET /me and GET /overview.
func (m *Module) Admin(r chi.Router) {
	r.Get("/me", auth.MeHandler)
	r.Get("/overview", m.handle)
}

func (m *Module) handle(w http.ResponseWriter, r *http.Request) {
	ov, err := m.Get(r.Context())
	if err != nil {
		m.log.ErrorContext(r.Context(), "overview", logging.Err(err))
		httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
		return
	}
	httpapi.JSON(w, http.StatusOK, ov)
}

// Get builds the overview.
func (m *Module) Get(ctx context.Context) (Overview, error) {
	if m.rec != nil {
		if err := m.rec.Flush(ctx); err != nil {
			m.log.WarnContext(ctx, "overview: analytics flush failed", logging.Err(err))
		}
	}
	var ov Overview
	err := m.db.QueryRow(ctx, `
		SELECT
		 (SELECT count(*) FROM threads WHERE status='open' AND read_at IS NULL),
		 (SELECT count(*) FROM threads WHERE status='open'),
		 (SELECT count(*) FROM posts WHERE status='published' AND published_at <= now()),
		 (SELECT count(*) FROM posts WHERE status='draft'),
		 (SELECT count(*) FROM posts WHERE status='published' AND published_at > now()),
		 (SELECT count(*) FROM projects WHERE visible), (SELECT count(*) FROM projects),
		 (SELECT max(synced_at) FROM projects),
		 (SELECT coalesce(sum(views),0)::bigint FROM page_view_stats WHERE bucket >= now() - interval '7 days'),
		 (SELECT count(DISTINCT visitor) FROM page_visitor_hours WHERE hour >= now() - interval '7 days')`).Scan(
		&ov.UnreadThreads, &ov.OpenThreads, &ov.Posts.Published, &ov.Posts.Drafts, &ov.Posts.Scheduled,
		&ov.Projects.Visible, &ov.Projects.Total, &ov.LastSyncAt, &ov.Views7d, &ov.Visitors7d)
	if err != nil {
		return Overview{}, fmt.Errorf("overview counts: %w", err)
	}
	if ov.LastSyncAt != nil {
		t := ov.LastSyncAt.UTC()
		ov.LastSyncAt = &t
	}
	if ov.RecentThreads, err = m.recentThreads(ctx); err != nil {
		return Overview{}, err
	}
	ov.TopPaths7d = []analytics.Breakdown{}
	if m.rec != nil {
		ov.LiveNow = m.rec.LiveNow()
		if ov.TopPaths7d, err = m.rec.TopPaths(ctx, m.now().Add(-7*24*time.Hour), 5); err != nil {
			return Overview{}, err
		}
	}
	return ov, nil
}

func (m *Module) recentThreads(ctx context.Context) ([]threadSummary, error) {
	rows, err := m.db.Query(ctx, `
		SELECT id, name, email, subject, status, read_at IS NULL, last_message_at, last_direction,
		       last_preview, message_count, created_at
		FROM threads WHERE status='open' ORDER BY last_message_at DESC, id DESC LIMIT 5`)
	if err != nil {
		return nil, fmt.Errorf("overview threads: %w", err)
	}
	defer rows.Close()
	out := []threadSummary{}
	for rows.Next() {
		var t threadSummary
		if err := rows.Scan(&t.ID, &t.Name, &t.Email, &t.Subject, &t.Status, &t.Unread, &t.LastMessageAt,
			&t.LastDirection, &t.LastPreview, &t.MessageCount, &t.CreatedAt); err != nil {
			return nil, err
		}
		t.LastMessageAt, t.CreatedAt = t.LastMessageAt.UTC(), t.CreatedAt.UTC()
		out = append(out, t)
	}
	return out, rows.Err()
}
