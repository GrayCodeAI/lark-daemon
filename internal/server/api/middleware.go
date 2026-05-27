package api

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// rateLimitMiddleware returns an HTTP handler that rate-limits per IP.
// Applies stricter limits to mutation endpoints.
func rateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx != -1 {
				ip = ip[:idx]
			}
			if !rl.Allow(ip) {
				writeErrorCode(w, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded")
				return
			}
			// Deduct extra token for mutations (POST, PATCH, DELETE)
			if r.Method == "POST" || r.Method == "PATCH" || r.Method == "DELETE" {
				if !rl.Allow(ip) {
					writeErrorCode(w, http.StatusTooManyRequests, ErrCodeRateLimited, "rate limit exceeded")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestLoggerMiddleware logs every request with method, path, status, and duration.
func requestLoggerMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).String(),
			)
		})
	}
}

// auditLogMiddleware logs all mutation operations (POST, PATCH, DELETE).
func auditLogMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == "POST" || r.Method == "PATCH" || r.Method == "DELETE" {
				member := memberFromContext(r)
				uid := "anonymous"
				if member != nil {
					uid = member.ID
				}
				logger.Info("audit", "method", r.Method, "path", r.URL.Path, "user", uid)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// cacheControlMiddleware sets Cache-Control: no-cache on all API responses
// to prevent stale data caching in real-time collaboration.
func cacheControlMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware adds standard security headers to all responses.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0") // disable legacy XSS filter; CSP is preferred
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		next.ServeHTTP(w, r)
	})
}
