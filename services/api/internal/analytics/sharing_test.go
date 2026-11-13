package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCourseScreeningEventNeverCapturesContent(t *testing.T) {
	for _, e := range []string{"course_visibility_changed", "course_published", "share_link_created", "share_link_revoked", "share_link_opened"} {
		if !allowlisted(e) {
			t.Fatalf("%s missing from the catalogue", e)
		}
	}
	tok := "SECRETTOKEN-abc"
	raw0, _ := json.Marshal(safeProperties("share_link_opened", map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "course_id": "c1", "has_account": false, "surface": "web", "token": tok, "ip": "1.2.3.4", "preview_url": tok}))
	if strings.Contains(string(raw0), "SECRET") || strings.Contains(string(raw0), "1.2.3.4") || !strings.Contains(string(raw0), `"has_account":false`) {
		t.Fatalf("share_link_opened props wrong: %s", raw0)
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
