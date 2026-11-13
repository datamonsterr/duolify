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

func TestGamificationPostHogCatalogueConsentAndPrivacy(t *testing.T) {
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
	for _, allowed := range []bool{true, false} {
		events, err := NewWithOptions("fixture-only", server.URL, Options{Consent: func(context.Context, string) (bool, error) { return allowed, nil }})
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"xp_earned", "lesson_completed", "daily_goal_met"} {
			if !allowlisted(name) {
				t.Fatal("missing catalogue", name)
			}
			if err = events.Capture("11111111-1111-4111-8111-111111111111", name, map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "event_id": "22222222-2222-4222-8222-222222222222", "amount": 23, "xp": 23, "goal_xp": 23, "local_date": "2026-11-01", "summary": "private learner text", "prompt": "private prompt"}); err != nil {
				t.Fatal(err)
			}
		}
		if err = events.Close(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Fatal("delivery/opt-out", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") {
			t.Fatal("leak", event)
		}
		props := event["properties"].(map[string]any)
		if props["source"] != "api" || props["platform"] != "web" || props["app_version"] != "0.1.0" || props["event_id"] == nil {
			t.Fatal("common properties", event)
		}
		if event["event"] == "lesson_completed" && props["xp"] != float64(23) {
			t.Fatal("lost authoritative XP", event)
		}
	}
}
