// Package httpapi provides the public REST router and HTTP lifecycle.
package httpapi

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"
)

type requestIDKey struct{}

// RequestID returns the server-generated correlation ID.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *responseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Middleware adds correlation, JSON access logs, panic recovery and a fixed Tauri allowlist.
func Middleware(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := rand.Text()
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
		rw := &responseWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(r.Context(), "handler panic", "request_id", id)
				if rw.status != 0 {
					panic(http.ErrAbortHandler)
				}
				writeJSON(rw, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			code := rw.status
			if code == 0 {
				code = http.StatusOK
			}
			logger.InfoContext(r.Context(), "http request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", code, "duration_ms", time.Since(start).Milliseconds())
		}()
		origin := r.Header.Get("Origin")
		rw.Header().Add("Vary", "Origin")
		if origin != "" {
			switch origin {
			case "tauri://localhost", "http://tauri.localhost", "https://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420":
				rw.Header().Set("Access-Control-Allow-Origin", origin)
				rw.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				rw.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, If-None-Match")
				rw.Header().Set("Access-Control-Expose-Headers", "X-Request-ID, ETag, Cache-Control")
			default:
				writeJSON(rw, http.StatusForbidden, map[string]string{"error": "origin not allowed"})
				return
			}
			if r.Method == http.MethodOptions {
				rw.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(rw, r)
	})
}
