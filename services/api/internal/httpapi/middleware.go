// Package httpapi provides the public REST router and HTTP lifecycle.
package httpapi

import (
	"context"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"log/slog"
	"net/http"
)

// RequestID returns the server-generated correlation ID.
func RequestID(ctx context.Context) string {
	return telemetry.CorrelationFrom(ctx).RequestID
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
func Middleware(logger *slog.Logger, next http.Handler, monitors ...*telemetry.Monitor) http.Handler {
	monitor := telemetry.New(logger)
	if len(monitors) > 0 && monitors[0] != nil {
		monitor = monitors[0]
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, end := monitor.Start(r.Context(), "http")
		id := telemetry.CorrelationFrom(ctx).RequestID
		w.Header().Set("X-Request-ID", id)
		r = r.WithContext(ctx)
		rw := &responseWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.ErrorContext(r.Context(), "handler panic", "request_id", id)
				if rw.status != 0 {
					end(false)
					panic(http.ErrAbortHandler)
				}
				writeJSON(rw, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			}
			code := rw.status
			if code == 0 {
				code = http.StatusOK
			}
			end(code < 500)
			logger.InfoContext(ctx, "http request", "request_id", id, "status", code)
		}()
		origin := r.Header.Get("Origin")
		rw.Header().Add("Vary", "Origin")
		if origin != "" {
			switch origin {
			case "tauri://localhost", "http://tauri.localhost", "https://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420":
				rw.Header().Set("Access-Control-Allow-Origin", origin)
				rw.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				rw.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID, X-Quote-ID, If-None-Match")
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
