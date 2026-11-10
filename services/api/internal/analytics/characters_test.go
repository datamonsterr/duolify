package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestCharacterEventSchemasNeverCaptureContent(t *testing.T) {
	for _, event := range []string{"character_updated", "character_deleted", "character_rejected", "course_cast_selected", "character_create_quoted", "character_created", "character_regenerated", "character_create_failed"} {
		if !allowlisted(event) {
			t.Fatalf("event dropped: %s", event)
		}
		input := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "character_id": "17111111-1111-4111-8111-111111111111", "character_version": 2, "fields_changed": []string{"info.name", "appearance.palette", "private-text"}, "operation": "manual_edit", "reason_code": "prompt_injection", "cast_source": "custom", "cast_version": 2, "supporting_count": 1, "name": "private-text", "bio": "private-text", "persona": "private-text", "svg": "private-text", "prompt": "private-text"}
		safe := safeProperties(event, input)
		raw, _ := json.Marshal(safe)
		if strings.Contains(string(raw), "private-text") {
			t.Fatalf("%s leaked content: %s", event, raw)
		}
		if event == "character_updated" && len(safe["fields_changed"].([]string)) != 2 {
			t.Fatal("schema fields dropped")
		}
	}
}

func TestPrivateCharacterEventsDeliverWithConsent(t *testing.T) {
	var mu sync.Mutex
	received := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var envelope struct {
			Batch []map[string]any `json:"batch"`
		}
		_ = json.NewDecoder(r.Body).Decode(&envelope)
		mu.Lock()
		received = append(received, envelope.Batch...)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	events, err := NewWithOptions("fixture-only", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	owner := "17111111-1111-4111-8111-111111111111"
	for _, event := range []string{"character_updated", "character_deleted", "character_rejected", "course_cast_selected", "character_create_quoted", "character_created", "character_regenerated", "character_create_failed"} {
		if err = events.Capture(owner, event, map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "operation": "manual_edit", "reason_code": "prompt_injection", "fields_changed": []string{"info.name"}, "prompt": "private text"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 8 {
		t.Fatalf("missing deliveries: %v", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if event["distinct_id"] != owner || strings.Contains(string(raw), "private text") {
			t.Fatalf("unsafe delivery: %s", raw)
		}
	}
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = opted.Capture(owner, "character_deleted", map[string]any{})
	_ = opted.Close()
	if len(received) != 8 {
		t.Fatal("opt-out sent events")
	}
}
