package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestMissingConsentResolverSuppressesEveryUngatedEmitter(t *testing.T) {
	var delivered atomic.Int64
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batch []json.RawMessage `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		delivered.Add(int64(len(body.Batch)))
		w.WriteHeader(200)
	}))
	defer sink.Close()
	events, err := New("fixture", sink.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"lesson_generated", "course_generation_failed", "lesson_generation_requested", "retry_variant_queued", "retry_variant_served", "retry_variant_answered", "retry_variant_generation_failed", "daily_credits_expired", "daily_credits_granted", "credits_reserved", "credits_settled", "credits_released", "credits_insufficient", "credits_exhausted"} {
		if err := events.Capture("11111111-1111-4111-8111-111111111111", event, map[string]any{"source": "api", "platform": "web", "app_version": "test"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	if delivered.Load() != 0 {
		t.Fatalf("missing resolver delivered %d learner events", delivered.Load())
	}
}
