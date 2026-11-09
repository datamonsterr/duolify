package courses

import (
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

func TestValidateCourseLevel(t *testing.T) {
	for _, valid := range []string{"", "beginner", "intermediate", "advanced", "expert"} {
		if err := ValidateCourseLevel(valid); err != nil {
			t.Fatalf("expected valid level %q, got error: %v", valid, err)
		}
	}

	for _, invalid := range []string{"master", "pro", "guru", "BEGINNER", " ", "invalid-level"} {
		if err := ValidateCourseLevel(invalid); err == nil {
			t.Fatalf("expected invalid level %q to be rejected", invalid)
		}
	}
}

func TestProjectEffectiveProfile(t *testing.T) {
	base := &enginev1.LearnerProfile{
		LearnerId:       "11111111-1111-4111-8111-111111111111",
		Locale:          "en-US",
		Background:      "Physics amateur",
		Goals:           "Stargazing",
		Level:           "beginner",
		Topic:           "Astronomy",
		TimeZone:        "UTC",
		DailyMinutes:    15,
		DailyXp:         30,
		ReminderEnabled: true,
		ReminderTime:    "20:00",
	}

	// Case 1: Course override is present ("advanced") -> overrides Level, preserves all other fields
	eff := ProjectEffectiveProfile(base, "advanced")
	if eff.GetLevel() != "advanced" {
		t.Fatalf("expected level 'advanced', got %s", eff.GetLevel())
	}
	if eff.GetGoals() != "Stargazing" || eff.GetBackground() != "Physics amateur" || eff.GetLocale() != "en-US" || eff.GetDailyMinutes() != 15 {
		t.Fatalf("global fields modified unexpectedly: %v", eff)
	}

	// Case 2: Course override is empty ("") -> falls back to base Level ("beginner")
	effReset := ProjectEffectiveProfile(base, "")
	if effReset.GetLevel() != "beginner" {
		t.Fatalf("expected level 'beginner', got %s", effReset.GetLevel())
	}

	// Case 3: Nil base profile -> returns profile with course override if provided
	effNil := ProjectEffectiveProfile(nil, "intermediate")
	if effNil.GetLevel() != "intermediate" {
		t.Fatalf("expected level 'intermediate', got %s", effNil.GetLevel())
	}
}
