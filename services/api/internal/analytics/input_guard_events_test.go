package analytics

import "testing"

// ADR 0025: the decision event carries closed enums and flags, never text.
func TestInputGuardDecisionKeepsOnlyClosedValues(t *testing.T) {
	if !allowlisted("input_guard_decision") {
		t.Fatal("input_guard_decision must be allowlisted")
	}
	got := safeProperties("input_guard_decision", map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"surface": "tutor_chat", "allowed": false, "reason_code": "self_harm", "guard_source": "model", "latency_ms": 120,
		"text": "I want to hurt myself", "raw": "x", "reason": "free text",
	})
	if got["surface"] != "tutor_chat" || got["reason_code"] != "self_harm" || got["guard_source"] != "model" || got["allowed"] != false {
		t.Fatalf("closed fields lost: %v", got)
	}
	for _, k := range []string{"text", "raw", "reason"} {
		if _, ok := got[k]; ok {
			t.Fatalf("%s must be dropped: %v", k, got)
		}
	}
	bad := safeProperties("input_guard_decision", map[string]any{"surface": "somewhere", "reason_code": "because", "guard_source": "x"})
	if len(bad) != 0 {
		t.Fatalf("unknown enum values must be dropped: %v", bad)
	}
}

func TestAIGenerationCarriesPromptModeAndRouterSourceAsClosedValues(t *testing.T) {
	got := safeProperties("$ai_generation", map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0", "feature": "lesson", "model": "m",
		"prompt_mode": "routed", "router_source": "cache", "skill_count": uint32(3),
	})
	if got["prompt_mode"] != "routed" || got["router_source"] != "cache" || got["skill_count"] != uint32(3) {
		t.Fatalf("lost: %v", got)
	}
	bad := safeProperties("$ai_generation", map[string]any{"feature": "lesson", "prompt_mode": "diagram_layout body", "router_source": "x"})
	if _, ok := bad["prompt_mode"]; ok {
		t.Fatalf("free text kept: %v", bad)
	}
	if _, ok := bad["router_source"]; ok {
		t.Fatalf("unknown router source kept: %v", bad)
	}
}
