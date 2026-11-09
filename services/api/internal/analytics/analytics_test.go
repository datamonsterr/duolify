package analytics

import (
	"reflect"
	"testing"
)

func TestEventAllowlist(t *testing.T) {
	for _, name := range []string{
		"learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed",
		"backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed",
		"$ai_generation", "ui_control_activated",
		"pricing_version_created", "pricing_version_approved", "pricing_version_rejected",
		"pricing_version_activated", "pricing_version_rolled_back", "price_table_activated",
		"daily_credits_granted", "daily_credits_expired",
	} {
		if !allowlisted(name) {
			t.Errorf("catalogue event %q rejected", name)
		}
	}
	for _, name := range []string{"private_text", "ui_appearance_changed", "unknown_event"} {
		if allowlisted(name) {
			t.Errorf("uncatalogued/API-owned event %q accepted", name)
		}
	}
}

func TestCourseBriefAttemptProperties(t *testing.T) {
	for _, succeeded := range []bool{false, true} {
		input := map[string]any{
			"source": "api", "platform": "web", "app_version": "0.1.0",
			"task": "course_brief_chat", "pricing_version": "usd-dev-course-brief-unapproved",
			"attempt": 2, "provider": "mock", "model": "haiku",
			"input_tokens": uint32(10), "output_tokens": uint32(5), "latency_ms": uint32(20),
			"fallback_used": true, "succeeded": succeeded, "usage_available": succeeded,
			"answer": "private answer", "brief": map[string]string{"topic": "private topic"},
		}
		got := safeProperties("$ai_generation", input)
		for _, key := range []string{"task", "pricing_version", "attempt", "provider", "model", "input_tokens", "output_tokens", "latency_ms", "fallback_used", "succeeded", "usage_available"} {
			if !reflect.DeepEqual(got[key], input[key]) {
				t.Errorf("%s: got %v, want %v", key, got[key], input[key])
			}
		}
		if got["$ai_is_error"] != !succeeded || got["feature"] != "course_brief_chat" {
			t.Errorf("incorrect canonical attempt properties: %v", got)
		}
		for _, key := range []string{"answer", "brief"} {
			if _, ok := got[key]; ok {
				t.Errorf("private property %s retained", key)
			}
		}
	}
}
