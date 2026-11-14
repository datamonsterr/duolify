package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestNarrationEventsReachPostHogWithoutPrivateText(t *testing.T) {
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
	const learner = "a1111111-1111-4111-8111-111111111111"
	events, err := NewWithOptions("fixture-only", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "lesson_id": "lesson-1", "slide_id": "sky-1",
		"narration_text": "private spoken words", "text": "private spoken words", "audio_url": "/private", "html": "private",
		"characters": 40, "voice_id": "mock-default-voice", "model": "eleven_flash_v2_5", "latency_ms": 12, "error_code": "invalid_alignment", "duration_ms": 2000,
		"muted": true, "speed": 1.25, "captions": true}
	for _, event := range []string{"narration_generated", "narration_failed", "narration_cache_hit", "narration_played", "narration_muted", "narration_speed_changed", "captions_toggled"} {
		if err := events.Capture(learner, event, base); err != nil {
			t.Fatal(err)
		}
	}
	generation := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "feature": "narration", "provider": "elevenlabs", "model": "eleven_flash_v2_5", "narration_characters": 40, "latency_ms": 12, "succeeded": true, "text": "private spoken words"}
	if err := events.Capture(learner, "$ai_generation", generation); err != nil {
		t.Fatal(err)
	}
	if err := events.Capture("not-a-uuid", "narration_generated", base); err != nil {
		t.Fatal(err)
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 8 {
		t.Fatalf("want 8 delivered events, got %d: %v", len(received), received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") {
			t.Fatal("learner text leaked", string(raw))
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != learner || p["source"] != "api" || p["platform"] != "web" || p["app_version"] != "0.1.0" {
			t.Fatal("common properties missing", event)
		}
		switch event["event"] {
		case "narration_generated":
			if p["characters"] != float64(40) || p["voice_id"] != "mock-default-voice" || p["model"] != "eleven_flash_v2_5" || p["latency_ms"] != float64(12) || p["error_code"] != nil {
				t.Fatal("generated schema", p)
			}
		case "narration_failed":
			if p["error_code"] != "invalid_alignment" || p["lesson_id"] != "lesson-1" || p["voice_id"] != nil {
				t.Fatal("failed schema", p)
			}
		case "narration_played":
			if p["slide_id"] != "sky-1" || p["duration_ms"] != float64(2000) || p["characters"] != nil {
				t.Fatal("played schema", p)
			}
		case "narration_muted":
			if p["slide_id"] != "sky-1" || p["muted"] != true || p["narration_text"] != nil {
				t.Fatal("muted schema", p)
			}
		case "narration_speed_changed":
			if p["slide_id"] != "sky-1" || p["speed"] != float64(1.25) || p["narration_text"] != nil {
				t.Fatal("speed schema", p)
			}
		case "captions_toggled":
			if p["slide_id"] != "sky-1" || p["captions"] != true || p["narration_text"] != nil {
				t.Fatal("captions schema", p)
			}
		case "$ai_generation":
			if p["feature"] != "narration" || p["narration_characters"] != float64(40) || p["$ai_provider"] != "elevenlabs" {
				t.Fatal("ai generation schema", p)
			}
		}
	}
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = opted.Capture(learner, "narration_played", base)
	_ = opted.Close()
	if len(received) != 8 {
		t.Fatal("opt-out emitted")
	}
}
