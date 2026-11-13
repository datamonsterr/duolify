package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestLessonTutorEventsReachPostHogWithoutPrivateText(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		received = append(received, body.Batch...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	const learner = "3f2b6c1e-7a1d-4a55-9c0b-2d9c5a4f8e10"
	events, err := NewWithOptions("fixture-only", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	common := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0"}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range common {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	_ = events.Capture(learner, "tutor_message_sent", with(map[string]any{"course_id": "c1", "class": "short", "message_length": 12, "has_lesson_context": true, "message": "private question", "lesson_title": "private title"}))
	_ = events.Capture(learner, "tutor_message_sent", with(map[string]any{"course_id": "c1", "class": "short", "message_length": 3, "has_lesson_context": "yes"}))
	_ = events.Capture(learner, "ui_sheet_opened", with(map[string]any{"sheet_id": "lesson_tutor", "draft": "private draft"}))
	_ = events.Capture(learner, "ui_recovery_selected", with(map[string]any{"surface": "lesson_tutor", "action": "top_up", "error_code": "insufficient_credits", "text": "private text"}))
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 4 {
		t.Fatalf("want 4 events, got %d", len(received))
	}
	raw, _ := json.Marshal(received)
	if strings.Contains(string(raw), "private") {
		t.Fatalf("private text leaked: %s", raw)
	}
	props := func(i int) map[string]any { return received[i]["properties"].(map[string]any) }
	if props(0)["has_lesson_context"] != true || props(1)["has_lesson_context"] != nil {
		t.Fatalf("has_lesson_context must be kept only as a boolean: %v %v", props(0), props(1))
	}
	if props(2)["sheet_id"] != "lesson_tutor" || props(3)["surface"] != "lesson_tutor" || props(3)["action"] != "top_up" {
		t.Fatalf("%v %v", props(2), props(3))
	}
	for i, e := range received {
		if e["distinct_id"] != learner && props(i)["distinct_id"] != learner {
			t.Fatalf("event %d lost the learner UUID: %v", i, e)
		}
	}
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = opted.Capture(learner, "tutor_message_sent", with(map[string]any{"has_lesson_context": true}))
	_ = opted.Close()
	if len(received) != 4 {
		t.Fatal("opt-out delivered an event")
	}
}
