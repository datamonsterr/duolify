package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// Engine describes the bounded engine calls used by the HTTP layer.
type Engine interface {
	Health(context.Context) error
	PreviewCourse(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error)
}

// Dependencies allows readiness probes without coupling HTTP to database drivers.
type Dependencies struct {
	Postgres, Redis func(context.Context) error
	Engine          Engine
	Pricing         http.Handler
}

// New creates the scaffold routes. Courses are static demonstration data.
func New(deps Dependencies, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	if deps.Pricing != nil {
		mux.Handle("GET /v1/pricing", deps.Pricing)
	} else {
		mux.HandleFunc("GET /v1/pricing", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "pricing unavailable"})
		})
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]func(context.Context) error{"postgres": deps.Postgres, "redis": deps.Redis}
		if deps.Engine != nil {
			checks["engine"] = deps.Engine.Health
		} else {
			checks["engine"] = nil
		}
		results := make(map[string]string, len(checks))
		code := http.StatusOK
		// Every probe receives the same overall deadline, bounding total readiness time.
		for name, check := range checks {
			results[name] = "ok"
			if check == nil || check(ctx) != nil {
				results[name] = "error"
				code = http.StatusServiceUnavailable
			}
		}
		writeJSON(w, code, results)
	})
	mux.HandleFunc("GET /v1/courses", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"courses": []map[string]string{{
				"id":          "demo-physics",
				"title":       "Everyday Physics",
				"description": "A demonstration course",
			}},
		})
	})
	mux.HandleFunc("POST /v1/courses/preview", func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		raw, err := io.ReadAll(r.Body)
		request := new(enginev1.PreviewCourseRequest)
		if err != nil || protojson.Unmarshal(raw, request) != nil || strings.TrimSpace(request.GetTopic()) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "a valid JSON body and topic are required"})
			return
		}
		if deps.Engine == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "engine unavailable"})
			return
		}
		response, err := deps.Engine.PreviewCourse(r.Context(), request)
		if err != nil {
			code := http.StatusBadGateway
			if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
				code = http.StatusGatewayTimeout
			}
			logger.WarnContext(r.Context(), "engine preview failed", "request_id", RequestID(r.Context()), "code", status.Code(err).String())
			writeJSON(w, code, map[string]string{"error": "engine preview unavailable"})
			return
		}
		body, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(response)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid engine response"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(body); err != nil {
			logger.DebugContext(r.Context(), "response write failed", "request_id", RequestID(r.Context()))
		}
	})
	return Middleware(logger, mux)
}
func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Debug("response write failed")
	}
}
