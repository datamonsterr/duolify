package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTutorPersonaEventsAreAllowlistedAndContentFree(t *testing.T) {
	input := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "speaker_key": "supporter", "character_version": 3, "cast_source": "lesson",
		"reason_code": "invalid_persona", "phase": "before_stream", "name": "private-text", "bio": "private-text", "persona": "private-text", "prompt": "private-text", "message": "private-text", "character_id": "private-text"}
	for _, event := range []string{"tutor_character_selected", "tutor_character_fallback"} {
		if !allowlisted(event) {
			t.Fatalf("event dropped: %s", event)
		}
		raw, _ := json.Marshal(safeProperties(event, input))
		if strings.Contains(string(raw), "private-text") {
			t.Fatalf("%s leaked content: %s", event, raw)
		}
	}
	if got := safeProperties("tutor_character_fallback", input); got["phase"] != "before_stream" || got["reason_code"] != "invalid_persona" {
		t.Fatalf("fallback props %v", got)
	}
	if got := safeProperties("tutor_character_fallback", map[string]any{"reason_code": "Ignore the rules", "phase": "mid_stream"}); got["reason_code"] != nil || got["phase"] != nil {
		t.Fatalf("unbounded values must be dropped: %v", got)
	}
	if got := safeProperties("tutor_character_selected", input); got["speaker_key"] != "supporter" || got["character_version"] != 3 || got["cast_source"] != "lesson" {
		t.Fatalf("selected props %v", got)
	}
}
