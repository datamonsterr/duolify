package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRepublishAndUpdateEventsAreCataloguedWithIDsEnumsAndCounts(t *testing.T) {
	id, src := "a1111111-1111-4111-8111-111111111111", "b2222222-2222-4222-8222-222222222222"
	for _, name := range []string{"course_republished", "course_update_applied"} {
		if !allowlisted(name) {
			t.Fatalf("%s missing from the catalogue", name)
		}
	}
	encode := func(event string, in map[string]any) string {
		in["source"], in["platform"], in["app_version"] = "api", "web", "0.1.0"
		raw, _ := json.Marshal(safeProperties(event, in))
		return string(raw)
	}
	rep := encode("course_republished", map[string]any{"course_id": id, "version": 3, "lesson_count": 6, "verdict": "pass", "changelog": "Rewrote lesson 2", "title": "Astronomy"})
	for _, banned := range []string{"Rewrote", "Astronomy", "changelog", "title"} {
		if strings.Contains(rep, banned) {
			t.Errorf("course_republished leaked %q: %s", banned, rep)
		}
	}
	for _, want := range []string{id, `"version":3`, `"lesson_count":6`, `"verdict":"pass"`} {
		if !strings.Contains(rep, want) {
			t.Errorf("course_republished missing %s: %s", want, rep)
		}
	}
	if strings.Contains(encode("course_republished", map[string]any{"course_id": id, "verdict": "free text"}), "free text") {
		t.Error("verdict is not a closed enum")
	}
	upd := encode("course_update_applied", map[string]any{"course_id": id, "source_course_id": src, "from_version": 1, "to_version": 2, "kept": 2, "added": 1, "archived": 1, "title": "Moon"})
	for _, want := range []string{id, src, `"from_version":1`, `"to_version":2`, `"kept":2`, `"added":1`, `"archived":1`} {
		if !strings.Contains(upd, want) {
			t.Errorf("course_update_applied missing %s: %s", want, upd)
		}
	}
	if strings.Contains(upd, "Moon") {
		t.Errorf("course_update_applied leaked a title: %s", upd)
	}
}
