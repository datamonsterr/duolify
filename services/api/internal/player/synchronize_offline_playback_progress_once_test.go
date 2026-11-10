package player

import (
	"strings"
	"testing"
)

func TestSyncEventValidation(t *testing.T) {
	m := ObjectManifest{Version: 1, Slides: []ObjectSlide{{ID: "sky-1", DurationMS: 3000}}}

	validTime := "2026-11-10T04:00:00Z"
	for _, body := range []string{
		`{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":0,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-2_uuid-123","lesson_id":"lesson-1","version":1,"slide_id":"sky-1","time_ms":3000,"occurred_at":"` + validTime + `"}`,
		`{"events":[{"action_key":"act-batch-1","lesson_id":"lesson-1","version":1,"slide_id":"sky-1","time_ms":1500,"occurred_at":"` + validTime + `"}]}`,
	} {
		events, err := decodeSyncEvents(strings.NewReader(body), "lesson-1", m)
		if err != nil {
			t.Fatalf("expected valid sync event %s, got error: %v", body, err)
		}
		if len(events) == 0 {
			t.Fatalf("expected at least 1 event from %s", body)
		}
	}

	for _, body := range []string{
		`{}`,
		`null`,
		`[]`,
		`{"action_key":"","version":1,"slide_id":"sky-1","time_ms":0,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"has spaces!","version":1,"slide_id":"sky-1","time_ms":0,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-1","version":2,"slide_id":"sky-1","time_ms":0,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-1","lesson_id":"other-lesson","version":1,"slide_id":"sky-1","time_ms":0,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-1","version":1,"slide_id":"foreign","time_ms":0,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":-1,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":3001,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":100,"occurred_at":"not-a-timestamp"}`,
		`{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":100,"occurred_at":"` + validTime + `","completed":true}`,
		`{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":100,"occurred_at":"` + validTime + `"} {}`,
		`{"events":[]}`,
		`{"events":[{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":-5,"occurred_at":"` + validTime + `"}]}`,
	} {
		if _, err := decodeSyncEvents(strings.NewReader(body), "lesson-1", m); err == nil {
			t.Fatalf("accepted invalid sync event: %s", body)
		}
	}
}
