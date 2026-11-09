package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/credits/creditstest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
)

func TestStripePackHTTPAnalyticsAndPrivacy(t *testing.T) {
	auth, token, db := signedInDatabase(t)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	owner := claims.Subject
	if _, err = db.Pool.Exec(t.Context(), "INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1,false) ON CONFLICT(learner_id) DO UPDATE SET analytics_opt_out=false", owner); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var captured []map[string]any
	posthog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Error(err)
		}
		mu.Lock()
		captured = append(captured, batch.Batch...)
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer posthog.Close()
	events, err := analytics.NewWithOptions("fixture-posthog-key", posthog.URL, analytics.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = events.Close() }()
	catalogue, err := pricing.Load(t.Context(), db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	stripe := creditstest.NewStripe()
	providerServer := httptest.NewServer(stripe.Handler())
	defer providerServer.Close()
	service := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, events)
	if err = service.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: creditstest.RemoteStripe{Endpoint: providerServer.URL, Client: providerServer.Client()}, WebhookSecret: creditstest.WebhookSecret}); err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Credits: service}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer api.Close()
	call := func(method, path, authorization, signature string, raw []byte) (int, []byte) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, api.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		if authorization != "" {
			req.Header.Set("Authorization", "Bearer "+authorization)
		}
		if signature != "" {
			req.Header.Set("Stripe-Signature", signature)
		}
		response, err := api.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body
	}
	cfg := catalogue.Config()
	pack := cfg.Packs[0]
	create := func() credits.CheckoutOrder {
		t.Helper()
		raw, _ := json.Marshal(credits.CheckoutInput{RequestID: uuid.NewString(), SKU: pack.SKU, PricingVersion: cfg.Version, Platform: "desktop", ReturnTo: "/notes"})
		code, body := call("POST", "/v1/credits/checkouts", token, "", raw)
		if code != 200 {
			t.Fatalf("create %d %s", code, body)
		}
		var order credits.CheckoutOrder
		if err := json.Unmarshal(body, &order); err != nil {
			t.Fatal(err)
		}
		return order
	}
	notify := func(order credits.CheckoutOrder, state, kind string) {
		t.Helper()
		raw, signature, err := stripe.Notification("cs_test_"+order.ID, state, kind)
		if err != nil {
			t.Fatal(err)
		}
		code, body := call("POST", "/v1/payments/stripe/webhook", "", signature, raw)
		if code != 200 {
			t.Fatalf("public signature route %d %s", code, body)
		}
		if err = service.ProcessPayments(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if code, _ := call("POST", "/v1/credits/checkouts", "", "", nil); code != 401 {
		t.Fatalf("unauthenticated checkout %d", code)
	}
	first := create()
	if code, _ := call("POST", "/v1/payments/stripe/webhook", "", "", []byte(`{"id":"forged"}`)); code != 400 {
		t.Fatalf("forged webhook %d", code)
	}
	notify(first, "unpaid", "checkout.session.completed")
	code, body := call("GET", "/v1/credits/checkouts/"+first.ID+"?payment=success", token, "", nil)
	if code != 200 || !strings.Contains(string(body), `"state":"pending"`) {
		t.Fatalf("forged callback %d %s", code, body)
	}
	notify(first, "paid", "checkout.session.async_payment_succeeded")
	notify(first, "paid", "checkout.session.completed")
	notify(first, "paid", "checkout.session.completed")
	second := create()
	notify(second, "failed", "checkout.session.async_payment_failed")
	if err = service.DispatchOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Global and per-learner opt-outs are independent of fulfillment.
	if _, err = db.Pool.Exec(t.Context(), "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1", owner); err != nil {
		t.Fatal(err)
	}
	third := create()
	notify(third, "paid", "checkout.session.completed")
	if err = service.DispatchOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = service.DispatchOutbox(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = events.Close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	counts := map[string]int{}
	for _, event := range captured {
		name, _ := event["event"].(string)
		counts[name]++
		props, _ := event["properties"].(map[string]any)
		if event["distinct_id"] != owner || props["source"] != "api" || props["platform"] != "desktop" || props["app_version"] == nil || props["price_table_version"] != cfg.Version {
			t.Fatalf("bad common payment properties: %+v", event)
		}
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), "private") || strings.Contains(string(raw), "http@example") {
			t.Fatal("private data reached PostHog")
		}
	}
	if counts["checkout_started"] != 2 || counts["credits_purchased"] != 1 || counts["credits_purchase_failed"] != 1 || len(captured) != 4 {
		t.Fatalf("purchase funnel delivery: %v", counts)
	}
	code, body = call("GET", "/v1/me/data-export", token, "", nil)
	if code != 200 {
		t.Fatalf("export %d %s", code, body)
	}
	var export struct {
		Tables map[string][]json.RawMessage `json:"tables"`
	}
	if err = json.Unmarshal(body, &export); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"credit_payment_sessions", "credit_payment_inbox", "credit_payment_results", "credit_payment_payloads"} {
		if len(export.Tables[table]) == 0 {
			t.Fatalf("missing payment export %s", table)
		}
	}
	code, body = call("POST", "/v1/me/deletion-request", token, "", []byte(`{"confirmation":"DELETE"}`))
	if code != 200 {
		t.Fatalf("delete %d %s", code, body)
	}
	notify(first, "paid", "checkout.session.expired")
	var n int
	if err = db.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_payment_results WHERE state='paid'").Scan(&n); err != nil || n != 2 {
		t.Fatalf("delete replay duplicated payment: %d %v", n, err)
	}
	if code, _ := call("GET", "/v1/credits/checkouts/"+first.ID, token, "", nil); code != 401 {
		t.Fatalf("deleted account access %d", code)
	}
}
