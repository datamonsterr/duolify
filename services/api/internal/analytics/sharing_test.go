package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCourseScreeningEventNeverCapturesContent(t *testing.T) {
	if allowlisted("course_published") {
		t.Fatal("course_published must stay off until visibility is live")
	}
	if !allowlisted("course_screening_completed") {
		t.Fatal("course_screening_completed dropped")
	}
	in := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "verdict": "needs_review", "lessons_sampled": 3, "finding_count": 2, "fallback_used": true,
		"title": "private-text", "findings": "private-text", "reason": "private-text", "course_id": "private-text course"}
	raw, _ := json.Marshal(safeProperties("course_screening_completed", in))
	if strings.Contains(string(raw), "private-text") {
		t.Fatalf("leaked content: %s", raw)
	}
	for _, want := range []string{`"verdict":"needs_review"`, `"lessons_sampled":3`, `"finding_count":2`, `"fallback_used":true`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
	in["verdict"] = "free text verdict"
	if raw, _ = json.Marshal(safeProperties("course_screening_completed", in)); strings.Contains(string(raw), "verdict") {
		t.Errorf("open verdict accepted: %s", raw)
	}
}
