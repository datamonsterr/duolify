package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestImmersiveEventsKeepOnlyClosedProperties(t *testing.T) {
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
	const learner = "a1111111-1111-4111-8111-111111111111"
	events, err := NewWithOptions("fixture-only", server.URL, Options{Consent: allowConsent})
	if err != nil {
		t.Fatal(err)
	}
	base := map[string]any{"source": "api", "platform": "android", "app_version": "0.1.0",
		"course_id": "c1", "lesson_id": "l1", "slide_index": 2, "trigger": "rotation", "seconds_in_mode": "10_30",
		"control": "replay_slide", "via": "double_tap", "caption_text": "private words", "free_text": "private"}
	for _, e := range []string{"immersive_mode_entered", "immersive_mode_exited", "immersive_control_used"} {
		if err := events.Capture(learner, e, base); err != nil {
			t.Fatal(err)
		}
	}
	bad := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "control": "not_a_control", "via": "telepathy"}
	if err := events.Capture(learner, "immersive_control_used", bad); err != nil {
		t.Fatal(err)
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 4 {
		t.Fatalf("want 4 events, got %d", len(received))
	}
	for _, ev := range received {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "private") {
			t.Fatal("free text leaked", string(raw))
		}
		p := ev["properties"].(map[string]any)
		switch ev["event"] {
		case "immersive_mode_entered":
			if p["trigger"] != "rotation" || p["slide_index"] != float64(2) || p["seconds_in_mode"] != nil || p["control"] != nil {
				t.Fatal("entered schema", p)
			}
		case "immersive_mode_exited":
			if p["seconds_in_mode"] != "10_30" || p["course_id"] != "c1" {
				t.Fatal("exited schema", p)
			}
		case "immersive_control_used":
			if p["control"] == "not_a_control" || p["via"] == "telepathy" {
				t.Fatal("open enum accepted", p)
			}
			if p["control"] == "replay_slide" && (p["via"] != "double_tap" || p["lesson_id"] != nil) {
				t.Fatal("control schema", p)
			}
		}
	}
}

func TestAnchoredNoteEventKeepsOnlyClosedProperties(t *testing.T) {
	got := safeProperties("lesson_anchored_note_created", map[string]any{
		"source": "voice", "course_id": "c1", "lesson_id": "l1", "slide_index": 3, "has_region": true,
		"platform": "web", "app_version": "0.1.0", "raw_text": "private words", "rect": "0.1,0.1", "note_id": "n1",
	})
	want := map[string]any{"source": "voice", "course_id": "c1", "lesson_id": "l1", "slide_index": 3, "has_region": true, "platform": "web", "app_version": "0.1.0"}
	if len(got) != len(want) {
		t.Fatalf("unexpected properties: %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %v, want %v", k, got[k], v)
		}
	}
	if bad := safeProperties("lesson_anchored_note_created", map[string]any{"source": "telepathy"}); bad["source"] != nil {
		t.Fatalf("unknown source kept: %v", bad)
	}
}
