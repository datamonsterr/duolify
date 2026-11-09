package analytics

import (
	"testing"
)

func TestEventAllowlist(t *testing.T) {
	for _, name := range []string{"learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed", "backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed"} {
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
