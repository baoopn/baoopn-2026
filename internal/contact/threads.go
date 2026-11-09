package contact

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/baoopn/baoopn-2026/internal/httpapi"
	"github.com/baoopn/baoopn-2026/internal/logging"
)

var statuses = map[string]bool{"open": true, "archived": true, "spam": true}

func (m *Module) internal(w http.ResponseWriter, what string, err error) {
	m.d.Log.Error("contact: "+what, logging.Err(err))
	httpapi.Error(w, http.StatusInternalServerError, "internal", "internal error")
}

func notFound(w http.ResponseWriter) {
	httpapi.Error(w, http.StatusNotFound, "not_found", "thread not found")
}

// idParam reads a positive integer URL parameter.
func idParam(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, name), 10, 64)
	return id, err == nil && id > 0
}

func (m *Module) handleList(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "open"
	}
	if !statuses[status] {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", "status must be open, archived or spam")
		return
	}
	sql := `SELECT ` + summaryCols + ` FROM threads WHERE status = $1`
	args := []any{status}
	if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
		sql += ` AND (name ILIKE $2 ESCAPE '\' OR email ILIKE $2 ESCAPE '\' OR subject ILIKE $2 ESCAPE '\' OR last_preview ILIKE $2 ESCAPE '\')`
		args = append(args, likePattern(clip(q, 200)))
	}
	sql += ` ORDER BY last_message_at DESC, id DESC LIMIT 200`
	rows, err := m.d.DB.Query(r.Context(), sql, args...)
	if err != nil {
		m.internal(w, "list threads", err)
		return
	}
	defer rows.Close()
	out := []ThreadSummary{}
	for rows.Next() {
		t, err := scanSummary(rows)
		if err != nil {
			m.internal(w, "scan thread", err)
			return
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		m.internal(w, "list threads", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, out)
}

func (m *Module) handleUnreadCount(w http.ResponseWriter, r *http.Request) {
	var n int64
	if err := m.d.DB.QueryRow(r.Context(),
		`SELECT count(*) FROM threads WHERE read_at IS NULL AND status = 'open'`).Scan(&n); err != nil {
		m.internal(w, "unread count", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, map[string]int64{"count": n})
}

func (m *Module) handleDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		notFound(w)
		return
	}
	t, found, err := loadThread(r.Context(), m.d.DB, id)
	if err != nil {
		m.internal(w, "load thread", err)
		return
	}
	if !found {
		notFound(w)
		return
	}
	rows, err := m.d.DB.Query(r.Context(),
		`SELECT `+messageCols+` FROM thread_messages WHERE thread_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		m.internal(w, "list messages", err)
		return
	}
	defer rows.Close()
	msgs := []ThreadMessage{}
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			m.internal(w, "scan message", err)
			return
		}
		msgs = append(msgs, msg)
	}
	if err := rows.Err(); err != nil {
		m.internal(w, "list messages", err)
		return
	}
	httpapi.JSON(w, http.StatusOK, ThreadDetail{Thread: t.ThreadSummary, Messages: msgs, ReplyEnabled: m.d.Mailer.Enabled()})
}

func (m *Module) handleRead(read bool) http.HandlerFunc {
	// The statement returns whether the thread changed, so a no-op (already
	// read, already unread) publishes nothing.
	sql := `WITH prev AS (SELECT id, read_at IS NULL AS changed FROM threads WHERE id = $1 FOR UPDATE)
		UPDATE threads t SET read_at = coalesce(t.read_at, now()) FROM prev WHERE t.id = prev.id RETURNING prev.changed`
	change := "read"
	if !read {
		sql = `WITH prev AS (SELECT id, read_at IS NOT NULL AS changed FROM threads WHERE id = $1 FOR UPDATE)
		UPDATE threads t SET read_at = NULL FROM prev WHERE t.id = prev.id RETURNING prev.changed`
		change = "unread"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := idParam(r, "id")
		if !ok {
			notFound(w)
			return
		}
		var changed bool
		err := m.d.DB.QueryRow(r.Context(), sql, id).Scan(&changed)
		if isNoRows(err) {
			notFound(w)
			return
		}
		if err != nil {
			m.internal(w, "mark read", err)
			return
		}
		if changed {
			m.events.publish(Event{Type: evThreadUpdated, ThreadID: id, Change: change})
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (m *Module) handlePatch(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		notFound(w)
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !httpapi.Decode(w, r, &in) {
		return
	}
	if !statuses[in.Status] {
		httpapi.Error(w, http.StatusUnprocessableEntity, "validation", "status must be open, archived or spam")
		return
	}
	t, err := scanSummary(m.d.DB.QueryRow(r.Context(),
		`UPDATE threads SET status = $2 WHERE id = $1 RETURNING `+summaryCols, id, in.Status))
	if isNoRows(err) {
		notFound(w)
		return
	}
	if err != nil {
		m.internal(w, "patch thread", err)
		return
	}
	m.events.publish(Event{Type: evThreadUpdated, ThreadID: id, Change: "status", Status: t.Status})
	httpapi.JSON(w, http.StatusOK, t)
}

func (m *Module) handleDelete(w http.ResponseWriter, r *http.Request) {
	id, ok := idParam(r, "id")
	if !ok {
		notFound(w)
		return
	}
	tag, err := m.d.DB.Exec(r.Context(), `DELETE FROM threads WHERE id = $1`, id)
	if err != nil {
		m.internal(w, "delete thread", err)
		return
	}
	if tag.RowsAffected() == 0 {
		notFound(w)
		return
	}
	m.events.publish(Event{Type: evThreadDeleted, ThreadID: id})
	w.WriteHeader(http.StatusNoContent)
}
