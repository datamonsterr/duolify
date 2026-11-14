package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestModerationEventsReachPostHogWithNoTextAndHonorOptOut(t *testing.T) {
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
	common := map[string]any{"source": "api", "platform": "web", "app_version": "test", "course_id": id}
	for event, extra := range map[string]map[string]any{
		"moderation_action":         {"action_id": id, "action": "takedown", "reason_code": "unsafe", "target_type": "publication", "reason": "PRIVATE reason text", "moderator": "private@example.test"},
		"moderation_appeal":         {"action_id": id, "outcome": "overturned", "text": "PRIVATE appeal text"},
		"course_visibility_changed": {"from": "public", "to": "private", "cause": "moderation"},
	} {
		props := map[string]any{}
		for k, v := range common {
			props[k] = v
		}
		for k, v := range extra {
			props[k] = v
		}
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
			t.Fatalf("private text delivered: %s", raw)
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != id || p["source"] != "api" || p["platform"] != "web" || p["course_id"] != id {
			t.Fatalf("common=%v", event)
		}
		switch event["event"] {
		case "moderation_action":
			if p["action"] != "takedown" || p["reason_code"] != "unsafe" || p["target_type"] != "publication" || p["action_id"] != id {
				t.Fatalf("action=%v", p)
			}
		case "moderation_appeal":
			if p["outcome"] != "overturned" || p["action_id"] != id {
				t.Fatalf("appeal=%v", p)
			}
		}
	}
	opted, err := NewWithOptions("fixture", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = opted.Capture(id, "moderation_action", common); err != nil {
		t.Fatal(err)
	}
	if err = opted.Close(); err != nil {
		t.Fatal(err)
	}
	if len(received) != 3 {
		t.Fatal("opt-out sent events")
	}
	if bad := safeProperties("moderation_action", map[string]any{"action": "PRIVATE", "reason_code": "PRIVATE", "target_type": "PRIVATE", "course_id": "PRIVATE"}); len(bad) != 0 {
		t.Fatalf("unbounded properties=%v", bad)
	}
	if bad := safeProperties("moderation_appeal", map[string]any{"outcome": "granted"}); len(bad) != 0 {
		t.Fatalf("unknown outcome=%v", bad)
	}
}
