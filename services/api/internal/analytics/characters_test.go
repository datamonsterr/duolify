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
	for _, event := range []string{"character_updated", "character_deleted", "character_rejected", "course_cast_selected", "character_create_quoted", "character_created", "character_regenerated", "character_create_failed", "character_reaction_shown"} {
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
	for _, event := range []string{"character_updated", "character_deleted", "character_rejected", "course_cast_selected", "character_create_quoted", "character_created", "character_regenerated", "character_create_failed", "character_reaction_shown"} {
		if err = events.Capture(owner, event, map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "operation": "manual_edit", "reason_code": "prompt_injection", "fields_changed": []string{"info.name"}, "prompt": "private text"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 9 {
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
	if len(received) != 9 {
		t.Fatal("opt-out sent events")
	}
}

func TestLessonCastAppliedKeepsIdsAndEnumsOnly(t *testing.T) {
	if !allowlisted("lesson_cast_applied") {
		t.Fatal("lesson_cast_applied dropped")
	}
	safe := safeProperties("lesson_cast_applied", map[string]any{"source": "api", "platform": "web", "app_version": "1.0.0", "course_id": "f1111111-1111-4111-8111-111111111111", "lesson_id": "f3333333-3333-4333-8333-333333333333", "cast_source": "custom", "cast_version": 3, "supporting_count": 1, "fallback_used": true, "persona": "private-text", "name": "private-text", "dialogue": "private-text"})
	raw, _ := json.Marshal(safe)
	if strings.Contains(string(raw), "private-text") || safe["supporting_count"] == nil || safe["cast_source"] != "custom" {
		t.Fatalf("unexpected properties: %s", raw)
	}
}

func TestReactionShownKeepsOnlyClosedEnums(t *testing.T) {
	safe := safeProperties("character_reaction_shown", map[string]any{"reaction_trigger": "answer_incorrect", "expression": "thinking", "answer": "Sun", "copy": "Not quite", "svg": "<svg/>"})
	if len(safe) < 2 || safe["reaction_trigger"] != "answer_incorrect" || safe["expression"] != "thinking" || safe["answer"] != nil || safe["copy"] != nil || safe["svg"] != nil {
		t.Fatalf("unexpected properties: %v", safe)
	}
	if bad := safeProperties("character_reaction_shown", map[string]any{"reaction_trigger": "Sun"}); bad["reaction_trigger"] != nil {
		t.Fatalf("free text accepted: %v", bad)
	}
}

func TestVoiceFallbackEventKeepsOnlyBoundedFields(t *testing.T) {
	if !allowlisted("character_voice_fallback") {
		t.Fatal("character_voice_fallback dropped")
	}
	safe := safeProperties("character_voice_fallback", map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "speaker_key": "supporter", "reason_code": "voice_retired", "voice_config_version": "2026-11-13.1", "text": "private-text", "name": "private-text", "voice_id": "private-text"})
	raw, _ := json.Marshal(safe)
	if strings.Contains(string(raw), "private-text") || safe["reason_code"] != "voice_retired" || safe["speaker_key"] != "supporter" {
		t.Fatalf("unsafe or missing: %s", raw)
	}
	if bad := safeProperties("character_voice_fallback", map[string]any{"reason_code": "Nova said hello"}); bad["reason_code"] != nil {
		t.Fatalf("free-text reason kept: %v", bad)
	}
}
