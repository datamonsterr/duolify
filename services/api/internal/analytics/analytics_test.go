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

func TestFundedSubscriptionEventsReachPostHogWithoutPrivateData(t *testing.T) {
	for _, interval := range []string{"monthly", "yearly"} {
		t.Run(interval, func(t *testing.T) {
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
			props := map[string]any{"source": "api", "platform": "web", "app_version": "development", "plan_id": "configured-plan", "interval": interval, "channel": "stripe", "currency": "USD", "price_table_version": "configured-version", "entitlement_version": "configured-version", "amount": 600, "amount_minor": 999, "period_key": "stripe:test:sub_fixture:1800000000:1802592000:0", "billing_period_start": "2027-01-15T08:00:00Z", "billing_period_end": "2027-02-15T08:00:00Z", "note": "private learner text", "card": "private payment details", "email": "private@example.test"}
			if interval == "yearly" {
				props["period_key"] = "stripe:test:sub_fixture:1800000000:1831536000:11"
			}
			id := "11111111-1111-4111-8111-111111111111"
			events, err := NewWithOptions("fixture-only", server.URL, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range []string{"plan_purchased", "subscription_grant_issued", "entitlement_changed", "subscription_renewed", "subscription_cancelled", "subscription_payment_failed"} {
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
			if len(received) != 6 {
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
				if !ok || p["source"] != "api" || p["platform"] != "web" || p["plan_id"] != "configured-plan" || p["app_version"] != "development" || p["period_key"] != props["period_key"] {
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
			if len(received) != 6 {
				t.Fatal("opt-out sent analytics")
			}
		})
	}
}

func TestEventAllowlist(t *testing.T) {
	for _, name := range []string{
		"course_outline_generated", "course_enrolled", "course_generation_failed", "ui_recovery_selected",
		"learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed",
		"backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed",
		"$ai_generation", "ui_control_activated", "ui_navigation_selected",
		"paywall_viewed", "credits_upsell_shown", "pricing_version_exposed", "ui_purchase_option_selected", "ui_checkout_requested",
		"pricing_version_created", "pricing_version_approved", "pricing_version_rejected",
		"pricing_version_activated", "pricing_version_rolled_back", "price_table_activated",
		"character_rendered", "character_render_fallback",
		"daily_credits_granted", "daily_credits_expired",
		"checkout_started", "credits_purchased", "credits_purchase_failed",
		"quick_note_created", "ui_sheet_opened",
		"bookmark_added", "bookmark_removed", "bookmark_opened",
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

func TestOutlineControlDeliveryAndPrivacy(t *testing.T) {
	var received []map[string]any
	var mu sync.Mutex
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
	events, err := New("fixture-only", sink.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, control := range []string{"outline_move", "outline_rename", "outline_remove", "outline_add", "outline_undo"} {
		if err = events.Capture("a1111111-1111-4111-8111-111111111111", "ui_control_activated", map[string]any{"surface": "outline", "control_id": control, "source": "api", "platform": "web", "app_version": "0.1.0", "title": "private title", "goal": "private goal", "history": "private history"}); err != nil {
			t.Fatal(err)
		}
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 5 {
		t.Fatal("missing outline events", received)
	}
	for _, event := range received {
		props := event["properties"].(map[string]any)
		if props["surface"] != "outline" || props["source"] != "api" || props["platform"] != "web" || props["app_version"] != "0.1.0" || event["distinct_id"] != "a1111111-1111-4111-8111-111111111111" {
			t.Fatal("missing common properties", event)
		}
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") {
			t.Fatal("private outline text leaked", string(raw))
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
		if !succeeded {
			input["error_code"] = "rate_limited"
		}
		got := safeProperties("$ai_generation", input)
		for _, key := range []string{"task", "pricing_version", "attempt", "provider", "model", "input_tokens", "output_tokens", "latency_ms", "fallback_used", "succeeded", "usage_available"} {
			if !reflect.DeepEqual(got[key], input[key]) {
				t.Errorf("%s: got %v, want %v", key, got[key], input[key])
			}
		}
		if !succeeded && got["error_code"] != "rate_limited" {
			t.Errorf("error_code: got %v, want rate_limited", got["error_code"])
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

func TestAnnualGrantMonthKeyPrivacy(t *testing.T) {
	for _, key := range []string{"stripe:test:sub_fixture:1800000000:1831536000:1", "stripe:test:sub_fixture:1800000000:1831536000:11"} {
		p := safeProperties("subscription_grant_issued", map[string]any{"period_key": key, "note": "private learner text"})
		if p["period_key"] != key {
			t.Fatalf("lost annual business key: %v", p)
		}
		if p["note"] != nil {
			t.Fatal("learner text leaked")
		}
	}
	for _, key := range []string{"stripe:test:sub_fixture:1800000000:1831536000:12", "stripe:test:private learner text:1800000000:1831536000:1"} {
		if p := safeProperties("subscription_grant_issued", map[string]any{"period_key": key}); p["period_key"] != nil {
			t.Fatal("invalid business key leaked", p)
		}
	}
}

func TestPreviewEventsReachPostHogWithoutPrivateData(t *testing.T) {
	for _, interval := range []string{"preview"} {
		t.Run(interval, func(t *testing.T) {
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
			props := map[string]any{"source": "api", "platform": "web", "app_version": "development", "stage": "preview", "error_code": "generation_unavailable", "preview_id": "11111111-1111-4111-8111-111111111111", "topic_length": 20, "has_profile": true, "duration_ms": 42, "topic": "private astronomy", "brief": "private answer", "html": "private slide"}
			id := "11111111-1111-4111-8111-111111111111"
			events, err := NewWithOptions("fixture-only", server.URL, Options{})
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range []string{"course_preview_requested", "course_preview_viewed", "course_generation_failed"} {
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
				if !ok || p["source"] != "api" || p["platform"] != "web" || p["stage"] != "preview" || p["app_version"] != "development" {
					t.Fatal("lost common/configured properties", event)
				}
			}
			opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
			if err != nil {
				t.Fatal(err)
			}
			if err = opted.Capture(id, "course_preview_viewed", props); err != nil {
				t.Fatal(err)
			}
			if err = opted.Close(); err != nil {
				t.Fatal(err)
			}
			if len(received) != 3 {
				t.Fatal("opt-out sent analytics")
			}
		})
	}
}

func TestEvalRunCompletedDeliveryAndMetadata(t *testing.T) {
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

	systemUUID := "00000000-0000-4000-8000-000000000011"
	props := map[string]any{
		"source":           "ci",
		"platform":         "web",
		"app_version":      "development",
		"task":             "course_preview",
		"score":            1.0,
		"threshold":        1.0,
		"passed":           true,
		"dataset_revision": "0123456789abcdef0123456789abcdef01234567",
		"dataset_source":   "mirror",
		"provider":         "live",
		"status":           "passed",
		"budget_usd":       5.0,
		"learner_text":     "private prompt content",
		"prompt":           "private prompt",
	}

	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err = events.Capture(systemUUID, "eval_run_completed", props); err != nil {
		t.Fatal(err)
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %v", received)
	}

	event := received[0]
	if event["distinct_id"] != systemUUID {
		t.Fatalf("expected distinct_id %s, got %v", systemUUID, event["distinct_id"])
	}
	p := event["properties"].(map[string]any)
	if p["task"] != "course_preview" || p["score"] != 1.0 || p["threshold"] != 1.0 || p["passed"] != true {
		t.Fatalf("eval properties mismatch: %v", p)
	}
	if p["dataset_revision"] != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("dataset_revision mismatch: %v", p["dataset_revision"])
	}
	if p["dataset_source"] != "mirror" {
		t.Fatalf("dataset_source mismatch: %v", p["dataset_source"])
	}
	if p["provider"] != "live" {
		t.Fatalf("provider mismatch: %v", p["provider"])
	}
	if p["status"] != "passed" {
		t.Fatalf("status mismatch: %v", p["status"])
	}
	if p["budget_usd"] != 5.0 {
		t.Fatalf("budget_usd mismatch: %v", p["budget_usd"])
	}
	if p["source"] != "ci" || p["platform"] != "web" || p["app_version"] != "development" {
		t.Fatalf("common properties mismatch: %v", p)
	}

	// Verify no private text leaked
	raw, _ := json.Marshal(event)
	if strings.Contains(string(raw), "private") {
		t.Fatalf("private text leaked in eval_run_completed: %s", string(raw))
	}

	// Test opt-out
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = opted.Capture(systemUUID, "eval_run_completed", props); err != nil {
		t.Fatal(err)
	}
	if err = opted.Close(); err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 {
		t.Fatalf("opt-out sent analytics: %v", received)
	}
}

func TestAICostBudgetAlertReachesPostHogWithoutPrivateData(t *testing.T) {
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

	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	systemID := "00000000-0000-4000-8000-000000000011"
	props := map[string]any{
		"source":        "api",
		"platform":      "web",
		"app_version":   "development",
		"scope":         "platform_daily_80",
		"actual_usd":    40.50,
		"budget_usd":    50.00,
		"threshold":     0.80,
		"status":        "firing",
		"learner_text":  "forbidden prompt text",
		"user_question": "forbidden learner question",
	}

	if err := events.Capture(systemID, "ai_cost_budget_alert", props); err != nil {
		t.Fatal(err)
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 event, got %d", len(received))
	}
	ev := received[0]
	if ev["event"] != "ai_cost_budget_alert" {
		t.Fatalf("expected ai_cost_budget_alert, got %v", ev["event"])
	}
	if ev["distinct_id"] != systemID {
		t.Fatalf("expected system ID %s, got %v", systemID, ev["distinct_id"])
	}
	p := ev["properties"].(map[string]any)
	if p["scope"] != "platform_daily_80" || p["actual_usd"] != 40.50 || p["budget_usd"] != 50.00 {
		t.Fatalf("missing or wrong properties: %v", p)
	}
	if p["learner_text"] != nil || p["user_question"] != nil {
		t.Fatalf("leaked private properties: %v", p)
	}
}

func TestQuickNoteAndSheetEventsReachPostHogWithoutPrivateData(t *testing.T) {
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

	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	learnerID := "22222222-2222-4222-8222-222222222222"

	// 1. Verify valid sources for quick_note_created: slide, lesson, chat
	for _, src := range []string{"slide", "lesson", "chat"} {
		props := map[string]any{
			"source":      src,
			"length":      42,
			"platform":    "web",
			"app_version": "0.1.0",
			"text":        "private note text that must never be sent",
			"raw_text":    "super secret raw text",
			"note_source": "slide",
		}
		if err := events.Capture(learnerID, "quick_note_created", props); err != nil {
			t.Fatal(err)
		}
	}

	// 2. Verify invalid sources are not accepted (must only be slide|lesson|chat)
	for _, invalidSrc := range []string{"api", "ci", "client", "desktop", "unknown"} {
		props := map[string]any{
			"source":      invalidSrc,
			"length":      10,
			"platform":    "desktop",
			"app_version": "0.1.0",
			"note_text":   "more private text",
		}
		if err := events.Capture(learnerID, "quick_note_created", props); err != nil {
			t.Fatal(err)
		}
	}

	// 3. Verify ui_sheet_opened
	sheetProps := map[string]any{
		"source":       "api",
		"sheet_id":     "quick_note",
		"platform":     "desktop",
		"app_version":  "0.1.0",
		"private_data": "secret content",
	}
	if err := events.Capture(learnerID, "ui_sheet_opened", sheetProps); err != nil {
		t.Fatal(err)
	}

	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	// 3 valid quick_note_created + 1 valid ui_sheet_opened = 4 events delivered
	// The 5 invalid-source quick_note_created events are dropped because their source is not slide|lesson|chat
	if len(received) != 4 {
		t.Fatalf("expected 4 events, got %d", len(received))
	}

	for i, ev := range received {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "secret") {
			t.Fatalf("private content leaked in event %d: %s", i, string(raw))
		}
		p, ok := ev["properties"].(map[string]any)
		if !ok {
			t.Fatalf("missing properties map in event %d", i)
		}
		// Confirm note text / note_source keys never present
		for _, forbidden := range []string{"text", "raw_text", "note_source", "note_text", "private_data"} {
			if p[forbidden] != nil {
				t.Fatalf("forbidden key %q found in properties: %v", forbidden, p)
			}
		}

		switch ev["event"] {
		case "quick_note_created":
			expectedSrc := []string{"slide", "lesson", "chat"}[i]
			if p["source"] != expectedSrc {
				t.Fatalf("expected source %q, got %v", expectedSrc, p["source"])
			}
			if p["length"] != float64(42) && p["length"] != 42 {
				t.Fatalf("expected length 42, got %v", p["length"])
			}
		case "ui_sheet_opened":
			if p["sheet_id"] != "quick_note" {
				t.Fatalf("expected sheet_id quick_note, got %v", p["sheet_id"])
			}
		}
	}

	// 4. Test opt-out suppresses delivery
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := opted.Capture(learnerID, "quick_note_created", map[string]any{"source": "slide", "length": 5}); err != nil {
		t.Fatal(err)
	}
	if err := opted.Capture(learnerID, "ui_sheet_opened", map[string]any{"sheet_id": "quick_note"}); err != nil {
		t.Fatal(err)
	}
	if err := opted.Close(); err != nil {
		t.Fatal(err)
	}

	if len(received) != 4 {
		t.Fatalf("opt-out sent analytics, received %d events", len(received))
	}
}

func TestQuickNoteEnrichmentEventsReachPostHogWithoutPrivateData(t *testing.T) {
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

	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}

	learnerID := "11111111-1111-4111-8111-111111111111"
	noteID := "22222222-2222-4222-8222-222222222222"

	// 1. Capture quick_note_enriched with forbidden private fields
	err = events.Capture(learnerID, "quick_note_enriched", map[string]any{
		"source":      "api",
		"platform":    "web",
		"app_version": "0.1.0",
		"note_id":     noteID,
		"raw_text":    "private learner words",
		"summary":     "private AI summary",
		"tags":        []string{"private"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Capture quick_note_enrichment_failed
	err = events.Capture(learnerID, "quick_note_enrichment_failed", map[string]any{
		"source":      "api",
		"platform":    "web",
		"app_version": "0.1.0",
		"note_id":     noteID,
		"reason":      "timeout",
		"error_code":  "deadline_exceeded",
		"raw_text":    "private learner words",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}

	for _, ev := range received {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "private") {
			t.Fatalf("private data leaked into analytics: %s", string(raw))
		}
		props, ok := ev["properties"].(map[string]any)
		if !ok {
			t.Fatal("missing properties map")
		}
		if props["note_id"] != noteID {
			t.Fatalf("expected note_id %s, got %v", noteID, props["note_id"])
		}
	}
}

func TestCharacterRenderAnalyticsDeliveryAndPrivacy(t *testing.T) {
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

	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}

	learnerID := "11111111-1111-4111-8111-111111111111"
	renderProps := map[string]any{
		"source":            "api",
		"platform":          "web",
		"app_version":       "development",
		"speaker_key":       "guide",
		"character_version": "1",
		"rig_version":       "original-v1",
		"expression":        "happy",
		"offline":           false,
		"character_text":    "private speech dialogue",
		"svg_content":       "<svg>private</svg>",
		"name":              "private name",
	}

	if err := events.Capture(learnerID, "character_rendered", renderProps); err != nil {
		t.Fatal(err)
	}

	fallbackProps := map[string]any{
		"source":         "api",
		"platform":       "web",
		"app_version":    "development",
		"reason_code":    "missing_rig",
		"offline":        true,
		"character_text": "private speech dialogue",
	}

	if err := events.Capture(learnerID, "character_render_fallback", fallbackProps); err != nil {
		t.Fatal(err)
	}

	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}

	for _, ev := range received {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "private") {
			t.Fatalf("private content leaked in character event: %s", string(raw))
		}
		p := ev["properties"].(map[string]any)
		if p["character_text"] != nil || p["svg_content"] != nil || p["name"] != nil {
			t.Fatalf("unsafe properties leaked: %v", p)
		}
	}
}

func TestBookmarkEventsReachPostHogWithoutPrivateData(t *testing.T) {
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

	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	learnerID := "22222222-2222-4222-8222-222222222222"
	courseID := "33333333-3333-4333-8333-333333333333"
	lessonID := "44444444-4444-4444-8444-444444444444"

	// 1. bookmark_added
	err = events.Capture(learnerID, "bookmark_added", map[string]any{
		"target_type":  "slide",
		"course_id":    courseID,
		"lesson_id":    lessonID,
		"slide_id":     1,
		"source":       "api",
		"platform":     "web",
		"app_version":  "0.1.0",
		"private_data": "secret content that must not leak",
		"html":         "<script>bad()</script>",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. bookmark_removed
	err = events.Capture(learnerID, "bookmark_removed", map[string]any{
		"target_type": "lesson",
		"course_id":   courseID,
		"lesson_id":   lessonID,
		"source":      "api",
		"platform":    "desktop",
		"app_version": "0.1.0",
		"note_text":   "must be excluded",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. bookmark_opened
	err = events.Capture(learnerID, "bookmark_opened", map[string]any{
		"target_type": "slide",
		"course_id":   courseID,
		"lesson_id":   lessonID,
		"slide_id":    2,
		"source":      "api",
		"platform":    "web",
		"app_version": "0.1.0",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 3 {
		t.Fatalf("expected 3 events, got %d", len(received))
	}

	for i, ev := range received {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "private") || strings.Contains(string(raw), "script") {
			t.Fatalf("private data leaked in event %d: %s", i, string(raw))
		}
		p, ok := ev["properties"].(map[string]any)
		if !ok {
			t.Fatalf("missing properties in event %d", i)
		}
		for _, forbidden := range []string{"private_data", "html", "note_text"} {
			if p[forbidden] != nil {
				t.Fatalf("forbidden key %q found in event %d", forbidden, i)
			}
		}
	}

	// Opt-out test
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := opted.Capture(learnerID, "bookmark_added", map[string]any{"target_type": "slide"}); err != nil {
		t.Fatal(err)
	}
	if err := opted.Close(); err != nil {
		t.Fatal(err)
	}
	if len(received) != 3 {
		t.Fatalf("opt-out sent event, received %d", len(received))
	}
}

func TestPlayerEventsReachPostHogWithClosedProjection(t *testing.T) {
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
	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}
	props := map[string]any{"lesson_id": "lesson-1", "course_id": "course-1", "slide_id": "slide-1", "error_code": "document_unavailable", "source": "api", "platform": "web", "app_version": "0.1.0", "html": "private slide", "narration_text": "private narration", "answer_key": "private answer", "message": "private learner text"}
	props["index"], props["watch_ms"], props["offline"] = 0, 1250, true
	for _, event := range []string{"lesson_started", "lesson_player_error", "slide_viewed", "slide_replayed", "slide_paused"} {
		if err := events.Capture("a1111111-1111-4111-8111-111111111111", event, props); err != nil {
			t.Fatal(err)
		}
	}
	_ = events.Capture("invalid-identity", "lesson_started", props)
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 5 {
		t.Fatal("missing/invalid events", received)
	}
	for _, event := range received {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") {
			t.Fatal("content leaked", string(raw))
		}
		p := event["properties"].(map[string]any)
		if event["distinct_id"] != "a1111111-1111-4111-8111-111111111111" || p["lesson_id"] != "lesson-1" || p["source"] != "api" || p["platform"] != "web" || p["app_version"] != "0.1.0" {
			t.Fatal(event)
		}
		if event["event"] == "lesson_started" && (p["slide_id"] != nil || p["error_code"] != nil) {
			t.Fatal("start schema expanded", event)
		}
		if event["event"] == "lesson_player_error" && (p["course_id"] != nil || p["error_code"] != "document_unavailable") {
			t.Fatal("error schema mismatch", event)
		}
		if event["event"] == "slide_viewed" && (p["watch_ms"] != float64(1250) || p["offline"] != true || p["index"] != float64(0)) {
			t.Fatal("view schema mismatch", event)
		}
	}
	opted, err := NewWithOptions("fixture-only", server.URL, Options{OptOut: true})
	if err != nil {
		t.Fatal(err)
	}
	_ = opted.Capture("a1111111-1111-4111-8111-111111111111", "lesson_started", props)
	for _, event := range []string{"slide_viewed", "slide_replayed", "slide_paused", "lesson_player_error"} {
		_ = opted.Capture("a1111111-1111-4111-8111-111111111111", event, props)
	}
	_ = opted.Close()
	if len(received) != 5 {
		t.Fatal("opt-out emitted")
	}
}

func TestQuickNoteReviewAndDeleteAnalyticsReachPostHogWithoutPrivateText(t *testing.T) {
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

	events, err := NewWithOptions("fixture-only", server.URL, Options{})
	if err != nil {
		t.Fatal(err)
	}

	learnerID := "11111111-1111-4111-8111-111111111111"
	noteID := "33333333-3333-4333-8333-333333333333"

	// 1. quick_note_reviewed
	err = events.Capture(learnerID, "quick_note_reviewed", map[string]any{
		"source":       "api",
		"platform":     "web",
		"app_version":  "0.1.0",
		"note_id":      noteID,
		"raw_text":     "private secret note text",
		"search_query": "secret query",
		"content":      "forbidden",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. quick_note_deleted
	err = events.Capture(learnerID, "quick_note_deleted", map[string]any{
		"source":      "api",
		"platform":    "desktop",
		"app_version": "0.1.0",
		"note_id":     noteID,
		"raw_text":    "another private note",
		"query":       "private query",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := events.Close(); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("expected 2 events, got %d", len(received))
	}

	for _, ev := range received {
		raw, _ := json.Marshal(ev)
		if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "forbidden") {
			t.Fatalf("private data leaked into analytics: %s", string(raw))
		}
		props, ok := ev["properties"].(map[string]any)
		if !ok {
			t.Fatal("missing properties map")
		}
		if props["note_id"] != noteID {
			t.Fatalf("expected note_id %s, got %v", noteID, props["note_id"])
		}
		for _, forbidden := range []string{"raw_text", "search_query", "content", "query"} {
			if props[forbidden] != nil {
				t.Fatalf("forbidden key %q found in event", forbidden)
			}
		}
	}
}
