package player

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSyncEventValidation(t *testing.T) {
	m := ObjectManifest{Version: 1, Slides: []ObjectSlide{{ID: "sky-1", DurationMS: 3000}}}

	validTime := "2026-11-10T04:00:00Z"
	for _, body := range []string{
		`{"action_key":"act-1","version":1,"slide_id":"sky-1","time_ms":0,"occurred_at":"` + validTime + `"}`,
		`{"action_key":"act-2_uuid-123","lesson_id":"lesson-1","version":1,"slide_id":"sky-1","time_ms":3000,"occurred_at":"` + validTime + `"}`,
		`{"events":[{"action_key":"act-batch-1","lesson_id":"lesson-1","version":1,"slide_id":"sky-1","time_ms":1500,"occurred_at":"` + validTime + `"}]}`,
	} {
		events, rejected, err := decodeSyncEvents(strings.NewReader(body), "lesson-1", m)
		if err != nil || len(rejected) != 0 {
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
		if events, rejected, err := decodeSyncEvents(strings.NewReader(body), "lesson-1", m); len(events) != 0 || (err == nil && len(rejected) == 0) {
			t.Fatalf("accepted invalid sync event: %s", body)
		}
	}
}

func TestSyncTimestampBounds(t *testing.T) {
	m := ObjectManifest{Version: 1, Slides: []ObjectSlide{{ID: "sky-1", DurationMS: 3000}}}
	for _, timestamp := range []string{"9999-01-01T00:00:00Z", "0001-01-01T00:00:00Z"} {
		key, slide, version, timeMS := "bounded", "sky-1", 1, 0
		before := time.Now().UTC()
		event, err := validateSyncEvent(rawSyncEvent{ActionKey: &key, SlideID: &slide, Version: &version, TimeMS: &timeMS, OccurredAt: &timestamp}, "lesson", m)
		if err != nil {
			t.Fatal(err)
		}
		if event.OccurredAt.After(before.Add(2*time.Minute+time.Second)) || event.OccurredAt.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("unbounded event timestamp: %v", event.OccurredAt)
		}
	}
}

func TestSyncBatchRejectsOnlyInvalidEvents(t *testing.T) {
	m := ObjectManifest{Version: 1, Slides: []ObjectSlide{{ID: "sky-1", DurationMS: 3000}}}
	body := `{"events":[{"action_key":"bad-type","time_ms":"wrong"},{"action_key":"bad-field","completed":true},null,{"action_key":"good","version":1,"slide_id":"sky-1","time_ms":0,"occurred_at":"2026-11-10T00:00:00Z"}]}`
	events, rejected, err := decodeSyncEvents(strings.NewReader(body), "lesson", m)
	if err != nil || len(events) != 1 || events[0].ActionKey != "good" || !reflect.DeepEqual(rejected, []string{"bad-type", "bad-field", ""}) {
		t.Fatalf("mixed batch: events=%v rejected=%v err=%v", events, rejected, err)
	}
}

func TestSyncRequestBodyLimit(t *testing.T) {
	m := ObjectManifest{Version: 1, Slides: []ObjectSlide{{ID: "sky-1", DurationMS: 3000}}}
	// A valid prefix followed by excess whitespace must not be silently accepted.
	body := `{"action_key":"good","version":1,"slide_id":"sky-1","time_ms":0,"occurred_at":"2026-11-10T00:00:00Z"}` + strings.Repeat(" ", 65536)
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/sync", strings.NewReader(body))
	w := httptest.NewRecorder()
	(&Service{}).sync(t.Context(), w, r, "owner", "lesson", m)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", w.Code, w.Body.String())
	}
}
