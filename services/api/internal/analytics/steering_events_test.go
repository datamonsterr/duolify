package analytics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSteeringEventsPostHogDeliveryAndOptOut(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		received = append(received, body.Batch...)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer server.Close()
	const owner = "a1111111-1111-4111-8111-111111111111"
	events, err := NewWithOptions("fixture", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	props := map[string]any{"source": "api", "platform": "web", "app_version": "1.0.0", "course_id": "c1", "lesson_id": "l1", "scope": "next_lesson", "length_bucket": "1_100", "reason_code": "prompt_injection", "is_course_copy": false, "feature": "steer_screen", "input_tokens": 20, "output_tokens": 5, "provider": "mock", "model": "mock-screen", "text": "PRIVATE_LEARNER_TEXT", "prompt": "PRIVATE_LEARNER_TEXT"}
	for _, event := range []string{"course_instruction_added", "course_instruction_rejected", "course_instruction_applied", "course_instruction_output_flagged", "$ai_generation"} {
		if err = events.Capture(owner, event, props); err != nil {
			t.Fatal(err)
		}
	}
	_ = events.Capture("invalid-uuid", "course_instruction_added", props)
	opted, err := NewWithOptions("fixture", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = opted.Capture(owner, "course_instruction_added", props)
	_ = opted.Close()
	denied := WithConsent(events, func(context.Context, string) (bool, error) { return false, nil })
	_ = denied.Capture(owner, "course_instruction_added", props)
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 5 {
		t.Fatalf("events: %v", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "PRIVATE_LEARNER_TEXT") {
			t.Fatal("text leaked")
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != owner || p["source"] != "api" || p["platform"] != "web" || p["app_version"] != "1.0.0" {
			t.Fatal(event)
		}
		if event["event"] == "course_instruction_added" && p["length_bucket"] != "1_100" {
			t.Fatal(p)
		}
	}
}

func TestSteeringEditDeleteEventsAreAllowlistedAndTextFree(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []map[string]any `json:"batch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		received = append(received, body.Batch...)
		mu.Unlock()
	}))
	defer server.Close()
	const owner = "a1111111-1111-4111-8111-111111111111"
	events, err := NewWithOptions("fixture", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	props := map[string]any{"source": "api", "platform": "web", "app_version": "1.0.0", "course_id": "c1", "scope": "rest_of_course", "length_bucket": "101_250", "changed": "both", "text": "PRIVATE_LEARNER_TEXT", "new_text": "PRIVATE_LEARNER_TEXT"}
	for _, event := range []string{"course_instruction_edited", "course_instruction_deleted"} {
		if err = events.Capture(owner, event, props); err != nil {
			t.Fatal(err)
		}
	}
	bad := map[string]any{"source": "api", "platform": "web", "app_version": "1.0.0", "course_id": "c1", "changed": "PRIVATE_LEARNER_TEXT"}
	_ = events.Capture(owner, "course_instruction_edited", bad)
	denied := WithConsent(events, func(context.Context, string) (bool, error) { return false, nil })
	_ = denied.Capture(owner, "course_instruction_deleted", props)
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Fatalf("events: %v", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "PRIVATE_LEARNER_TEXT") {
			t.Fatalf("text leaked: %s", raw)
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != owner || p["source"] != "api" || p["course_id"] != "c1" {
			t.Fatal(event)
		}
	}
	first := received[0]["properties"].(map[string]any)
	if first["changed"] != "both" || first["scope"] != "rest_of_course" || first["length_bucket"] != "101_250" {
		t.Fatal(first)
	}
}
