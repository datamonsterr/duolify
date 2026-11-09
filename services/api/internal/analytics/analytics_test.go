package analytics

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestMonthlySubscriptionEventsReachPostHogWithoutPrivateData(t *testing.T) {
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
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	props := map[string]any{"source": "api", "platform": "web", "app_version": "development", "plan_id": "configured-plan", "interval": "monthly", "channel": "stripe", "currency": "USD", "price_table_version": "configured-version", "entitlement_version": "configured-version", "amount": 600, "amount_minor": 999, "period_key": "stripe:test:sub_fixture:1800000000:1802592000:0", "billing_period_start": "2027-01-15T08:00:00Z", "billing_period_end": "2027-02-15T08:00:00Z", "note": "private learner text", "card": "private payment details", "email": "private@example.test"}
	id := "11111111-1111-4111-8111-111111111111"
	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"plan_purchased", "subscription_grant_issued", "entitlement_changed"} {
		if !allowlisted(event) {
			t.Fatal("event missing from catalogue", event)
		}
		if err = events.Capture(id, event, props); err != nil {
			t.Fatal(err)
		}
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 3 {
		t.Fatalf("PostHog delivery %v", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") {
			t.Fatal("private content leaked", string(raw))
		}
		if event["distinct_id"] != id {
			t.Fatal("non-UUID identity", event)
		}
		p, ok := event["properties"].(map[string]any)
		if !ok || p["source"] != "api" || p["platform"] != "web" || p["plan_id"] != "configured-plan" || p["app_version"] != "development" {
			t.Fatal("lost common/configured properties", event)
		}
	}
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = opted.Capture(id, "plan_purchased", props); err != nil {
		t.Fatal(err)
	}
	if err = opted.Close(); err != nil {
		t.Fatal(err)
	}
	if len(received) != 3 {
		t.Fatal("opt-out sent analytics")
	}
}

func TestEventAllowlist(t *testing.T) {
	for _, name := range []string{
		"learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed",
		"backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed",
		"$ai_generation", "ui_control_activated",
		"paywall_viewed", "credits_upsell_shown", "pricing_version_exposed", "ui_purchase_option_selected", "ui_checkout_requested",
		"pricing_version_created", "pricing_version_approved", "pricing_version_rejected",
		"pricing_version_activated", "pricing_version_rolled_back", "price_table_activated",
		"daily_credits_granted", "daily_credits_expired",
		"checkout_started", "credits_purchased", "credits_purchase_failed",
	} {
		if !allowlisted(name) {
			t.Errorf("catalogue event %q rejected", name)
		}
	}
	for _, name := range []string{"private_text", "ui_appearance_changed", "unknown_event"} {
		if allowlisted(name) {
			t.Errorf("uncatalogued/API-owned event %q accepted", name)
		}
	}
}

func TestPaymentPropertiesExcludePrivateData(t *testing.T) {
	input := map[string]any{"source": "api", "platform": "desktop", "app_version": "0.1.0", "amount": 519, "currency": "EUR", "price": 7.23, "amount_minor": 723, "sku_id": "configured_pack", "channel": "stripe", "price_table_version": "configured", "reason": "failed", "email": "private@example.test", "raw_payload": "private payload", "draft": "private learner text", "payment_id": "pi_private", "url": "https://private.test", "error": "private error"}
	for _, name := range []string{"checkout_started", "credits_purchased", "credits_purchase_failed"} {
		got := safeProperties(name, input)
		for _, key := range []string{"source", "platform", "app_version", "amount", "currency", "price", "amount_minor", "sku_id", "channel", "price_table_version", "reason"} {
			if !reflect.DeepEqual(got[key], input[key]) {
				t.Errorf("%s lost %s", name, key)
			}
		}
		for _, key := range []string{"email", "raw_payload", "draft", "payment_id", "url", "error"} {
			if _, ok := got[key]; ok {
				t.Errorf("%s leaked %s", name, key)
			}
		}
	}
	input["reason"] = "private learner text"
	if safeProperties("credits_purchase_failed", input)["reason"] != nil {
		t.Fatal("private failure reason accepted")
	}
}

func TestPurchaseRecoveryPropertiesExcludeWorkAndURLs(t *testing.T) {
	input := map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "price_table_version": "version-a", "offer_id": "pack_try", "billing_period": "yearly", "balance": 12, "next_daily_grant_at": "2026-11-10T00:00:00+07:00", "draft": "private draft", "raw_note": "private note", "return_to": "/notes?secret=private", "payment": "success"}
	for _, event := range []string{"paywall_viewed", "credits_upsell_shown", "ui_purchase_option_selected", "ui_checkout_requested"} {
		got := safeProperties(event, input)
		for _, key := range []string{"price_table_version", "offer_id", "billing_period", "balance", "next_daily_grant_at"} {
			if !reflect.DeepEqual(got[key], input[key]) {
				t.Fatalf("%s missing %s", event, key)
			}
		}
		for _, key := range []string{"draft", "raw_note", "return_to", "payment"} {
			if _, exists := got[key]; exists {
				t.Fatalf("private %s in %s", key, event)
			}
		}
	}
}

func TestCourseBriefAttemptProperties(t *testing.T) {
	for _, succeeded := range []bool{false, true} {
		input := map[string]any{
			"source": "api", "platform": "web", "app_version": "0.1.0",
			"task": "course_brief_chat", "pricing_version": "usd-dev-course-brief-unapproved",
			"attempt": 2, "provider": "mock", "model": "haiku",
			"input_tokens": uint32(10), "output_tokens": uint32(5), "latency_ms": uint32(20),
			"fallback_used": true, "succeeded": succeeded, "usage_available": succeeded,
			"answer": "private answer", "brief": map[string]string{"topic": "private topic"},
		}
		got := safeProperties("$ai_generation", input)
		for _, key := range []string{"task", "pricing_version", "attempt", "provider", "model", "input_tokens", "output_tokens", "latency_ms", "fallback_used", "succeeded", "usage_available"} {
			if !reflect.DeepEqual(got[key], input[key]) {
				t.Errorf("%s: got %v, want %v", key, got[key], input[key])
			}
		}
		if got["$ai_is_error"] != !succeeded || got["feature"] != "course_brief_chat" {
			t.Errorf("incorrect canonical attempt properties: %v", got)
		}
		for _, key := range []string{"answer", "brief"} {
			if _, ok := got[key]; ok {
				t.Errorf("private property %s retained", key)
			}
		}
	}
}
