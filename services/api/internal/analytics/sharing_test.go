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

func TestEnrollmentEventsAreUUIDsAndEnumsOnly(t *testing.T) {
	for _, e := range []string{"course_enrolled_published", "published_lesson_reused"} {
		if !allowlisted(e) {
			t.Fatalf("%s missing from the catalogue", e)
		}
	}
	const id = "11111111-1111-4111-8111-111111111111"
	raw, _ := json.Marshal(safeProperties("course_enrolled_published", map[string]any{"source": "link", "platform": "web", "app_version": "0.1.0",
		"course_id": id, "source_course_id": id, "version": 3, "token": "SECRETTOKEN", "title": "private-text", "username": "mina"}))
	for _, bad := range []string{"SECRET", "private-text", "mina"} {
		if strings.Contains(string(raw), bad) {
			t.Fatalf("leaked %s: %s", bad, raw)
		}
	}
	for _, want := range []string{`"course_id":"` + id + `"`, `"source_course_id":"` + id + `"`, `"version":3`, `"source":"link"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing %s in %s", want, raw)
		}
	}
	raw, _ = json.Marshal(safeProperties("course_enrolled_published", map[string]any{"source": "free text", "course_id": "has a space"}))
	if strings.Contains(string(raw), "free text") || strings.Contains(string(raw), "has a space") {
		t.Errorf("open values accepted: %s", raw)
	}
	raw, _ = json.Marshal(safeProperties("published_lesson_reused", map[string]any{"course_id": id, "lesson_id": id, "version": 2, "html": "<h1>x</h1>"}))
	if strings.Contains(string(raw), "<h1>") || !strings.Contains(string(raw), `"lesson_id"`) {
		t.Errorf("reuse props wrong: %s", raw)
	}
}
