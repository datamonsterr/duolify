package pricing

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func sampleValidConfig() map[string]any {
	cfg := map[string]any{
		"version":        "usd-test-1",
		"effective_from": time.Now().UTC().Format(time.RFC3339Nano),
		"currency":       "USD",
		"actions": map[string]any{
			"preview":             map[string]any{"credits": 25},
			"outline":             map[string]any{"credits": 55},
			"lesson_content":      map[string]any{"credits": 140},
			"narration":           map[string]any{"credits": 50},
			"tutor_short":         map[string]any{"credits": 5},
			"tutor_long":          map[string]any{"credits": 8},
			"quick_note":          map[string]any{"credits": 2},
			"cached_replay":       map[string]any{"credits": 0},
			"setup_chat":          map[string]any{"credits": 0},
			"course_brief_chat":   map[string]any{"credits": 0},
			"outline_chat_change": map[string]any{"credits": 5},
		},
		"narration": map[string]any{
			"credits":    50,
			"characters": 1440,
			"rounding":   "ceil_once_per_request",
		},
		"daily_free": map[string]any{
			"credits":          46,
			"eligible_actions": []string{"quick_note", "tutor_short", "tutor_long", "outline_chat_change"},
			"expires":          "learner_local_day_end",
			"spend_first":      true,
		},
		"packs": []map[string]any{
			{"sku": "pack_try", "name": "Try", "currency": "USD", "amount_minor": 500, "credits": 400},
			{"sku": "pack_build", "name": "Build", "currency": "USD", "amount_minor": 1500, "credits": 1350},
			{"sku": "pack_deep", "name": "Deep", "currency": "USD", "amount_minor": 4000, "credits": 4000},
		},
		"plans": []map[string]any{
			{
				"id":   "learn",
				"name": "Learn",
				"monthly": map[string]any{
					"sku": "learn_monthly", "currency": "USD", "amount_minor": 999,
				},
				"yearly": map[string]any{
					"sku": "learn_yearly", "currency": "USD", "amount_minor": 9600,
				},
				"monthly_credits":       600,
				"concurrent_generation": 1,
				"benefits":              []string{"Offline course library downloads"},
			},
		},
		"paid_credits": map[string]any{
			"expire":         false,
			"rollover_cap":   nil,
			"grant_interval": "service_month",
			"annual_grant":   "monthly_anniversary_month_end_clamp",
		},
		"limits": map[string]any{
			"protected_cost_ceiling_usd_per_credit": 0.0015,
			"free_learner_usd_per_local_day":        0.08,
			"paid_learner_usd_per_local_day":        5.0,
			"generation_usd_per_request":            0.5,
			"platform_usd_per_day":                  50.0,
			"preview":                               map[string]any{"input_tokens": 2000, "output_tokens": 2500, "slides": 2},
			"outline":                               map[string]any{"input_tokens": 4000, "output_tokens": 6000},
			"lesson_content":                        map[string]any{"input_tokens": 8000, "output_tokens": 14000, "slides": 8, "test_questions": 5, "retry_variants_per_question": 2, "narration_characters": 1440},
			"tutor":                                 map[string]any{"cached_context_tokens": 12000, "prompt_tokens": 48000, "short_input_tokens": 500, "short_output_tokens": 250, "long_input_tokens": 1000, "long_output_tokens": 650, "concurrent_free_streams": 1},
			"quick_note":                            map[string]any{"input_tokens": 1200, "output_tokens": 300},
			"setup_chat":                            map[string]any{"input_tokens": 2000, "output_tokens": 1000},
			"course_brief_chat":                     map[string]any{"input_tokens": 2000, "output_tokens": 1000, "attempt_timeout_ms": 3000, "max_attempts": 3, "max_cost_nanos": 2100000, "input_nanos_per_token": 100, "output_nanos_per_token": 500, "requests_per_hour": 30, "platform_requests_per_day": 10000},
			"outline_chat_change":                   map[string]any{"input_tokens": 12000, "output_tokens": 2000, "timeout_ms": 10000, "max_attempts": 2, "max_cost_nanos": 5000000},
			"generation_deadline_seconds":           180,
			"tutor_note_deadline_seconds":           30,
			"tts_deadline_seconds":                  60,
			"max_repairs_or_fallbacks":              1,
			"timezone_change_delay_hours":           24,
			"adverse_notice_hours":                  72,
			"minimum_paid_ai_margin_percent":        70,
			"budget_alert_percent":                  []int{80, 100},
			"cost_review_increase_percent":          20,
			"cost_review_days":                      7,
			"cache_hit_alert_below_percent":         60,
		},
		"development_mappings": map[string]string{
			"pack_try":      "synthetic:pack_try",
			"pack_build":    "synthetic:pack_build",
			"pack_deep":     "synthetic:pack_deep",
			"learn_monthly": "synthetic:learn_monthly",
			"learn_yearly":  "synthetic:learn_yearly",
		},
		"design_chat_decision": "interim_adr_0012",
	}
	return cfg
}

func TestParseNewKeysAndEligibility(t *testing.T) {
	cfg := sampleValidConfig()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}

	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse valid interim config failed: %v", err)
	}

	if parsed.Actions["outline_chat_change"].Credits != 5 {
		t.Fatalf("outline_chat_change credits = %d, want 5", parsed.Actions["outline_chat_change"].Credits)
	}
	if parsed.Actions["setup_chat"].Credits != 0 {
		t.Fatalf("setup_chat credits = %d, want 0", parsed.Actions["setup_chat"].Credits)
	}
	if parsed.Actions["course_brief_chat"].Credits != 0 {
		t.Fatalf("course_brief_chat credits = %d, want 0", parsed.Actions["course_brief_chat"].Credits)
	}
	if parsed.DesignChatDecision != "interim_adr_0012" {
		t.Fatalf("design_chat_decision = %q, want interim_adr_0012", parsed.DesignChatDecision)
	}

	// Test invalid decision enum
	cfg["design_chat_decision"] = "invalid_decision"
	rawInvalid, _ := json.Marshal(cfg)
	if _, err := Parse(rawInvalid); err == nil {
		t.Fatal("expected error for invalid design_chat_decision enum")
	}

	// Test unknown action in daily_free
	cfg["design_chat_decision"] = "interim_adr_0012"
	cfg["daily_free"].(map[string]any)["eligible_actions"] = []string{"non_existent_action"}
	rawUnknown, _ := json.Marshal(cfg)
	if _, err := Parse(rawUnknown); err == nil {
		t.Fatal("expected error for unknown action in daily_free.eligible_actions")
	}
}

func TestInterimFixtureValid(t *testing.T) {
	fixtureData, err := os.ReadFile("fixtures/usd_interim_design_chat.json")
	if err != nil {
		t.Fatalf("read fixture file: %v", err)
	}
	parsed, err := Parse(fixtureData)
	if err != nil {
		t.Fatalf("parse fixture file: %v", err)
	}
	if parsed.Version != "usd-interim-design-chat" {
		t.Fatalf("version = %s, want usd-interim-design-chat", parsed.Version)
	}
	if parsed.DesignChatDecision != "interim_adr_0012" {
		t.Fatalf("decision = %s, want interim_adr_0012", parsed.DesignChatDecision)
	}
	if parsed.Actions["setup_chat"].Credits != 0 {
		t.Fatalf("setup_chat = %d, want 0", parsed.Actions["setup_chat"].Credits)
	}
	if parsed.Actions["course_brief_chat"].Credits != 0 {
		t.Fatalf("course_brief_chat = %d, want 0", parsed.Actions["course_brief_chat"].Credits)
	}
	if parsed.Actions["outline_chat_change"].Credits != 5 {
		t.Fatalf("outline_chat_change = %d, want 5", parsed.Actions["outline_chat_change"].Credits)
	}
}

func TestDecisionGateCheck(t *testing.T) {
	ownerID := "00000000-0000-4000-8000-000000000001"
	nonOwnerID := "00000000-0000-4000-8000-000000000002"

	tests := []struct {
		name        string
		env         string
		decision    string
		approver    string
		shouldPass  bool
		errContains string
	}{
		{
			name:        "production with interim decision fails",
			env:         "production",
			decision:    "interim_adr_0012",
			approver:    ownerID,
			shouldPass:  false,
			errContains: "owner approval",
		},
		{
			name:        "production with missing decision fails",
			env:         "production",
			decision:    "",
			approver:    ownerID,
			shouldPass:  false,
			errContains: "owner approval",
		},
		{
			name:        "production with pending decision fails",
			env:         "production",
			decision:    "pending",
			approver:    ownerID,
			shouldPass:  false,
			errContains: "owner approval",
		},
		{
			name:        "production with owner_approved but non-owner approver fails",
			env:         "production",
			decision:    "owner_approved",
			approver:    nonOwnerID,
			shouldPass:  false,
			errContains: "owner approval",
		},
		{
			name:       "production with owner_approved and owner approver passes",
			env:        "production",
			decision:   "owner_approved",
			approver:   ownerID,
			shouldPass: true,
		},
		{
			name:        "empty env treated as production (fails closed)",
			env:         "",
			decision:    "interim_adr_0012",
			approver:    ownerID,
			shouldPass:  false,
			errContains: "owner approval",
		},
		{
			name:        "staging treated as production (fails closed)",
			env:         "staging",
			decision:    "interim_adr_0012",
			approver:    ownerID,
			shouldPass:  false,
			errContains: "owner approval",
		},
		{
			name:       "development with interim decision passes",
			env:        "development",
			decision:   "interim_adr_0012",
			approver:   nonOwnerID,
			shouldPass: true,
		},
		{
			name:       "test with interim decision passes",
			env:        "test",
			decision:   "interim_adr_0012",
			approver:   nonOwnerID,
			shouldPass: true,
		},
		{
			name:        "development with pending decision fails",
			env:         "development",
			decision:    "pending",
			approver:    nonOwnerID,
			shouldPass:  false,
			errContains: "recorded decision",
		},
		{
			name:        "development with missing decision fails",
			env:         "development",
			decision:    "",
			approver:    nonOwnerID,
			shouldPass:  false,
			errContains: "recorded decision",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := sampleValidConfig()
			if tt.decision == "" {
				delete(cfg, "design_chat_decision")
			} else {
				cfg["design_chat_decision"] = tt.decision
			}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := Parse(raw)
			if err != nil {
				t.Fatalf("Parse error: %v", err)
			}

			err = CheckDesignChatDecision(parsed, tt.env, tt.approver, ownerID)
			if tt.shouldPass {
				if err != nil {
					t.Fatalf("expected pass, got: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected failure, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Fatalf("expected error containing %q, got: %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestActionFlagsAndRollbackMapping(t *testing.T) {
	cfg := sampleValidConfig()
	raw, _ := json.Marshal(cfg)
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	cat := FromConfig(parsed)
	if !cat.HasAction("setup_chat") {
		t.Fatal("expected HasAction(setup_chat) to be true")
	}
	if !cat.HasAction("course_brief_chat") {
		t.Fatal("expected HasAction(course_brief_chat) to be true")
	}
	if !cat.HasAction("outline_chat_change") {
		t.Fatal("expected HasAction(outline_chat_change) to be true")
	}
	if !cat.HasAction("outline") {
		t.Fatal("expected HasAction(outline) to be true")
	}

	// Remove design chat actions to simulate rollback version
	delete(cfg["actions"].(map[string]any), "setup_chat")
	delete(cfg["actions"].(map[string]any), "course_brief_chat")
	delete(cfg["actions"].(map[string]any), "outline_chat_change")
	cfg["daily_free"].(map[string]any)["eligible_actions"] = []string{"quick_note", "tutor_short", "tutor_long"}
	delete(cfg, "design_chat_decision")
	rawRolled, _ := json.Marshal(cfg)
	parsedRolled, err := Parse(rawRolled)
	if err != nil {
		t.Fatal(err)
	}
	catRolled := FromConfig(parsedRolled)

	if catRolled.HasAction("setup_chat") {
		t.Fatal("expected HasAction(setup_chat) to be false on rollback")
	}
	if catRolled.HasAction("course_brief_chat") {
		t.Fatal("expected HasAction(course_brief_chat) to be false on rollback")
	}
	if catRolled.HasAction("outline_chat_change") {
		t.Fatal("expected HasAction(outline_chat_change) to be false on rollback")
	}
	// Unrelated actions remain active
	if !catRolled.HasAction("outline") {
		t.Fatal("expected HasAction(outline) to remain true on rollback")
	}
	if !catRolled.HasAction("preview") {
		t.Fatal("expected HasAction(preview) to remain true on rollback")
	}
}

func TestDesignChatOwnerEnforcementFailsClosed(t *testing.T) {
	cfg, err := Parse(mustJSON(t, withDecision(sampleValidConfig(), "owner_approved")))
	if err != nil {
		t.Fatal(err)
	}
	owner := "00000000-0000-4000-8000-000000000001"
	for _, tc := range []struct{ name, approver, owner string }{
		{"empty approver and owner", "", ""},
		{"empty approver with owner set", "", owner},
		{"approver set with empty owner", owner, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckDesignChatDecision(cfg, "production", tc.approver, tc.owner); err == nil {
				t.Fatal("production owner check passed with empty identity")
			}
		})
	}
	if err := CheckDesignChatDecision(cfg, "production", strings.ToUpper(owner), owner); err != nil {
		t.Fatalf("owner rejected: %v", err)
	}
}

func withDecision(cfg map[string]any, decision string) map[string]any {
	cfg["design_chat_decision"] = decision
	return cfg
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestClassifyAdverseDesignChatRemoval(t *testing.T) {
	build := func(mutate func(map[string]any)) []byte {
		cfg := sampleValidConfig()
		cfg["limits"].(map[string]any)["outline_chat_change"] = map[string]any{"input_tokens": 1}
		mutate(cfg)
		return mustJSON(t, cfg)
	}
	before := build(func(map[string]any) {})
	tests := []struct {
		name    string
		mutate  func(map[string]any)
		adverse bool
	}{
		{"eligibility removed, action still priced", func(c map[string]any) {
			c["daily_free"].(map[string]any)["eligible_actions"] = []string{"quick_note", "tutor_short", "tutor_long"}
		}, true},
		{"limits entry removed, action still priced", func(c map[string]any) {
			delete(c["limits"].(map[string]any), "outline_chat_change")
		}, true},
		{"action removed with eligibility and limits (rollback)", func(c map[string]any) {
			delete(c["actions"].(map[string]any), "outline_chat_change")
			delete(c["limits"].(map[string]any), "outline_chat_change")
			c["daily_free"].(map[string]any)["eligible_actions"] = []string{"quick_note", "tutor_short", "tutor_long"}
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := classifyAdverse(before, build(tt.mutate))
			if err != nil || got != tt.adverse {
				t.Fatalf("adverse=%v err=%v, want %v", got, err, tt.adverse)
			}
		})
	}
}
