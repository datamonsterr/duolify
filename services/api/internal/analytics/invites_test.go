package analytics

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCourseInviteEventsAreCataloguedAndCarryOnlyIDsAndEnums(t *testing.T) {
	for _, name := range []string{"course_invite_sent", "course_invite_accepted", "course_invite_declined"} {
		if !allowlisted(name) {
			t.Fatalf("%s missing from the catalogue", name)
		}
	}
	id, inv := "a1111111-1111-4111-8111-111111111111", "b2222222-2222-4222-8222-222222222222"
	encode := func(event string, in map[string]any) string {
		if _, ok := in["source"]; !ok {
			in["source"] = "api"
		}
		in["platform"], in["app_version"] = "web", "0.1.0"
		raw, _ := json.Marshal(safeProperties(event, in))
		return string(raw)
	}
	sent := encode("course_invite_sent", map[string]any{"method": "friend", "course_id": id, "invite_id": inv,
		"username": "ada", "title": "Astronomy", "recipient_id": inv})
	for _, banned := range []string{"ada", "Astronomy", `"username"`, `"title"`, `"recipient_id"`} {
		if strings.Contains(sent, banned) {
			t.Errorf("course_invite_sent leaked %q: %s", banned, sent)
		}
	}
	for _, want := range []string{`"method":"friend"`, id, inv} {
		if !strings.Contains(sent, want) {
			t.Errorf("course_invite_sent missing %s: %s", want, sent)
		}
	}
	if strings.Contains(encode("course_invite_sent", map[string]any{"method": "email me@x.y", "course_id": "nope"}), "me@x.y") {
		t.Error("method is not a closed enum")
	}
	for _, name := range []string{"course_invite_accepted", "course_invite_declined"} {
		got := encode(name, map[string]any{"course_id": id, "invite_id": inv, "method": "username"})
		if !strings.Contains(got, inv) || !strings.Contains(got, id) {
			t.Errorf("%s lost its ids: %s", name, got)
		}
	}
	enrolled := encode("course_enrolled_published", map[string]any{"source": "invite", "course_id": id, "source_course_id": inv, "version": 2})
	if !strings.Contains(enrolled, `"source":"invite"`) {
		t.Errorf("source invite dropped: %s", enrolled)
	}
}
