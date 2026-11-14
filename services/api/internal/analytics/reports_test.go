package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestReportEventsReachPostHogWithNoTextAndHonorOptOut(t *testing.T) {
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
	props := map[string]any{"source": "api", "platform": "web", "app_version": "test", "course_id": id, "report_id": id, "reason": "copyright", "target_type": "review", "reporter_count": 3, "text": "PRIVATE-REPORT", "email": "private@example.test", "username": "PRIVATE-NAME"}
	for _, event := range []string{"course_reported", "course_hidden_by_reports"} {
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
	if len(received) != 2 {
		t.Fatalf("delivery=%v", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "private@example") {
			t.Fatalf("PII=%s", raw)
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != id || p["source"] != "api" || p["platform"] != "web" || p["app_version"] != "test" || p["course_id"] != id {
			t.Fatalf("common=%v", event)
		}
		switch event["event"] {
		case "course_reported":
			if p["reason"] != "copyright" || p["target_type"] != "review" || p["report_id"] != id {
				t.Fatalf("reported=%v", p)
			}
		case "course_hidden_by_reports":
			if p["reporter_count"] != float64(3) || p["reason"] != nil {
				t.Fatalf("hidden=%v", p)
			}
		}
	}
	opted, err := NewWithOptions("fixture", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = opted.Capture(id, "course_reported", props); err != nil {
		t.Fatal(err)
	}
	if err = opted.Close(); err != nil {
		t.Fatal(err)
	}
	if len(received) != 2 {
		t.Fatal("optout sent events")
	}
	filtered := safeProperties("course_reported", map[string]any{"course_id": "private-text", "report_id": "private-text", "reason": "private-text", "target_type": "private-text"})
	if len(filtered) != 0 {
		t.Fatalf("unbounded properties=%v", filtered)
	}
	if hidden := safeProperties("course_hidden_by_reports", map[string]any{"course_id": id, "reporter_count": 1_000_000}); hidden["reporter_count"] != nil {
		t.Fatalf("unbounded count=%v", hidden)
	}
}
