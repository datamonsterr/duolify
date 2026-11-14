package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestReviewEventsReachPostHogWithNoTextAndHonorOptOut(t *testing.T) {
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
	events, err := NewWithOptions("fixture", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	id := "11111111-1111-4111-8111-111111111111"
	props := map[string]any{"source": "api", "platform": "web", "app_version": "test", "course_id": id, "review_id": id, "rating": 5, "version": 2, "verdict": "block", "feature": "screen_review", "text": "PRIVATE-REVIEW", "email": "private@example.test", "author": "PRIVATE-NAME"}
	for _, event := range []string{"review_submitted", "review_deleted", "$ai_generation"} {
		if !allowlisted(event) {
			t.Fatal(event)
		}
		if err := events.Capture(id, event, props); err != nil {
			t.Fatal(err)
		}
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Fatalf("delivery=%v", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "private@example") {
			t.Fatalf("PII=%s", raw)
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != id || p["source"] != "api" || p["platform"] != "web" || p["app_version"] != "test" {
			t.Fatalf("common=%v", event)
		}
		if event["event"] == "review_submitted" && (p["rating"] != float64(5) || p["verdict"] != "block" || p["course_id"] != id) {
			t.Fatalf("review=%v", p)
		}
	}
	opted, err := NewWithOptions("fixture", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = opted.Capture(id, "review_submitted", props); err != nil {
		t.Fatal(err)
	}
	if err = opted.Close(); err != nil {
		t.Fatal(err)
	}
	if len(received) != 3 {
		t.Fatal("optout sent events")
	}
	filtered := safeProperties("review_submitted", map[string]any{"course_id": "private-text", "review_id": "private-text", "rating": 6, "verdict": "private-text"})
	if len(filtered) != 0 {
		t.Fatalf("unbounded properties=%v", filtered)
	}
}
