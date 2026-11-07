// Package httpapi wires the HTTP routes: the public API (/api/v1), the admin
// API (/api/admin, Clerk-protected), webhooks (/api/hooks) and media (/media).
package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Module mounts a feature's routes. Any method may be a no-op.
type Module interface {
	Public(r chi.Router) // /api/v1
	Admin(r chi.Router)  // /api/admin (authenticated)
	Hooks(r chi.Router)  // /api/hooks (signed webhooks)
}

// Streamer is implemented by modules with long-lived public routes (SSE).
// They are mounted under /api/v1 with the rate limit but without the request
// timeout, and must clear their own connection deadlines.
type Streamer interface {
	Streams(r chi.Router)
}

// Deps are the shared dependencies of the router.
type Deps struct {
	Log       *slog.Logger
	DB        *pgxpool.Pool
	AdminAuth func(http.Handler) http.Handler // nil = admin disabled
	ClientIP  func(http.Handler) http.Handler
	PublicRPM int
	Media     http.Handler // serves /media/*
	Modules   []Module
	Version   string
	// AdminConfig serves GET /api/admin/config (unauthenticated: Clerk key).
	AdminConfig http.HandlerFunc
}

// New builds the root handler.
func New(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer, securityHeaders)
	if d.ClientIP != nil {
		r.Use(d.ClientIP)
	}
	r.Use(accessLog(d.Log))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok", "version": d.Version})
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := d.DB.Ping(r.Context()); err != nil {
			Error(w, http.StatusServiceUnavailable, "db_unavailable", "database unreachable")
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})
	if d.Media != nil {
		r.Handle("/media/*", d.Media)
	}

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(RateLimit(d.PublicRPM))
		r.Group(func(r chi.Router) {
			r.Use(middleware.Timeout(20 * time.Second))
			for _, m := range d.Modules {
				m.Public(r)
			}
		})
		for _, m := range d.Modules {
			if s, ok := m.(Streamer); ok {
				s.Streams(r)
			}
		}
	})

	r.Route("/api/hooks", func(r chi.Router) {
		r.Use(middleware.Timeout(30*time.Second), RateLimit(120))
		for _, m := range d.Modules {
			m.Hooks(r)
		}
	})

	r.Route("/api/admin", func(r chi.Router) {
		r.Use(noCache, RateLimit(600), middleware.Timeout(60*time.Second))
		if d.AdminConfig != nil {
			r.Get("/config", d.AdminConfig)
		}
		r.Group(func(r chi.Router) {
			if d.AdminAuth == nil {
				r.Use(func(http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
						Error(w, http.StatusServiceUnavailable, "admin_disabled", "admin auth is not configured")
					})
				})
			} else {
				r.Use(d.AdminAuth)
			}
			for _, m := range d.Modules {
				m.Admin(r)
			}
		})
	})

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		Error(w, http.StatusNotFound, "not_found", "no such route")
	})
	return r
}
