package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
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
	Player          http.Handler
	Notes           http.Handler
	Eval            http.Handler
	Characters      http.Handler
	Steering        http.Handler
	Tutor           http.Handler
	Gamification    http.Handler
	Social          http.Handler
	Sharing         interface {
		MountRoutes(*http.ServeMux, func(http.Handler) http.Handler, func(http.Handler) http.Handler)
	}
	// NarrationVoices serves the voice list, quote and consent endpoints; nil when narration is off.
	NarrationVoices         http.Handler
	LessonTestTracerEnabled bool
	LessonTest              http.Handler
	Accounts                interface {
		http.Handler
		Protect(http.Handler) http.Handler
	}
}

// NewWithLessonTest attaches the lesson testing service.
func NewWithLessonTest(deps Dependencies, lessonTest http.Handler, logger *slog.Logger) http.Handler {
	deps.LessonTest = lessonTest
	return New(deps, logger)
}

// New creates the scaffold routes. Courses are static demonstration data.
func New(deps Dependencies, logger *slog.Logger) http.Handler {
	if deps.Monitor == nil {
		deps.Monitor = telemetry.New(logger)
	}
	mux := http.NewServeMux()
	if deps.Gamification != nil && deps.Accounts != nil {
		mux.Handle("/v1/gamification", deps.Accounts.Protect(deps.Gamification))
		mux.Handle("/v1/gamification/", deps.Accounts.Protect(deps.Gamification))
	}
	if deps.Sharing != nil && deps.Accounts != nil {
		optional := func(next http.Handler) http.Handler { return next }
		if o, ok := deps.Accounts.(interface {
			Optional(http.Handler) http.Handler
		}); ok {
			optional = o.Optional
		}
		deps.Sharing.MountRoutes(mux, deps.Accounts.Protect, optional)
	}
	if deps.Social != nil && deps.Accounts != nil {
		mux.Handle("/v1/social/", deps.Accounts.Protect(deps.Social))
	}
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
	mux.HandleFunc("GET /v1/courses/shared-demo", courses.ServeSharedDemoHTTP)
	if deps.CourseBrief != nil && deps.Accounts != nil {
		mux.Handle("/v1/course-brief/", deps.Accounts.Protect(deps.CourseBrief))
		if creation, ok := deps.CourseBrief.(interface {
			ServeCreationHTTP(http.ResponseWriter, *http.Request)
			ServeCourseHTTP(http.ResponseWriter, *http.Request)
			ServeCourseDeleteHTTP(http.ResponseWriter, *http.Request)
			ServeCoursesListHTTP(http.ResponseWriter, *http.Request)
			ServeCourseSelectHTTP(http.ResponseWriter, *http.Request)
			ServeCourseLevelHTTP(http.ResponseWriter, *http.Request)
		}); ok {
			h := deps.Accounts.Protect(http.HandlerFunc(creation.ServeCreationHTTP))
			mux.Handle("/v1/course-creations", h)
			mux.Handle("/v1/course-creations/", h)
			mux.Handle("GET /v1/courses", deps.Accounts.Protect(http.HandlerFunc(creation.ServeCoursesListHTTP)))
			mux.Handle("GET /v1/courses/{id}", deps.Accounts.Protect(http.HandlerFunc(creation.ServeCourseHTTP)))
			mux.Handle("DELETE /v1/courses/{id}", deps.Accounts.Protect(http.HandlerFunc(creation.ServeCourseDeleteHTTP)))
			mux.Handle("POST /v1/courses/{id}/select", deps.Accounts.Protect(http.HandlerFunc(creation.ServeCourseSelectHTTP)))
			mux.Handle("POST /v1/courses/{id}/level", deps.Accounts.Protect(http.HandlerFunc(creation.ServeCourseLevelHTTP)))
			mux.Handle("PUT /v1/courses/{id}/level", deps.Accounts.Protect(http.HandlerFunc(creation.ServeCourseLevelHTTP)))
		}
		if outlines, ok := deps.CourseBrief.(interface {
			ServeOutlineHTTP(http.ResponseWriter, *http.Request)
		}); ok {
			handler := deps.Accounts.Protect(http.HandlerFunc(outlines.ServeOutlineHTTP))
			mux.Handle("/v1/outline-drafts", handler)
			mux.Handle("/v1/outline-drafts/", handler)
		}
		if proposals, ok := deps.CourseBrief.(interface {
			ServeProposalHTTP(http.ResponseWriter, *http.Request)
		}); ok {
			handler := deps.Accounts.Protect(http.HandlerFunc(proposals.ServeProposalHTTP))
			mux.Handle("/v1/outline-proposals", handler)
			mux.Handle("/v1/outline-proposals/", handler)
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
	if deps.Player != nil && deps.Accounts != nil {
		mux.Handle("GET /v1/lessons/{lesson}/manifest", deps.Accounts.Protect(deps.Player))
		mux.Handle("GET /v1/lessons/{lesson}/position", deps.Accounts.Protect(deps.Player))
		mux.Handle("PUT /v1/lessons/{lesson}/position", deps.Accounts.Protect(deps.Player))
		mux.Handle("POST /v1/lessons/{lesson}/sync", deps.Accounts.Protect(deps.Player))
		mux.Handle("GET /v1/lessons/{lesson}/slides/{slide}", deps.Accounts.Protect(deps.Player))
		mux.Handle("GET /v1/lessons/{lesson}/slides/{slide}/narration/{part}", deps.Accounts.Protect(deps.Player))
		mux.Handle("POST /v1/lessons/{lesson}/slides/{slide}/narration/{part}", deps.Accounts.Protect(deps.Player))
		mux.Handle("GET /v1/courses/{course}/download", deps.Accounts.Protect(deps.Player))
	}
	if deps.NarrationVoices != nil && deps.Accounts != nil {
		mux.Handle("GET /v1/me/narration/voices", deps.Accounts.Protect(deps.NarrationVoices))
		mux.Handle("GET /v1/lessons/{lesson}/narration/voice", deps.Accounts.Protect(deps.NarrationVoices))
		mux.Handle("POST /v1/lessons/{lesson}/narration/voice/quote", deps.Accounts.Protect(deps.NarrationVoices))
		mux.Handle("POST /v1/lessons/{lesson}/narration/voice", deps.Accounts.Protect(deps.NarrationVoices))
	}
	if deps.Eval != nil {
		// Ingest authenticates service credentials itself; all reads require a
		// live account session and the eval handler's shared admin policy.
		mux.Handle("POST /v1/eval/reports", deps.Eval)
		if deps.Accounts != nil {
			mux.Handle("/v1/eval/", deps.Accounts.Protect(deps.Eval))
		} else {
			mux.HandleFunc("/v1/eval/", func(w http.ResponseWriter, _ *http.Request) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
			})
		}
	}
	if deps.Tutor != nil && deps.Accounts != nil {
		tutor := deps.Accounts.Protect(deps.Tutor)
		mux.Handle("POST /v1/courses/{id}/tutor/quote", tutor)
		mux.Handle("POST /v1/courses/{id}/tutor/messages", tutor)
		mux.Handle("GET /v1/courses/{id}/tutor/thread", tutor)
		mux.Handle("GET /v1/courses/{id}/tutor/personas", tutor)
		mux.Handle("GET /v1/courses/{id}/tutor/turns/{key}", tutor)
		mux.Handle("POST /v1/courses/{id}/tutor/turns/{key}/stop", tutor)
		mux.Handle("GET /v1/tutor/threads/{id}/messages", tutor)
		mux.Handle("DELETE /v1/tutor/threads/{id}", tutor)
		mux.Handle("POST /v1/tutor/messages/{id}/note", tutor)
	}
	if deps.Notes != nil && deps.Accounts != nil {
		mux.Handle("/v1/notes", deps.Accounts.Protect(deps.Notes))
		mux.Handle("/v1/notes/", deps.Accounts.Protect(deps.Notes))
		if bmHandler, ok := deps.Notes.(interface {
			ServeBookmarksHTTP(http.ResponseWriter, *http.Request)
		}); ok {
			mux.Handle("/v1/bookmarks", deps.Accounts.Protect(http.HandlerFunc(bmHandler.ServeBookmarksHTTP)))
			mux.Handle("/v1/bookmarks/", deps.Accounts.Protect(http.HandlerFunc(bmHandler.ServeBookmarksHTTP)))
		}
	}
	mux.HandleFunc("GET /v1/features", func(w http.ResponseWriter, _ *http.Request) {
		marketplace := false
		if m, ok := deps.Sharing.(interface{ MarketplaceEnabled() bool }); ok {
			marketplace = m.MarketplaceEnabled()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"lesson_test_tracer_enabled": deps.LessonTestTracerEnabled,
			// E15-07 release flag: the Marketplace stays hidden until moderation, takedown and legal approval.
			"marketplace_enabled": marketplace,
		})
	})
	if deps.LessonTestTracerEnabled && deps.LessonTest != nil && deps.Accounts != nil {
		mux.Handle("/v1/lesson-generations/", deps.Accounts.Protect(deps.LessonTest))
	}
	if deps.Steering != nil && deps.Accounts != nil {
		mux.Handle("POST /v1/courses/{id}/instructions", deps.Accounts.Protect(deps.Steering))
		mux.Handle("GET /v1/courses/{id}/instructions", deps.Accounts.Protect(deps.Steering))
		mux.Handle("GET /v1/courses/{id}/instructions/latest", deps.Accounts.Protect(deps.Steering))
		mux.Handle("PATCH /v1/courses/{id}/instructions/{instructionId}", deps.Accounts.Protect(deps.Steering))
		mux.Handle("DELETE /v1/courses/{id}/instructions/{instructionId}", deps.Accounts.Protect(deps.Steering))
	}
	if deps.Characters != nil {
		mux.Handle("GET /v1/characters/default-cast", deps.Characters)
		if deps.Accounts != nil {
			mux.Handle("/v1/characters", deps.Accounts.Protect(deps.Characters))
			mux.Handle("POST /v1/characters/jobs", deps.Accounts.Protect(deps.Characters))
			mux.Handle("GET /v1/characters/jobs/{jobId}", deps.Accounts.Protect(deps.Characters))
			mux.Handle("DELETE /v1/characters/jobs/{jobId}", deps.Accounts.Protect(deps.Characters))
			mux.Handle("POST /v1/characters/jobs/{jobId}/confirm", deps.Accounts.Protect(deps.Characters))
			mux.Handle("GET /v1/characters/creation", deps.Accounts.Protect(deps.Characters))
			mux.Handle("GET /v1/characters/{id}", deps.Accounts.Protect(deps.Characters))
			mux.Handle("PATCH /v1/characters/{id}", deps.Accounts.Protect(deps.Characters))
			mux.Handle("DELETE /v1/characters/{id}", deps.Accounts.Protect(deps.Characters))
			mux.Handle("GET /v1/courses/{id}/cast", deps.Accounts.Protect(deps.Characters))
			mux.Handle("PUT /v1/courses/{id}/cast", deps.Accounts.Protect(deps.Characters))
		}
		mux.Handle("/v1/characters/", deps.Characters)
		if deps.Accounts != nil {
			mux.Handle("/v1/courses/{courseId}/lessons/", deps.Accounts.Protect(deps.Characters))
		}
	}
	lessonMux := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/manifest") && deps.Characters != nil {
			deps.Characters.ServeHTTP(w, r)
			return
		}
		if deps.LessonTestTracerEnabled && deps.LessonTest != nil && deps.Accounts != nil {
			deps.Accounts.Protect(deps.LessonTest).ServeHTTP(w, r)
			return
		}
		if deps.Characters != nil {
			deps.Characters.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
	mux.Handle("/v1/lessons/", lessonMux)
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
		// Queueing and quote resolution consume the same budget as provider work.
		providerCtx, cancel := context.WithDeadline(r.Context(), job.DeadlineAt)
		defer cancel()
		// API resolves bounds from the pinned quote.
		request.Limits, err = financial.Limits(providerCtx, owner, job.ID, "preview")
		if err != nil {
			writeJSON(w, 409, map[string]string{"error": "quote bounds unavailable"})
			return
		}
		if err = financial.Claim(providerCtx, owner, job.ID); err != nil {
			writeJSON(w, 409, map[string]string{"error": "Generation queue unavailable. Refresh your wallet and retry."})
			return
		}
		claimed = true
		started := time.Now()
		preparer.PreviewEvent(r.Context(), owner, "course_preview_requested", request, job.ID, 0)
		// The engine's internal attempt budget cannot restart the elapsed API budget.
		remaining := time.Until(job.DeadlineAt).Milliseconds()
		if remaining <= 0 || providerCtx.Err() != nil {
			writeJSON(w, http.StatusGatewayTimeout, map[string]string{"error": "Preview deadline exceeded. Your brief is kept."})
			return
		}
		if remaining <= math.MaxUint32 {
			request.Limits.TimeoutMs = min(request.Limits.TimeoutMs, uint32(remaining))
		}
		response, err := deps.Engine.PreviewCourse(providerCtx, request)
		valid := err == nil && providerCtx.Err() == nil && courses.ValidatePreview(response) == nil && len(response.Attempts) > 0 && response.Attempts[len(response.Attempts)-1].GetDelivered()
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
		finished = finishErr == nil && (settled.State == "released" || settled.State == "settled")
		if !valid || finishErr != nil || settled.State != "settled" {
			preparer.PreviewEvent(finishCtx, owner, "course_generation_failed", request, job.ID, time.Since(started).Milliseconds())
		}
		if recorder, ok := deps.Eval.(interface {
			RecordCallMetrics(context.Context, eval.CallMetrics) error
		}); ok {
			var fallbackUsed bool
			var inTokens, outTokens, cacheWrite, cacheRead int64
			for _, a := range attempts {
				if a != nil {
					if a.FallbackUsed {
						fallbackUsed = true
					}
					inTokens += int64(a.InputTokens)
					outTokens += int64(a.OutputTokens)
					cacheWrite += int64(a.CacheWriteTokens)
					cacheRead += int64(a.CacheReadTokens)
				}
			}
			_ = recorder.RecordCallMetrics(finishCtx, eval.CallMetrics{
				Task:               "course_preview",
				Success:            valid && finishErr == nil,
				FallbackUsed:       fallbackUsed,
				LatencyMs:          time.Since(started).Milliseconds(),
				InputTokens:        inTokens,
				OutputTokens:       outTokens,
				CacheWriteTokens:   cacheWrite,
				CacheReadTokens:    cacheRead,
				CostNanos:          settled.CostNanos,
				CreditRevenueCents: settled.ContentSpent + settled.AudioSpent,
			})
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
	// Public routes are explicit; every new route inherits authentication.
	guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Disabled tracer endpoints remain unavailable even to authenticated callers.
		// Respond here, without dispatching a handler through the public allowlist.
		if !deps.LessonTestTracerEnabled {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			disabled := strings.HasPrefix(r.URL.Path, "/v1/lesson-generations/")
			if len(parts) >= 3 && parts[0] == "v1" && parts[1] == "lessons" {
				disabled = disabled || len(parts) == 3 || (len(parts) >= 4 && (parts[3] == "prepare" || parts[3] == "test"))
			}
			if disabled {
				http.NotFound(w, r)
				return
			}
		}
		public := false
		if r.Method == http.MethodGet {
			switch r.URL.Path {
			case "/healthz", "/readyz", "/v1/pricing", "/v1/features", "/v1/courses/shared-demo", "/v1/characters/default-cast":
				public = true
			}
		}
		if r.Method == http.MethodPost {
			switch r.URL.Path {
			case "/v1/auth/code", "/v1/auth/verify", "/v1/auth/refresh", "/v1/auth/logout", "/v1/auth/google/start", "/v1/auth/google/complete", "/v1/auth/apple/start", "/v1/auth/apple/complete", "/v1/payments/stripe/webhook", "/v1/eval/reports":
				public = true
			}
		}
		// Anonymous course link preview: the handler is rate limited, noindex and token-gated.
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/public/shares/") && strings.Count(r.URL.Path, "/") == 4 {
			public = true
		}
		// Marketplace browse and Course page: anonymous-safe, rate limited and flag-gated in the handler.
		if r.Method == http.MethodGet && (r.URL.Path == "/v1/marketplace/courses" || (strings.HasPrefix(r.URL.Path, "/v1/marketplace/courses/") && strings.Count(r.URL.Path, "/") == 4)) {
			public = true
		}
		if public {
			mux.ServeHTTP(w, r)
			return
		}
		if deps.Accounts == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "sign in required"})
			return
		}
		protected := deps.Accounts.Protect(mux)
		// The default guard must preserve the admin boundary's denial audit hook.
		if strings.HasPrefix(r.URL.Path, "/admin/v1/pricing/") {
			if auth, ok := deps.Accounts.(interface {
				ProtectWithDenied(http.Handler, func(*http.Request)) http.Handler
			}); ok {
				if audit, ok := deps.PricingAdmin.(interface{ RecordDenied(*http.Request) }); ok {
					protected = auth.ProtectWithDenied(mux, audit.RecordDenied)
				}
			}
		}
		protected.ServeHTTP(w, r)
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
