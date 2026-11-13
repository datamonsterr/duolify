package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type evalProviderDouble struct {
	enginev1.UnimplementedEngineServiceServer
	mu           sync.Mutex
	mode         string
	costNanos    int64
	failPrimary  bool
	failFallback bool
	cacheHits    uint32
	cacheWrites  uint32
	inputTokens  uint32
	outputTokens uint32
	lastAttempts []*enginev1.Usage
}

func (p *evalProviderDouble) Health(context.Context, *enginev1.HealthRequest) (*enginev1.HealthResponse, error) {
	return &enginev1.HealthResponse{Version: "e10-05-test", Providers: []string{"anthropic", "openrouter"}}, nil
}

func (p *evalProviderDouble) PreviewCourse(_ context.Context, req *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	topic := req.GetTopic()
	if strings.Contains(topic, "fail-all") || p.failFallback {
		p.lastAttempts = []*enginev1.Usage{
			{
				Provider:         req.Limits.Provider,
				Model:            req.Limits.Model,
				Attempt:          1,
				FallbackUsed:     false,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "provider_outage",
			},
			{
				Provider:         req.Limits.FallbackProvider,
				Model:            req.Limits.FallbackModel,
				Attempt:          2,
				FallbackUsed:     true,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "provider_outage",
			},
		}
		return nil, status.Error(codes.Unavailable, "all providers unavailable")
	}

	if strings.Contains(topic, "fallback-used") || p.failPrimary {
		p.lastAttempts = []*enginev1.Usage{
			{
				Provider:         req.Limits.Provider,
				Model:            req.Limits.Model,
				Attempt:          1,
				FallbackUsed:     false,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "rate_limited",
			},
			{
				Provider:         req.Limits.FallbackProvider,
				Model:            req.Limits.FallbackModel,
				InputTokens:      100,
				OutputTokens:     50,
				CacheReadTokens:  p.cacheHits,
				CacheWriteTokens: p.cacheWrites,
				Attempt:          2,
				FallbackUsed:     true,
				Component:        "content",
				Delivered:        true,
				UsageUnavailable: false,
			},
		}
		return &enginev1.PreviewCourseResponse{
			Attempts: p.lastAttempts,
			Usage:    p.lastAttempts[1],
			SampleOutline: &enginev1.CourseOutline{
				Title: topic,
				Units: []*enginev1.Unit{{Title: "Unit 1", Lessons: []*enginev1.LessonRef{{Title: "Lesson 1", Goal: "Goal"}}}},
			},
			ExampleSlides: []*enginev1.Slide{{SlideId: "s1", Html: `<div id="root">Fallback content</div>`, Narration: "Fallback narration", DurationMs: 1000}},
		}, nil
	}

	// Normal or high-cost call
	inputTok := p.inputTokens
	if inputTok == 0 {
		inputTok = 200
	}
	outTok := p.outputTokens
	if outTok == 0 {
		outTok = 100
	}

	p.lastAttempts = []*enginev1.Usage{
		{
			Provider:         req.Limits.Provider,
			Model:            req.Limits.Model,
			InputTokens:      inputTok,
			OutputTokens:     outTok,
			CacheReadTokens:  p.cacheHits,
			CacheWriteTokens: p.cacheWrites,
			Attempt:          1,
			FallbackUsed:     false,
			Component:        "content",
			Delivered:        true,
			UsageUnavailable: false,
		},
	}

	return &enginev1.PreviewCourseResponse{
		Attempts: p.lastAttempts,
		Usage:    p.lastAttempts[0],
		SampleOutline: &enginev1.CourseOutline{
			Title: topic,
			Units: []*enginev1.Unit{{Title: "Unit 1", Lessons: []*enginev1.LessonRef{{Title: "Lesson 1", Goal: "Goal"}}}},
		},
		ExampleSlides: []*enginev1.Slide{{SlideId: "s1", Html: `<div id="root">Normal content</div>`, Narration: "Normal narration", DurationMs: 1000}},
	}, nil
}

func TestReliabilityAndCostDashboardsE2E(t *testing.T) {
	grpcListener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	provider := &evalProviderDouble{}
	backend := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(backend, provider)
	go func() { _ = backend.Serve(grpcListener) }()
	t.Cleanup(backend.Stop)

	client, err := engine.New("http://"+grpcListener.Addr().String(), 500*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	auth, access, database := signedInDatabase(t)
	claims, err := auth.ParseAccess(access)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	if _, err = database.Pool.Exec(t.Context(), `INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)`, owner); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var capturedEvents []map[string]any
	posthog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if decErr := json.NewDecoder(r.Body).Decode(&batch); decErr == nil {
			mu.Lock()
			capturedEvents = append(capturedEvents, batch.Batch...)
			mu.Unlock()
		}
		w.WriteHeader(200)
	}))
	defer posthog.Close()

	events, err := analytics.NewWithOptions("test-posthog-key", posthog.URL, analytics.Options{Consent: func(context.Context, string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()

	catalogue, err := pricing.Load(t.Context(), database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	creditService := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, events)
	quotes := pricing.NewQuotes(database.Pool)

	// Fund initial credits
	if _, err = database.Pool.Exec(t.Context(), "INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,100000,'purchase','e10-05-init',$2)", owner, catalogue.Config().Version); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	monitor := telemetry.New(logger)

	evalService := eval.NewService(database.Pool, catalogue, monitor, events)

	handler := httpapi.New(httpapi.Dependencies{
		Accounts:    auth,
		CourseBrief: courses.New(courses.Options{Pool: database.Pool}),
		Engine:      client,
		Credits:     creditService,
		Quotes:      quotes,
		Monitor:     monitor,
		Eval:        evalService,
		Postgres:    func(context.Context) error { return nil },
		Redis:       func(context.Context) error { return errors.New("unavailable") },
	}, logger)

	server := httpapi.NewServer("127.0.0.1:0", handler)
	listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	serverCtx, stop := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- httpapi.Serve(serverCtx, server, listener) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Log(err)
		}
	})

	httpClient := &http.Client{Timeout: 3 * time.Second}

	callPreview := func(topic string) (int, string) {
		brief := courses.Brief{Topic: topic}
		raw, _ := json.Marshal(brief)
		var revision string
		if e := database.Pool.QueryRow(t.Context(), `INSERT INTO course_brief_revisions(learner_id,brief,content_hash) VALUES($1,$2,$3) RETURNING id`, owner, raw, brief.Hash()).Scan(&revision); e != nil {
			t.Fatalf("insert brief: %v", e)
		}
		q, quoteErr := quotes.Create(t.Context(), owner, pricing.QuoteInput{Action: "preview", BriefRevision: revision})
		if quoteErr != nil {
			t.Fatalf("create quote: %v", quoteErr)
		}
		if acceptErr := quotes.Accept(t.Context(), owner, q.ID); acceptErr != nil {
			t.Fatalf("accept quote: %v", acceptErr)
		}
		reqBody := fmt.Sprintf(`{"brief_revision":%q}`, revision)
		httpReq, httpErr := http.NewRequestWithContext(t.Context(), "POST", "http://"+listener.Addr().String()+"/v1/courses/preview", strings.NewReader(reqBody))
		if httpErr != nil {
			t.Fatalf("new request: %v", httpErr)
		}
		httpReq.Header.Set("Authorization", "Bearer "+access)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Quote-ID", q.ID)
		resp, doErr := httpClient.Do(httpReq)
		if doErr != nil {
			t.Fatalf("do request: %v", doErr)
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(body)
	}

	t.Run("inject failing and costly calls and observe deduplicated alerts", func(t *testing.T) {
		// 1. Inject failing call (primary failure -> fallback used)
		code, _ := callPreview("fallback-used topic")
		if code != http.StatusOK {
			t.Fatalf("expected 200 on fallback, got %d", code)
		}

		// 2. Inject terminal failure (all fail)
		code, _ = callPreview("fail-all topic")
		if code != http.StatusBadGateway {
			t.Fatalf("expected 502 on all fail, got %d", code)
		}

		// 3. Inject costly calls to reach 80% ($40) of $50 budget
		// Simulate platform spend accumulation: $40.50 (40_500_000_000 nanos)
		today := time.Now().UTC().Format(time.DateOnly)
		_, err = database.Pool.Exec(t.Context(), `
			INSERT INTO credit_cost_counters(scope, owner_reference, day, held_nanos, spent_nanos)
			VALUES('platform', 'platform', $1::date, 0, 41000000000)
			ON CONFLICT (scope, owner_reference, day)
			DO UPDATE SET spent_nanos = 41000000000
		`, today)
		if err != nil {
			t.Fatalf("failed to inject platform spend: %v", err)
		}

		// Trigger alert check
		alertResults, err := evalService.CheckAndTriggerAlerts(t.Context())
		if err != nil {
			t.Fatalf("check alerts failed: %v", err)
		}
		if len(alertResults) == 0 {
			t.Fatalf("expected at least one alert to fire for 80%% budget, got 0")
		}

		// Verify deduplication: immediately checking alerts again should NOT emit duplicate notifications
		mu.Lock()
		initialCount := len(capturedEvents)
		mu.Unlock()

		alertResults2, err := evalService.CheckAndTriggerAlerts(t.Context())
		if err != nil {
			t.Fatalf("second check alerts failed: %v", err)
		}
		// Second check should deduplicate firing alerts
		for _, a := range alertResults2 {
			if a.Deduplicated == false && a.Scope == "platform_daily_80" {
				t.Fatalf("expected platform_daily_80 to be deduplicated on second check")
			}
		}

		mu.Lock()
		afterCount := len(capturedEvents)
		mu.Unlock()
		if afterCount != initialCount {
			t.Fatalf("expected deduplication to suppress duplicate PostHog events, got initial=%d after=%d", initialCount, afterCount)
		}

		// Now inject spend to 100% ($50 budget: e.g. $51.00 = 51_000_000_000 nanos)
		_, err = database.Pool.Exec(t.Context(), `
			UPDATE credit_cost_counters SET spent_nanos = 51000000000
			WHERE scope = 'platform' AND owner_reference = 'platform' AND day = $1::date
		`, today)
		if err != nil {
			t.Fatalf("failed to update platform spend to 100%%: %v", err)
		}

		alertResults3, err := evalService.CheckAndTriggerAlerts(t.Context())
		if err != nil {
			t.Fatalf("third check alerts failed: %v", err)
		}
		found100 := false
		for _, a := range alertResults3 {
			if a.Scope == "platform_daily_100" && !a.Deduplicated {
				found100 = true
			}
		}
		if !found100 {
			t.Fatalf("expected platform_daily_100 alert to fire without deduplication, got %+v", alertResults3)
		}

		// Flush analytics client so enqueued events are delivered to the test server
		_ = events.Close()

		// Verify PostHog double received ai_cost_budget_alert with no prompt or learner text
		mu.Lock()
		var budgetAlertEvents []map[string]any
		for _, e := range capturedEvents {
			if e["event"] == "ai_cost_budget_alert" {
				budgetAlertEvents = append(budgetAlertEvents, e)
			}
		}
		mu.Unlock()

		if len(budgetAlertEvents) == 0 {
			t.Fatalf("expected ai_cost_budget_alert events in PostHog, found none (total captured=%d)", len(capturedEvents))
		}
		for _, e := range budgetAlertEvents {
			// DistinctID must be the reserved system UUID
			if e["distinct_id"] != "00000000-0000-4000-8000-000000000011" {
				t.Fatalf("expected system operational UUID, got %v", e["distinct_id"])
			}
			raw, _ := json.Marshal(e)
			for _, forbidden := range []string{"prompt", "learner", "fallback-used topic", "fail-all topic"} {
				if strings.Contains(strings.ToLower(string(raw)), forbidden) {
					t.Fatalf("found forbidden text %q in alert event: %s", forbidden, string(raw))
				}
			}
		}

		// 4. Query HTTP dashboard endpoints and check dimensions
		dashReq, _ := http.NewRequestWithContext(t.Context(), "GET", "http://"+listener.Addr().String()+"/v1/eval/dashboards", nil)
		dashReq.Header.Set("Authorization", "Bearer "+access)
		dashResp, err := httpClient.Do(dashReq)
		if err != nil {
			t.Fatalf("get dashboards: %v", err)
		}
		defer func() { _ = dashResp.Body.Close() }()
		if dashResp.StatusCode != http.StatusOK {
			t.Fatalf("dashboards status: %d", dashResp.StatusCode)
		}

		dashBody, _ := io.ReadAll(dashResp.Body)
		var dashData struct {
			Reliability struct {
				TotalCalls    int64   `json:"total_calls"`
				FallbackCalls int64   `json:"fallback_calls"`
				ErrorCalls    int64   `json:"error_calls"`
				FallbackRate  float64 `json:"fallback_rate"`
				ErrorRate     float64 `json:"error_rate"`
			} `json:"reliability"`
			Cost struct {
				TotalCostUSD      float64 `json:"total_cost_usd"`
				PlatformBudgetUSD float64 `json:"platform_budget_usd"`
				CacheHitRate      float64 `json:"cache_hit_rate"`
				PaidMargin        float64 `json:"paid_margin"`
			} `json:"cost"`
		}
		if err := json.Unmarshal(dashBody, &dashData); err != nil {
			t.Fatalf("unmarshal dashboards: %v, raw: %s", err, string(dashBody))
		}

		// Check dimensions are published without prompts
		if dashData.Reliability.TotalCalls == 0 {
			t.Fatalf("expected positive total calls in dashboard, got 0")
		}
		if dashData.Cost.PlatformBudgetUSD != 50.00 {
			t.Fatalf("expected platform budget USD 50.00, got %f", dashData.Cost.PlatformBudgetUSD)
		}
		for _, forbidden := range []string{"prompt", owner, "fallback-used", "fail-all", "unit 1", "normal content"} {
			if strings.Contains(strings.ToLower(string(dashBody)), strings.ToLower(forbidden)) {
				t.Fatalf("forbidden text %q in dashboard output: %s", forbidden, string(dashBody))
			}
		}
	})
}
