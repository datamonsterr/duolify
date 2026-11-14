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

func TestReplanEventsPostHogDeliveryOptOutAndTextFree(t *testing.T) {
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
		w.WriteHeader(200)
	}))
	defer server.Close()
	const owner = "a1111111-1111-4111-8111-111111111111"
	events, err := NewWithOptions("fixture", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	common := map[string]any{"source": "api", "platform": "web", "app_version": "1.0.0", "course_id": "c1", "proposal_id": "p1", "pricing_version": "usd-replan-v1", "credits": 55, "lessons_added": 1, "lessons_removed": 2, "lessons_changed": 3,
		"text": "PRIVATE_LEARNER_TEXT", "instructions": "PRIVATE_LEARNER_TEXT", "outline": "PRIVATE_LEARNER_TEXT", "title": "PRIVATE_LEARNER_TEXT", "diff": "PRIVATE_LEARNER_TEXT"}
	for _, event := range []string{"course_replan_quoted", "course_replan_proposed"} {
		if err = events.Capture(owner, event, common); err != nil {
			t.Fatal(err)
		}
	}
	failed := map[string]any{"course_id": "c1", "reason": "insufficient_credits"}
	for k, v := range common {
		if _, ok := failed[k]; !ok {
			failed[k] = v
		}
	}
	if err = events.Capture(owner, "course_replan_failed", failed); err != nil {
		t.Fatal(err)
	}
	// A free-form reason is dropped, never forwarded.
	if err = events.Capture(owner, "course_replan_failed", map[string]any{"source": "api", "platform": "web", "app_version": "1.0.0", "course_id": "c1", "reason": "PRIVATE_LEARNER_TEXT"}); err != nil {
		t.Fatal(err)
	}
	_ = events.Capture("invalid-uuid", "course_replan_quoted", common)
	opted, err := NewWithOptions("fixture", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = opted.Capture(owner, "course_replan_quoted", common)
	_ = opted.Close()
	denied := WithConsent(events, func(context.Context, string) (bool, error) { return false, nil })
	_ = denied.Capture(owner, "course_replan_quoted", common)
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 4 {
		t.Fatalf("events: %v", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "PRIVATE_LEARNER_TEXT") {
			t.Fatalf("text leaked: %s", raw)
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != owner || p["source"] != "api" || p["platform"] != "web" || p["app_version"] != "1.0.0" || p["course_id"] != "c1" {
			t.Fatal(event)
		}
	}
	if received[1]["event"] != "course_replan_proposed" || received[1]["properties"].(map[string]any)["lessons_changed"] != float64(3) {
		t.Fatal(received[1])
	}
	if received[2]["properties"].(map[string]any)["reason"] != "insufficient_credits" {
		t.Fatal(received[2])
	}
	if _, ok := received[3]["properties"].(map[string]any)["reason"]; ok {
		t.Fatal("free-form reason forwarded")
	}
}
