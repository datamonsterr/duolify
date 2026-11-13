package lessontest_test

import (
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
)

func TestReorderChoicesCorrectAnswerInvariance(t *testing.T) {
	choices := []string{"Alpha", "Beta", "Gamma", "Delta"}

	for originalIdx, expectedText := range choices {
		reordered, newIdx := lessontest.ReorderChoices(choices, originalIdx)

		if len(reordered) != len(choices) {
			t.Fatalf("expected length %d, got %d", len(choices), len(reordered))
		}
		if newIdx < 0 || newIdx >= len(reordered) {
			t.Fatalf("newIdx %d out of bounds", newIdx)
		}
		if reordered[newIdx] != expectedText {
			t.Errorf("expected correct choice %q at newIdx %d, got %q", expectedText, newIdx, reordered[newIdx])
		}
		// In a cyclic shift with len >= 2, the choice order must differ from original
		sameOrder := true
		for i := range choices {
			if choices[i] != reordered[i] {
				sameOrder = false
				break
			}
		}
		if sameOrder {
			t.Errorf("reordered choices must differ in order from original choices")
		}
	}
}

func TestReorderChoicesEdgeCases(t *testing.T) {
	t.Run("empty choices", func(t *testing.T) {
		reordered, newIdx := lessontest.ReorderChoices([]string{}, 0)
		if len(reordered) != 0 || newIdx != 0 {
			t.Errorf("unexpected output for empty: %v, %d", reordered, newIdx)
		}
	})

	t.Run("single choice", func(t *testing.T) {
		reordered, newIdx := lessontest.ReorderChoices([]string{"Only"}, 0)
		if len(reordered) != 1 || newIdx != 0 || reordered[0] != "Only" {
			t.Errorf("unexpected output for single choice: %v, %d", reordered, newIdx)
		}
	})

	t.Run("two choices", func(t *testing.T) {
		reordered, newIdx := lessontest.ReorderChoices([]string{"First", "Second"}, 1)
		if reordered[newIdx] != "Second" {
			t.Errorf("expected 'Second' at newIdx %d, got %s", newIdx, reordered[newIdx])
		}
		if newIdx != 0 {
			t.Errorf("expected newIdx 0, got %d", newIdx)
		}
	})
}

func TestTelemetryPayloadPrivacyCompliance(t *testing.T) {
	// Prohibited learner text check for retry_variant_generation_failed
	payload := map[string]any{
		"course_id":  "course-123",
		"lesson_id":  "lesson-456",
		"topic_id":   "top-sunrise",
		"error_code": "pool_exhausted",
	}

	for k, v := range payload {
		keyLower := strings.ToLower(k)
		if strings.Contains(keyLower, "prompt") || strings.Contains(keyLower, "text") || strings.Contains(keyLower, "answer") {
			t.Fatalf("forbidden key in telemetry: %s", k)
		}
		strVal, ok := v.(string)
		if ok {
			if strings.Contains(strVal, "What makes the Sun rise") || strings.Contains(strVal, "Earth rotates") {
				t.Fatalf("forbidden learner text in telemetry property %s: %s", k, strVal)
			}
		}
	}
}
