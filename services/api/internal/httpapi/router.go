package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
)

// Engine describes the bounded engine calls used by the HTTP layer.
type Engine interface {
	Health(context.Context) error
	PreviewCourse(context.Context, *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error)
	SetupChat(context.Context, *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error)
}

// Dependencies allows readiness probes without coupling HTTP to database drivers.
type Dependencies struct {
	Postgres, Redis func(context.Context) error
	ObjectStorage   func(context.Context) error
	Monitor         *telemetry.Monitor
	Engine          Engine
	Pricing         http.Handler
	CourseBrief     http.Handler
	PricingAdmin    http.Handler
	Quotes          http.Handler
	Credits         http.Handler
	Accounts        interface {
		http.Handler
		Protect(http.Handler) http.Handler
	}
}

// New creates the scaffold routes. Courses are static demonstration data.
func New(deps Dependencies, logger *slog.Logger) http.Handler {
	if deps.Monitor == nil {
		deps.Monitor = telemetry.New(logger)
	}
	mux := http.NewServeMux()
	if deps.Accounts != nil {
		mux.Handle("/v1/auth/", deps.Accounts)
		mux.Handle("GET /v1/me", deps.Accounts)
		mux.Handle("/v1/me/", deps.Accounts)
	} else {
		mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
		})
	}
	if deps.Pricing != nil {
		mux.Handle("GET /v1/pricing", deps.Pricing)
	} else {
		mux.HandleFunc("GET /v1/pricing", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "pricing unavailable"})
		})
	}
	if deps.PricingAdmin != nil && deps.Accounts != nil {
		if protected, ok := deps.Accounts.(interface {
			ProtectWithDenied(http.Handler, func(*http.Request)) http.Handler
		}); ok {
			if audit, ok := deps.PricingAdmin.(interface{ RecordDenied(*http.Request) }); ok {
				mux.Handle("/admin/v1/pricing/", protected.ProtectWithDenied(deps.PricingAdmin, audit.RecordDenied))
			} else {
				mux.Handle("/admin/v1/pricing/", deps.Accounts.Protect(deps.PricingAdmin))
			}
		} else {
			mux.Handle("/admin/v1/pricing/", deps.Accounts.Protect(deps.PricingAdmin))
		}
	} else {
		mux.HandleFunc("/admin/v1/pricing/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "pricing administration unavailable"})
		})
	}
	if deps.CourseBrief != nil && deps.Accounts != nil {
		mux.Handle("/v1/course-brief/", deps.Accounts.Protect(deps.CourseBrief))
		if outlines, ok := deps.CourseBrief.(interface {
			ServeOutlineHTTP(http.ResponseWriter, *http.Request)
		}); ok {
			handler := deps.Accounts.Protect(http.HandlerFunc(outlines.ServeOutlineHTTP))
			mux.Handle("/v1/outline-drafts", handler)
			mux.Handle("/v1/outline-drafts/", handler)
		}
	}
	if deps.Quotes != nil {
		mux.Handle("POST /v1/quotes", deps.Quotes)
		mux.Handle("GET /v1/quotes/{id}", deps.Quotes)
		mux.Handle("POST /v1/quotes/{id}/accept", deps.Quotes)
	}
	if deps.Credits != nil && deps.Accounts != nil {
		mux.Handle("POST /v1/payments/stripe/webhook", deps.Credits)
		mux.Handle("POST /v1/credits/checkouts", deps.Accounts.Protect(deps.Credits))
		mux.Handle("POST /v1/credits/subscription/change", deps.Accounts.Protect(deps.Credits))
		mux.Handle("GET /v1/credits/checkouts/{id}", deps.Accounts.Protect(deps.Credits))
		mux.Handle("POST /v1/quotes/{id}/reserve", deps.Accounts.Protect(deps.Credits))
		mux.Handle("POST /v1/credits/reservations/{id}/release", deps.Accounts.Protect(deps.Credits))
		mux.Handle("GET /v1/me/credits", deps.Accounts.Protect(deps.Credits))
		mux.Handle("GET /v1/me/credits/activity", deps.Accounts.Protect(deps.Credits))
		mux.Handle("GET /v1/credits/wallet", deps.Accounts.Protect(deps.Credits))
		mux.Handle("GET /v1/credits/activity", deps.Accounts.Protect(deps.Credits))
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]func(context.Context) error{"postgres": deps.Postgres, "redis": deps.Redis, "object_storage": deps.ObjectStorage}
		if deps.Engine != nil {
			checks["engine"] = deps.Engine.Health
		} else {
			checks["engine"] = nil
		}
		results := make(map[string]string, len(checks))
		code := http.StatusOK
		var mu sync.Mutex
		var probes sync.WaitGroup
		// Concurrent probes keep a slow dependency from hiding healthy dependencies.
		for name, check := range checks {
			probes.Go(func() {
				probeCtx, end := deps.Monitor.Start(ctx, "dependency")
				ok := check != nil && check(probeCtx) == nil
				end(ok)
				deps.Monitor.Incident(probeCtx, "dependency", name, ok)
				mu.Lock()
				defer mu.Unlock()
				results[name] = "ok"
				if !ok {
					results[name] = "error"
					code = http.StatusServiceUnavailable
				}
			})
		}
		probes.Wait()
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
		w.Header().Set("Cache-Control", "no-store")
		var in struct {
			BriefRevision string `json:"brief_revision"`
		}
		quoteID := r.Header.Get("X-Quote-ID")
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
		decoder.DisallowUnknownFields()
		var extra any
		if decoder.Decode(&in) != nil || decoder.Decode(&extra) != io.EOF || in.BriefRevision == "" || quoteID == "" {
			writeJSON(w, 400, map[string]string{"error": "confirmed brief revision and quote required"})
			return
		}
		preparer, ok := deps.CourseBrief.(interface {
			PreparePreview(context.Context, string, string, string) (*enginev1.PreviewCourseRequest, error)
			PreviewEvent(context.Context, string, string, *enginev1.PreviewCourseRequest, string, int64)
		})
		if !ok {
			writeJSON(w, 503, map[string]string{"error": "brief preview unavailable"})
			return
		}
		owner := accounts.LearnerID(r.Context())
		request, err := preparer.PreparePreview(r.Context(), owner, in.BriefRevision, quoteID)
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "fresh accepted quote for the confirmed brief required"})
			return
		}
		if deps.Engine == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "engine unavailable"})
			return
		}
		financial, ok := deps.Credits.(interface {
			ReserveAction(context.Context, string, string, string) (credits.Reservation, error)
			Limits(context.Context, string, string, string) (*enginev1.UsageLimits, error)
			Claim(context.Context, string, string) error
			Settle(context.Context, string, string, []*enginev1.Usage) (credits.Reservation, error)
			Release(context.Context, string, string) (credits.Reservation, error)
		})
		if !ok {
			writeJSON(w, 503, map[string]string{"error": "credit reservation unavailable"})
			return
		}
		job, err := financial.ReserveAction(r.Context(), owner, quoteID, "preview")
		if err != nil {
			message := "Accepted eligible quote required. Renew the quote and retry."
			if errors.Is(err, credits.ErrInsufficient) {
				message = "Not enough eligible purchased credits. Daily free credits cannot pay for previews. Your brief is kept."
			}
			if errors.Is(err, credits.ErrBudget) || errors.Is(err, credits.ErrConcurrency) {
				message = "Generation limit reached. Your brief is kept; wait and retry."
			}
			writeJSON(w, 409, map[string]string{"error": message})
			return
		}
		if job.State != "reserved" {
			writeJSON(w, 409, map[string]string{"error": "quote already finalized"})
			return
		}
		// A failure before provider work also releases the reserved component.
		finished := false
		claimed := false
		defer func() {
			if !finished {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
				defer cancel()
				if claimed {
					_, _ = financial.Settle(ctx, owner, job.ID, nil)
				} else {
					_, _ = financial.Release(ctx, owner, job.ID)
				}
			}
		}()
		// API resolves bounds from the pinned quote.
		request.Limits, err = financial.Limits(r.Context(), owner, job.ID, "preview")
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "quote bounds unavailable"})
			return
		}
		if err = financial.Claim(r.Context(), owner, job.ID); err != nil {
			writeJSON(w, 409, map[string]string{"error": "Generation queue unavailable. Refresh your wallet and retry."})
			return
		}
		claimed = true
		started := time.Now()
		preparer.PreviewEvent(r.Context(), owner, "course_preview_requested", request, job.ID, 0)
		providerCtx, cancel := context.WithTimeout(r.Context(), time.Duration(request.Limits.TimeoutMs)*time.Millisecond)
		defer cancel()
		response, err := deps.Engine.PreviewCourse(providerCtx, request)
		valid := err == nil && courses.ValidatePreview(response) == nil && len(response.Attempts) > 0 && response.Attempts[len(response.Attempts)-1].GetDelivered()
		if !valid && response != nil {
			for _, attempt := range response.Attempts {
				if attempt != nil {
					attempt.Delivered = false
				}
			}
		}
		var attempts []*enginev1.Usage
		if response != nil {
			attempts = response.Attempts
		}
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Duration(request.Limits.TimeoutMs)*time.Millisecond)
		defer finishCancel()
		settled, finishErr := financial.Settle(finishCtx, owner, job.ID, attempts)
		finished = finishErr == nil || settled.State == "released" || settled.State == "settled"
		if !valid || finishErr != nil {
			preparer.PreviewEvent(finishCtx, owner, "course_generation_failed", request, job.ID, time.Since(started).Milliseconds())
		}
		if finishErr != nil {
			writeJSON(w, 502, map[string]string{"error": "usage verification failed"})
			return
		}
		if err == nil && (!valid || settled.State != "settled") {
			writeJSON(w, 502, map[string]string{"error": "Preview unavailable. Reserved credits were released. Your brief is kept."})
			return
		}
		if err != nil {
			code := http.StatusBadGateway
			if errors.Is(err, context.DeadlineExceeded) || status.Code(err) == codes.DeadlineExceeded {
				code = http.StatusGatewayTimeout
			}
			logger.WarnContext(r.Context(), "engine preview failed", "request_id", RequestID(r.Context()), "code", status.Code(err).String())
			writeJSON(w, code, map[string]string{"error": "Preview unavailable. Reserved credits were released. Your brief is kept."})
			return
		}
		preparer.PreviewEvent(finishCtx, owner, "course_preview_viewed", request, job.ID, time.Since(started).Milliseconds())
		body, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(response)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "invalid engine response"})
			return
		}
		writeJSON(w, http.StatusOK, json.RawMessage(body))
	})
	// Public health and pricing stay available; learner-owned routes require auth.
	guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/courses") || strings.HasPrefix(r.URL.Path, "/v1/quotes") || strings.HasPrefix(r.URL.Path, "/v1/credits") {
			if deps.Accounts == nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
				return
			}
			deps.Accounts.Protect(mux).ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return Middleware(logger, guard, deps.Monitor)
}
func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.Debug("response write failed")
	}
}
