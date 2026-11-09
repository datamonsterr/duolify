package eval_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
)

func TestAlertEvaluatorThresholdBoundaries(t *testing.T) {
	config := eval.AlertConfig{
		PlatformDailyBudgetUSD:       50.00,
		DailyBudgetAlert80Percent:    true,
		DailyBudgetAlert100Percent:   true,
		LearnerCostIncreaseThreshold: 0.20,
		CacheHitRateThreshold:        0.60,
		PaidMarginThreshold:          0.70,
		FallbackRateThreshold:        0.15,
		ErrorRateThreshold:           0.05,
	}

	t.Run("80% and 100% platform budget boundaries", func(t *testing.T) {
		// Exactly below 80%: $39.99 of $50
		res := eval.EvaluatePlatformBudget(39.99, config)
		if res.Fire80 || res.Fire100 {
			t.Fatalf("expected no budget alerts at $39.99, got fire80=%v fire100=%v", res.Fire80, res.Fire100)
		}

		// Exactly at 80%: $40.00 of $50
		res = eval.EvaluatePlatformBudget(40.00, config)
		if !res.Fire80 || res.Fire100 {
			t.Fatalf("expected fire80 at $40.00, got fire80=%v fire100=%v", res.Fire80, res.Fire100)
		}

		// Between 80% and 100%: $45.00 of $50
		res = eval.EvaluatePlatformBudget(45.00, config)
		if !res.Fire80 || res.Fire100 {
			t.Fatalf("expected fire80 at $45.00, got fire80=%v fire100=%v", res.Fire80, res.Fire100)
		}

		// Exactly at 100%: $50.00 of $50
		res = eval.EvaluatePlatformBudget(50.00, config)
		if !res.Fire80 || !res.Fire100 {
			t.Fatalf("expected fire80 and fire100 at $50.00, got fire80=%v fire100=%v", res.Fire80, res.Fire100)
		}
	})

	t.Run("seven-day learner cost increase boundary (+20%)", func(t *testing.T) {
		// Prior avg $10.00, current $11.90 (+19%): no alert
		if eval.EvaluateLearnerCostIncrease(10.00, 11.90, config.LearnerCostIncreaseThreshold) {
			t.Fatalf("expected no alert at +19%% increase")
		}

		// Prior avg $10.00, current $12.00 (+20%): alert
		if !eval.EvaluateLearnerCostIncrease(10.00, 12.00, config.LearnerCostIncreaseThreshold) {
			t.Fatalf("expected alert at +20%% increase")
		}

		// Prior avg $10.00, current $12.50 (+25%): alert
		if !eval.EvaluateLearnerCostIncrease(10.00, 12.50, config.LearnerCostIncreaseThreshold) {
			t.Fatalf("expected alert at +25%% increase")
		}
	})

	t.Run("cache hit rate boundary (<60%)", func(t *testing.T) {
		// Exactly 60.0% or 65.0%: no alert
		if eval.EvaluateCacheHitRate(60, 100, config.CacheHitRateThreshold) {
			t.Fatalf("expected no alert at 60%% cache hit rate")
		}
		if eval.EvaluateCacheHitRate(65, 100, config.CacheHitRateThreshold) {
			t.Fatalf("expected no alert at 65%% cache hit rate")
		}

		// 59.9% (<60%): alert
		if !eval.EvaluateCacheHitRate(59, 100, config.CacheHitRateThreshold) {
			t.Fatalf("expected alert at 59%% cache hit rate")
		}
	})

	t.Run("paid AI margin boundary (<70%)", func(t *testing.T) {
		// Revenue 100, Cost 30 -> Margin 70%: no alert
		if eval.EvaluatePaidMargin(100.0, 30.0, config.PaidMarginThreshold) {
			t.Fatalf("expected no alert at 70%% margin")
		}

		// Revenue 100, Cost 25 -> Margin 75%: no alert
		if eval.EvaluatePaidMargin(100.0, 25.0, config.PaidMarginThreshold) {
			t.Fatalf("expected no alert at 75%% margin")
		}

		// Revenue 100, Cost 35 -> Margin 65% (<70%): alert
		if !eval.EvaluatePaidMargin(100.0, 35.0, config.PaidMarginThreshold) {
			t.Fatalf("expected alert at 65%% margin")
		}
	})

	t.Run("fallback and error rate boundaries", func(t *testing.T) {
		// Fallback rate: 14 out of 100 (14%): no alert (threshold 15%)
		if eval.EvaluateRate(14, 100, config.FallbackRateThreshold) {
			t.Fatalf("expected no alert at 14%% fallback rate")
		}
		// Fallback rate: 15 out of 100 (15%): alert
		if !eval.EvaluateRate(15, 100, config.FallbackRateThreshold) {
			t.Fatalf("expected alert at 15%% fallback rate")
		}

		// Error rate: 4 out of 100 (4%): no alert (threshold 5%)
		if eval.EvaluateRate(4, 100, config.ErrorRateThreshold) {
			t.Fatalf("expected no alert at 4%% error rate")
		}
		// Error rate: 5 out of 100 (5%): alert
		if !eval.EvaluateRate(5, 100, config.ErrorRateThreshold) {
			t.Fatalf("expected alert at 5%% error rate")
		}
	})
}

func TestDeduplicationWindowAndKeyGeneration(t *testing.T) {
	window := time.Date(2026, 11, 10, 14, 0, 0, 0, time.UTC)
	key1 := eval.DedupeKey("platform_daily_80", window, 60)
	key2 := eval.DedupeKey("platform_daily_80", window.Add(15*time.Minute), 60)
	key3 := eval.DedupeKey("platform_daily_80", window.Add(65*time.Minute), 60)

	if key1 != key2 {
		t.Fatalf("expected identical dedupe keys within the same 60-min window, got %q vs %q", key1, key2)
	}
	if key1 == key3 {
		t.Fatalf("expected different dedupe keys across window boundary, got %q == %q", key1, key3)
	}
}

func TestDashboardSanitizationNoPromptsOrLearnerText(t *testing.T) {
	dash := eval.Dashboards{
		Reliability: eval.ReliabilityDashboard{
			TotalCalls:    100,
			FallbackCalls: 10,
			ErrorCalls:    2,
			FallbackRate:  0.10,
			ErrorRate:     0.02,
			TaskQuality: map[string]eval.TaskQualityMetric{
				"course_preview": {
					Task:        "course_preview",
					Score:       0.95,
					Threshold:   0.85,
					Passed:      true,
					SampleCount: 50,
				},
			},
			LatencyMs: eval.LatencySummary{
				Mean: 450,
				P50:  400,
				P95:  900,
				Max:  1200,
			},
		},
		Cost: eval.CostDashboard{
			TotalInputTokens:      150000,
			TotalOutputTokens:     50000,
			CacheWriteTokens:      10000,
			CacheReadTokens:       80000,
			CacheHitRate:          0.57,
			TotalCostUSD:          12.50,
			PlatformBudgetUSD:     50.00,
			PlatformBudgetPercent: 25.0,
			LearnerCostGrowth:     0.05,
			PaidMargin:            0.78,
			ActiveLearners:        42,
		},
	}

	bytes, err := json.Marshal(dash)
	if err != nil {
		t.Fatal(err)
	}
	raw := strings.ToLower(string(bytes))

	for _, forbidden := range []string{"prompt", "learner_id", "email", "completion", "topic", "lesson_text", "slide_html", "user_input"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("dashboard JSON contains forbidden sensitive attribute %q: %s", forbidden, raw)
		}
	}
}

type fakeEvents struct {
	captured []map[string]any
}

var _ analytics.Events = (*fakeEvents)(nil)

func (f *fakeEvents) Capture(learnerID, event string, properties map[string]any) error {
	props := make(map[string]any, len(properties))
	for k, v := range properties {
		props[k] = v
	}
	props["distinct_id"] = learnerID
	props["event"] = event
	f.captured = append(f.captured, props)
	return nil
}

func (f *fakeEvents) Close() error { return nil }

func TestEmitCostBudgetAlertPostHogDelivery(t *testing.T) {
	events := &fakeEvents{}

	err := eval.EmitBudgetAlert(events, "platform_daily_80", 40.50, 50.00)
	if err != nil {
		t.Fatalf("emit budget alert failed: %v", err)
	}

	if len(events.captured) != 1 {
		t.Fatalf("expected 1 captured event, got %d", len(events.captured))
	}
	ev := events.captured[0]
	if ev["event"] != "ai_cost_budget_alert" {
		t.Fatalf("expected event ai_cost_budget_alert, got %v", ev["event"])
	}
	if ev["distinct_id"] != eval.SystemOperationalUUID {
		t.Fatalf("expected distinct_id %s, got %v", eval.SystemOperationalUUID, ev["distinct_id"])
	}
	if ev["scope"] != "platform_daily_80" {
		t.Fatalf("expected scope platform_daily_80, got %v", ev["scope"])
	}
	if ev["actual_usd"] != 40.50 {
		t.Fatalf("expected actual_usd 40.50, got %v", ev["actual_usd"])
	}
	if ev["budget_usd"] != 50.00 {
		t.Fatalf("expected budget_usd 50.00, got %v", ev["budget_usd"])
	}
}
