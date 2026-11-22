package analytics

import "testing"

func TestRegionQuestionEventsKeepOnlyIdentifiersAndEnums(t *testing.T) {
	for _, event := range []string{"lesson_region_selected", "lesson_region_question_sent", "lesson_answer_panel_closed"} {
		if !allowlisted(event) {
			t.Fatalf("%s is not allowlisted", event)
		}
	}
	sent := safeProperties("lesson_region_question_sent", map[string]any{
		"course_id": "c1", "lesson_id": "l1", "slide_index": 2, "input": "text", "has_region": true, "grounding": "whole_slide",
		"message": "private question", "description": "private region text", "x": 0.25, "rect": map[string]any{"x": 0.1},
	})
	if sent["input"] != "text" || sent["grounding"] != "whole_slide" || sent["has_region"] != true || sent["slide_index"] != 2 || sent["lesson_id"] != "l1" {
		t.Fatalf("allowed properties lost: %v", sent)
	}
	for _, key := range []string{"message", "description", "x", "rect"} {
		if _, ok := sent[key]; ok {
			t.Fatalf("%s must not reach analytics: %v", key, sent)
		}
	}
	bad := safeProperties("lesson_region_question_sent", map[string]any{"input": "telepathy", "grounding": "everything"})
	if bad["input"] != nil || bad["grounding"] != nil {
		t.Fatalf("unknown enum values kept: %v", bad)
	}
	selected := safeProperties("lesson_region_selected", map[string]any{"course_id": "c1", "lesson_id": "l1", "slide_index": 1, "method": "two_finger", "area_bucket": "small", "regions_matched": "1", "free_text": "x"})
	if selected["method"] != "two_finger" || selected["area_bucket"] != "small" || selected["regions_matched"] != "1" || selected["free_text"] != nil {
		t.Fatalf("%v", selected)
	}
	closed := safeProperties("lesson_answer_panel_closed", map[string]any{"course_id": "c1", "lesson_id": "l1", "outcome": "stopped", "saved_note": true, "follow_ups": "2_plus"})
	if closed["outcome"] != "stopped" || closed["saved_note"] != true || closed["follow_ups"] != "2_plus" {
		t.Fatalf("%v", closed)
	}
}
