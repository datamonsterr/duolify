package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSharedDemoCatalogueDeliveryAndPrivacy(t *testing.T) {
	var mu sync.Mutex
	var received []map[string]any
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer sink.Close()
	owner := "11111111-1111-4111-8111-111111111111"
	props := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "preview_kind": "shared_demo", "demo_version": "sky-v1", "surface": "course_brief", "control_id": "shared_demo", "topic": "private learner topic", "html": "private slide HTML"}
	for _, optOut := range []bool{false, true} {
		events, err := NewWithOptions("fixture", sink.URL, Options{OptOut: optOut, Consent: allowConsent})
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range []string{"course_preview_viewed", "ui_control_activated"} {
			if err = events.Capture(owner, event, props); err != nil {
				t.Fatal(err)
			}
			if err = events.Capture("not-a-uuid", event, props); err != nil {
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
		t.Fatal("delivery/optout", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") || event["distinct_id"] != owner {
			t.Fatal("privacy", event)
		}
		p := event["properties"].(map[string]any)
		if p["source"] != "api" || p["platform"] != "web" || p["app_version"] != "0.1.0" {
			t.Fatal("common properties", p)
		}
		if event["event"] == "course_preview_viewed" && (p["preview_kind"] != "shared_demo" || p["demo_version"] != "sky-v1") {
			t.Fatal("missing demo catalogue properties", p)
		}
		if event["event"] == "ui_control_activated" && (p["control_id"] != "shared_demo" || p["surface"] != "course_brief") {
			t.Fatal("missing control properties", p)
		}
	}
}
