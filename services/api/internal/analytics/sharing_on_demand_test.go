package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOnDemandGenerationEventIsUUIDsAndNumbersOnly(t *testing.T) {
	if !allowlisted("published_lesson_generated_on_demand") {
		t.Fatal("published_lesson_generated_on_demand missing from the catalogue")
	}
	id := "a1111111-1111-4111-8111-111111111111"
	raw, _ := json.Marshal(safeProperties("published_lesson_generated_on_demand", map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"course_id": id, "lesson_id": id, "version": 3,
		"title": "Eclipses", "html": "<h1>x</h1>", "quote_id": "free text", "answer": "Shadows",
	}))
	for _, banned := range []string{"Eclipses", "<h1>", "free text", "Shadows", "quote_id"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("leaked %q: %s", banned, raw)
		}
	}
	for _, want := range []string{`"course_id"`, `"lesson_id"`, `"version":3`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
}
