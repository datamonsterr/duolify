package main

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/narration"
)

func TestNarrationBudgetEnvironment(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if got := narrationBudget(env(map[string]string{"NARRATION_MONTHLY_CHAR_LIMIT": "100"})); got.Enabled() {
		t.Fatalf("mock speech spends no provider characters, so nothing is enforced: %+v", got)
	}
	live := map[string]string{"NARRATION_LIVE_ENABLED": "true"}
	if got := narrationBudget(env(live)); got != (narration.Budget{Limit: 40000, StopPercent: 95, CycleDay: 1}) {
		t.Fatalf("defaults: %+v", got)
	}
	live["NARRATION_MONTHLY_CHAR_LIMIT"], live["NARRATION_BUDGET_STOP_PERCENT"], live["NARRATION_CYCLE_START_DAY"] = "100000", "90", "17"
	if got := narrationBudget(env(live)); got != (narration.Budget{Limit: 100000, StopPercent: 90, CycleDay: 17}) {
		t.Fatalf("configured: %+v", got)
	}
	live["NARRATION_MONTHLY_CHAR_LIMIT"], live["NARRATION_BUDGET_STOP_PERCENT"], live["NARRATION_CYCLE_START_DAY"] = "0", "150", "40"
	if got := narrationBudget(env(live)); got != (narration.Budget{Limit: 40000, StopPercent: 95, CycleDay: 1}) {
		t.Fatalf("invalid values fall back to defaults: %+v", got)
	}
}
