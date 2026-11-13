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
		for _, name := range []string{"xp_earned", "lesson_completed", "daily_goal_met", "league_promoted", "league_demoted"} {
			if !allowlisted(name) {
				t.Fatal("missing catalogue", name)
			}
			if err = events.Capture("11111111-1111-4111-8111-111111111111", name, map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "event_id": "22222222-2222-4222-8222-222222222222", "amount": 23, "xp": 23, "goal_xp": 23, "week_start": "2026-11-02", "league": "Dawn", "rank": 1, "from_tier": 1, "to_tier": 2, "local_date": "2026-11-01", "summary": "private learner text", "prompt": "private prompt"}); err != nil {
				t.Fatal(err)
			}
		}
		if err = events.Close(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 5 {
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

func TestStreakEventsAreCataloguedConsentGatedAndSanitised(t *testing.T) {
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
		props := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "event_id": "22222222-2222-4222-8222-222222222222", "local_date": "2026-11-01", "broken_on": "2026-11-02", "streak_length": 3, "previous_length": 4, "summary": "private learner text"}
		for _, name := range []string{"streak_extended", "streak_broken"} {
			if !allowlisted(name) {
				t.Fatal("missing catalogue", name)
			}
			if err = events.Capture("11111111-1111-4111-8111-111111111111", name, props); err != nil {
				t.Fatal(err)
			}
		}
		if err = events.Close(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatal("delivery/opt-out", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") {
			t.Fatal("leak", event)
		}
		props := event["properties"].(map[string]any)
		if event["event"] == "streak_extended" && props["streak_length"] != float64(3) || event["event"] == "streak_broken" && props["previous_length"] != float64(4) || props["event_id"] == nil || props["source"] != "api" {
			t.Fatal("props", event)
		}
	}
}

func TestStreakFreezeEventsAreCataloguedConsentGatedAndSanitised(t *testing.T) {
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
		props := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "event_id": "22222222-2222-4222-8222-222222222222", "reason": "streak", "local_date": "2026-11-07", "streak_length": 7, "protected_on": "2026-11-08", "remaining": 0, "summary": "private learner text", "prompt": "private prompt"}
		for _, name := range []string{"streak_freeze_earned", "streak_freeze_used"} {
			if !allowlisted(name) {
				t.Fatal("missing catalogue", name)
			}
			if err = events.Capture("11111111-1111-4111-8111-111111111111", name, props); err != nil {
				t.Fatal(err)
			}
		}
		if err = events.Close(); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatal("delivery/opt-out", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") {
			t.Fatal("leak", event)
		}
		props := event["properties"].(map[string]any)
		if event["event"] == "streak_freeze_earned" && (props["reason"] != "streak" || props["streak_length"] != float64(7)) || event["event"] == "streak_freeze_used" && (props["remaining"] != float64(0) || props["protected_on"] != "2026-11-08") || props["event_id"] == nil || props["source"] != "api" {
			t.Fatal("props", event)
		}
	}
}
