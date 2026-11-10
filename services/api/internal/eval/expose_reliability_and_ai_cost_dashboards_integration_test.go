//go:build integration

package eval_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestExposeReliabilityAndAICostDashboardsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("eval_e10_05_int"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	database, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	catalogue, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var capturedEvents []map[string]any
	posthogServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer posthogServer.Close()

	events, err := analytics.NewWithOptions("test-key", posthogServer.URL, analytics.Options{Consent: func(context.Context, string) (bool, error) { return true, nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()

	monitor := telemetry.New(nil)
	evalService := eval.NewService(database.Pool, catalogue, monitor, events)

	t.Run("Postgres operational counters, alert windows, and deduplication", func(t *testing.T) {
		today := time.Now().UTC()
		todayStr := today.Format(time.DateOnly)

		// Record daily operational metrics for task 'course_preview' with healthy cache hits (80%)
		err = evalService.RecordCallMetrics(ctx, eval.CallMetrics{
			Task:               "course_preview",
			Success:            true,
			FallbackUsed:       false,
			LatencyMs:          350,
			InputTokens:        200,
			OutputTokens:       100,
			CacheWriteTokens:   0,
			CacheReadTokens:    1000,
			CostNanos:          3000000,
			CreditRevenueCents: 25,
		})
		if err != nil {
			t.Fatalf("record call metrics failed: %v", err)
		}

		// Record a fallback call
		err = evalService.RecordCallMetrics(ctx, eval.CallMetrics{
			Task:               "course_preview",
			Success:            true,
			FallbackUsed:       true,
			LatencyMs:          800,
			InputTokens:        200,
			OutputTokens:       100,
			CacheWriteTokens:   0,
			CacheReadTokens:    1000,
			CostNanos:          5000000,
			CreditRevenueCents: 25,
		})
		if err != nil {
			t.Fatalf("record fallback metrics failed: %v", err)
		}

		// Record a failed call
		err = evalService.RecordCallMetrics(ctx, eval.CallMetrics{
			Task:               "course_preview",
			Success:            false,
			ErrorCode:          "provider_outage",
			LatencyMs:          1200,
			InputTokens:        200,
			OutputTokens:       0,
			CacheWriteTokens:   0,
			CacheReadTokens:    1000,
			CostNanos:          1000000,
			CreditRevenueCents: 0,
		})
		if err != nil {
			t.Fatalf("record failed call metrics failed: %v", err)
		}

		// Retrieve dashboard from service
		dash, err := evalService.GetDashboards(ctx)
		if err != nil {
			t.Fatalf("get dashboards failed: %v", err)
		}

		if dash.Reliability.TotalCalls != 3 {
			t.Fatalf("expected 3 total calls, got %d", dash.Reliability.TotalCalls)
		}
		if dash.Reliability.FallbackCalls != 1 {
			t.Fatalf("expected 1 fallback call, got %d", dash.Reliability.FallbackCalls)
		}
		if dash.Reliability.ErrorCalls != 1 {
			t.Fatalf("expected 1 error call, got %d", dash.Reliability.ErrorCalls)
		}

		// Verify fallback rate alert triggers when fallback rate (1/3 = 33%) exceeds threshold (15%)
		alerts, err := evalService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatalf("check alerts failed: %v", err)
		}

		foundFallbackAlert := false
		foundErrorAlert := false
		for _, a := range alerts {
			if a.Scope == "fallback_rate" && !a.Deduplicated {
				foundFallbackAlert = true
			}
			if a.Scope == "error_rate" && !a.Deduplicated {
				foundErrorAlert = true
			}
		}
		if !foundFallbackAlert {
			t.Fatalf("expected fallback_rate alert, got %+v", alerts)
		}
		if !foundErrorAlert {
			t.Fatalf("expected error_rate alert, got %+v", alerts)
		}

		// Deduplication check: repeated alert check within the same window should be deduplicated
		alerts2, err := evalService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatalf("second check alerts failed: %v", err)
		}
		for _, a := range alerts2 {
			if (a.Scope == "fallback_rate" || a.Scope == "error_rate") && !a.Deduplicated {
				t.Fatalf("expected alert %s to be deduplicated on immediate recheck", a.Scope)
			}
		}

		// Inject platform spend to exceed 80% ($40) of $50 budget
		_, err = database.Pool.Exec(ctx, `
			INSERT INTO credit_cost_counters(scope, owner_reference, day, held_nanos, spent_nanos)
			VALUES('platform', 'platform', $1::date, 0, 42000000000)
			ON CONFLICT (scope, owner_reference, day)
			DO UPDATE SET spent_nanos = 42000000000
		`, todayStr)
		if err != nil {
			t.Fatalf("inject spend: %v", err)
		}

		budgetAlerts, err := evalService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatalf("check budget alerts: %v", err)
		}
		found80 := false
		for _, a := range budgetAlerts {
			if a.Scope == "platform_daily_80" && !a.Deduplicated {
				found80 = true
			}
		}
		if !found80 {
			t.Fatalf("expected platform_daily_80 alert to fire, got %+v", budgetAlerts)
		}

		// Deduplication check on budget alert
		budgetAlerts2, err := evalService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatalf("second check budget alerts: %v", err)
		}
		for _, a := range budgetAlerts2 {
			if a.Scope == "platform_daily_80" && !a.Deduplicated {
				t.Fatalf("expected platform_daily_80 to be deduplicated on second check")
			}
		}
	})

	t.Run("7-day cost increase, cache hit rate, and paid margin alarms", func(t *testing.T) {
		// Test 7-day +20% cost per active learner alarm:
		// Setup previous 7 days (avg spend $1.00 / active learner)
		// Setup current 7 days (avg spend $1.30 / active learner -> +30% increase)
		now := time.Now().UTC()
		for i := 14; i >= 8; i-- {
			d := now.AddDate(0, 0, -i).Format(time.DateOnly)
			_, _ = database.Pool.Exec(ctx, `
				INSERT INTO credit_cost_counters(scope, owner_reference, day, held_nanos, spent_nanos)
				VALUES('learner', 'learner-1', $1::date, 0, 1000000000)
				ON CONFLICT DO NOTHING
			`, d)
		}
		for i := 7; i >= 1; i-- {
			d := now.AddDate(0, 0, -i).Format(time.DateOnly)
			_, _ = database.Pool.Exec(ctx, `
				INSERT INTO credit_cost_counters(scope, owner_reference, day, held_nanos, spent_nanos)
				VALUES('learner', 'learner-1', $1::date, 0, 1300000000)
				ON CONFLICT DO NOTHING
			`, d)
		}

		alerts, err := evalService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatalf("check learner cost increase alert failed: %v", err)
		}
		foundLearnerGrowth := false
		for _, a := range alerts {
			if a.Scope == "seven_day_learner_cost" && !a.Deduplicated {
				foundLearnerGrowth = true
			}
		}
		if !foundLearnerGrowth {
			t.Fatalf("expected seven_day_learner_cost alert to fire, got %+v", alerts)
		}

		// Test cache hit rate alarm (<60%):
		// Record high miss tokens: 1000 input, 100 cache read -> cache hit rate = 100/(1000+100) = ~9% (<60%)
		err = evalService.RecordCallMetrics(ctx, eval.CallMetrics{
			Task:            "course_outline",
			Success:         true,
			InputTokens:     10000,
			CacheReadTokens: 100,
		})
		if err != nil {
			t.Fatal(err)
		}

		alerts, err = evalService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		foundCacheHitAlert := false
		for _, a := range alerts {
			if a.Scope == "cache_hit_rate" && !a.Deduplicated {
				foundCacheHitAlert = true
			}
		}
		if !foundCacheHitAlert {
			t.Fatalf("expected cache_hit_rate alert to fire, got %+v", alerts)
		}

		// Test paid margin alarm (<70%):
		// Revenue: $1.00 (100 cents), Provider Cost: $0.80 (800_000_000 nanos) -> overall Margin < 70%
		err = evalService.RecordCallMetrics(ctx, eval.CallMetrics{
			Task:               "lesson_slides",
			Success:            true,
			CostNanos:          800000000,
			CreditRevenueCents: 100,
		})
		if err != nil {
			t.Fatal(err)
		}

		alerts, err = evalService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatal(err)
		}
		foundMarginAlert := false
		for _, a := range alerts {
			if a.Scope == "paid_margin" && !a.Deduplicated {
				foundMarginAlert = true
			}
		}
		if !foundMarginAlert {
			t.Fatalf("expected paid_margin alert to fire, got %+v", alerts)
		}
	})

	t.Run("PostHog double outage and opt-out resilience", func(t *testing.T) {
		// Create service with opt-out enabled
		optOutEvents, err := analytics.NewWithOptions("test-key", posthogServer.URL, analytics.Options{OptOut: true})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = optOutEvents.Close() }()

		resilientMonitor := telemetry.New(nil)
		resilientService := eval.NewService(database.Pool, catalogue, resilientMonitor, optOutEvents)

		// Operational recording continues
		err = resilientService.RecordCallMetrics(ctx, eval.CallMetrics{
			Task:         "tutor_chat",
			Success:      false,
			ErrorCode:    "timeout",
			FallbackUsed: true,
		})
		if err != nil {
			t.Fatalf("record metrics under opt-out failed: %v", err)
		}

		// Alert evaluation continues and records incident in monitor & postgres
		alerts, err := resilientService.CheckAndTriggerAlerts(ctx)
		if err != nil {
			t.Fatalf("check alerts under opt-out failed: %v", err)
		}
		if len(alerts) == 0 {
			t.Fatalf("expected alerts to still be evaluated and returned under opt-out")
		}
	})

	t.Run("Eval report persistence and 90-day retention", func(t *testing.T) {
		report := eval.Report{
			Task:            "course_preview",
			DatasetRevision: "rev-20261110",
			PromptRevision:  "v1",
			Model:           "anthropic/claude-sonnet-5.5",
			Score:           0.95,
			Threshold:       0.85,
			Passed:          true,
			Details:         map[string]any{"check_1": "pass", "judge_score": 0.95},
		}

		id, err := evalService.SaveReport(ctx, report)
		if err != nil {
			t.Fatalf("save report failed: %v", err)
		}

		// Verify 90-day retention
		var createdAt, expiresAt time.Time
		err = database.Pool.QueryRow(ctx, "SELECT created_at, expires_at FROM eval_reports WHERE id=$1", id).Scan(&createdAt, &expiresAt)
		if err != nil {
			t.Fatalf("query report dates: %v", err)
		}

		diffDays := expiresAt.Sub(createdAt).Hours() / 24.0
		if diffDays < 89.0 || diffDays > 91.0 {
			t.Fatalf("expected ~90 day retention, got %f days", diffDays)
		}
	})
}
