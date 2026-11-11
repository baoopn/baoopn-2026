package httpapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
)

type ipKey struct{}

// ClientIP returns the client IP stored by TrustedClientIP.
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(ipKey{}).(string); ok {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Country returns the visitor country forwarded by the web server
// (from Cloudflare's CF-IPCountry), or "".
func Country(r *http.Request) string {
	if r.Header.Get("X-Internal-Token") == "" {
		return ""
	}
	c := strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Client-Country")))
	if len(c) != 2 || c == "XX" || c == "T1" {
		return ""
	}
	return c
}

// TrustedClientIP takes the client IP from X-Client-IP only when the request
// carries the shared internal token (it came through the SvelteKit server).
// Without a token configured (development) the header is trusted as-is.
func TrustedClientIP(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-Internal-Token")
			trusted := token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
			if !trusted {
				r.Header.Del("X-Internal-Token")
				r.Header.Del("X-Client-Country")
			}
			if ip := strings.TrimSpace(r.Header.Get("X-Client-IP")); trusted && net.ParseIP(ip) != nil {
				r = r.WithContext(context.WithValue(r.Context(), ipKey{}, ip))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit limits requests per client IP per minute (IPv6 bucketed by /64).
func RateLimit(perMinute int) func(http.Handler) http.Handler {
	return RateLimitWindow(perMinute, time.Minute)
}

// RateLimitWindow limits requests per client IP to n per window.
func RateLimitWindow(n int, window time.Duration) func(http.Handler) http.Handler {
	if n <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	return httprate.LimitBy(n, window,
		func(r *http.Request) (string, error) { return ipBucket(ClientIP(r)), nil },
		httprate.WithLimitHandler(func(w http.ResponseWriter, _ *http.Request) {
			Error(w, http.StatusTooManyRequests, "rate_limited", "too many requests, slow down")
		}))
}

// ClientBucket is the per-client key used by the rate limits: the client IP,
// with IPv6 addresses bucketed by /64.
func ClientBucket(r *http.Request) string { return ipBucket(ClientIP(r)) }

func ipBucket(ip string) string {
	p := net.ParseIP(ip)
	if p == nil || p.To4() != nil {
		return ip
	}
	return p.Mask(net.CIDRMask(64, 128)).String()
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func accessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			if r.URL.Path == "/healthz" {
				return
			}
			lvl := slog.LevelInfo
			if ww.Status() >= 500 {
				lvl = slog.LevelError
			}
			log.LogAttrs(r.Context(), lvl, "http",
				slog.String("method", r.Method), slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()), slog.Duration("dur", time.Since(start)),
				slog.String("req_id", middleware.GetReqID(r.Context())))
		})
	}
}
