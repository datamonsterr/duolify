package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type fakeStripeService struct {
	mu     sync.Mutex
	prices map[string]pricing.StripePriceRecord
}

func newFakeStripe() *fakeStripeService {
	return &fakeStripeService{
		prices: map[string]pricing.StripePriceRecord{
			"price_try_valid": {
				ID:            "price_try_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   500,
				RecurringType: "one_time",
				Environment:   "test",
			},
			"price_try_wrong_amount": {
				ID:            "price_try_wrong_amount",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   400, // Expected 500
				RecurringType: "one_time",
				Environment:   "test",
			},
			"price_try_wrong_currency": {
				ID:            "price_try_wrong_currency",
				Active:        true,
				Currency:      "EUR", // Expected USD
				AmountMinor:   500,
				RecurringType: "one_time",
				Environment:   "test",
			},
			"price_learn_m_valid": {
				ID:            "price_learn_m_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   999,
				RecurringType: "recurring",
				Interval:      "month",
				IntervalCount: 1,
				Environment:   "test",
			},
			"price_learn_m_wrong_interval": {
				ID:            "price_learn_m_wrong_interval",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   999,
				RecurringType: "recurring",
				Interval:      "year", // Expected month
				IntervalCount: 1,
				Environment:   "test",
			},
			"price_inactive": {
				ID:            "price_inactive",
				Active:        false,
				Currency:      "USD",
				AmountMinor:   500,
				RecurringType: "one_time",
				Environment:   "test",
			},
		},
	}
}

func (f *fakeStripeService) VerifyStripePrice(_ context.Context, env, priceID string, expected pricing.Offer, interval string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.prices[priceID]
	if !ok {
		return pricing.ErrStripePriceNotFound
	}
	return p.Matches(env, expected, interval)
}

func (f *fakeStripeService) VerifyStoreMapping(_ context.Context, channel, productID string) error {
	return pricing.ValidateStoreProductID(channel, productID)
}

func TestPricingE12_07_RejectWrongStripePriceAndUneconomicCatalogueDraft(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_e12_07"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	connURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, connURL); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, connURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var captureMu sync.Mutex
	var captured []map[string]any
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch struct {
			Batch []map[string]any `json:"batch"`
		}
		if decodeErr := json.NewDecoder(r.Body).Decode(&batch); decodeErr != nil {
			t.Error(decodeErr)
		}
		captureMu.Lock()
		captured = append(captured, batch.Batch...)
		captureMu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, "test-only-signing-key-at-least-32-bytes", mail)
	if err != nil {
		t.Fatal(err)
	}
	login := func(email string) (string, string) {
		t.Helper()
		call := func(path, body string) *httptest.ResponseRecorder {
			r := httptest.NewRequestWithContext(ctx, http.MethodPost, path, strings.NewReader(body))
			w := httptest.NewRecorder()
			auth.ServeHTTP(w, r)
			return w
		}
		if w := call("/v1/auth/code", `{"email":"`+email+`","age_confirmed":true}`); w.Code != 202 {
			t.Fatal(w.Body)
		}
		w := call("/v1/auth/verify", `{"email":"`+email+`","age_confirmed":true,"code":"`+mail.Code(email)+`"}`)
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
		var tokens accounts.Tokens
		if err := json.Unmarshal(w.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		claims, err := auth.ParseAccess(tokens.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		return tokens.AccessToken, claims.Subject
	}
	author, authorID := login("e12-author@example.test")
	approver, approverID := login("e12-approver@example.test")
	for _, id := range []string{authorID, approverID} {
		if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", id); err != nil {
			t.Fatal(err)
		}
	}

	events, err := analytics.New("synthetic-project-key", sink.URL)
	if err != nil {
		t.Fatal(err)
	}

	fakeStripe := newFakeStripe()
	admin := pricing.NewAdmin(db.Pool, events, "")
	admin.SetVerifier(fakeStripe)

	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.New(httpapi.Dependencies{
		Accounts:     auth,
		Pricing:      catalogue,
		PricingAdmin: admin,
		Quotes:       pricing.NewQuotes(db.Pool),
		Postgres:     db.Ping,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	server := httptest.NewServer(handler)
	defer server.Close()

	request := func(method, path, token string, body []byte) *httptest.ResponseRecorder {
		r, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		w.Code = resp.StatusCode
		_, _ = w.Body.Write(bodyBytes)
		return w
	}

	var current map[string]any
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-launch-2026-11-08'").Scan(&current); err != nil {
		t.Fatal(err)
	}

	// Supply every SKU to the provider fake; preserve the deliberately bad IDs.
	mappingFor := map[string]any{}
	addOffer := func(offer map[string]any, interval string) {
		sku := offer["sku"].(string)
		id := "price_fixture_" + sku
		recurring := "one_time"
		if interval != "one_time" {
			recurring = "recurring"
		}
		fakeStripe.prices[id] = pricing.StripePriceRecord{ID: id, Active: true, Currency: current["currency"].(string), AmountMinor: int(offer["amount_minor"].(float64)), RecurringType: recurring, Interval: interval, IntervalCount: 1, Environment: "test"}
		mappingFor[sku] = id
	}
	for _, pack := range current["packs"].([]any) {
		addOffer(pack.(map[string]any), "one_time")
	}
	for _, plan := range current["plans"].([]any) {
		p := plan.(map[string]any)
		addOffer(p["monthly"].(map[string]any), "month")
		addOffer(p["yearly"].(map[string]any), "year")
	}
	mappingsWithPack := func(id string) map[string]any {
		result := map[string]any{}
		for sku, value := range mappingFor {
			result[sku] = value
		}
		result["pack_try"] = id
		return result
	}
	t.Run("Reject wrong Stripe price amount", func(t *testing.T) {
		wrongStripe := make(map[string]any)
		for k, v := range current {
			wrongStripe[k] = v
		}
		wrongStripe["version"] = "usd-e12-wrong-stripe-1"
		wrongStripe["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)
		wrongStripe["stripe_mappings"] = mappingsWithPack("price_try_wrong_amount")
		payload, _ := json.Marshal(map[string]any{
			"config": wrongStripe,
			"reason": "draft with wrong stripe price",
		})
		w := request(http.MethodPost, "/admin/v1/pricing/drafts", author, payload)
		if w.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", w.Code, w.Body)
		}

		// Validating draft must reject mismatching Stripe price
		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-e12-wrong-stripe-1/validate", author, []byte(`{"reason":"validate stripe price"}`))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for wrong stripe price, got %d body=%s", w.Code, w.Body)
		}
		if !strings.Contains(strings.ToLower(w.Body.String()), "stripe price") {
			t.Fatalf("expected Stripe price mismatch message, got %s", w.Body.String())
		}

		// Submitting must also fail
		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-e12-wrong-stripe-1/submit", author, []byte(`{"reason":"attempt submit"}`))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for submit with wrong stripe price, got %d body=%s", w.Code, w.Body)
		}
	})

	t.Run("Reject uneconomic catalogue draft below 70% margin", func(t *testing.T) {
		uneconomic := make(map[string]any)
		for k, v := range current {
			uneconomic[k] = v
		}
		uneconomic["version"] = "usd-e12-uneconomic-1"
		uneconomic["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)

		// Set pack_try to give 5000 credits for $5.00 (which is well below 70% margin)
		packsRaw, _ := json.Marshal(uneconomic["packs"])
		var packs []map[string]any
		_ = json.Unmarshal(packsRaw, &packs)
		packs[0]["credits"] = 5000 // 5000 * 0.0015 = $7.50 cost for $5.00 price
		uneconomic["packs"] = packs

		payload, _ := json.Marshal(map[string]any{
			"config": uneconomic,
			"reason": "draft with uneconomic pack margin",
		})
		w := request(http.MethodPost, "/admin/v1/pricing/drafts", author, payload)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for uneconomic draft creation, got %d body=%s", w.Code, w.Body)
		}
		if !strings.Contains(w.Body.String(), "below 70% margin") {
			t.Fatalf("expected 70%% margin error, got %s", w.Body.String())
		}
	})

	t.Run("Reject standard action cost above ceiling without override", func(t *testing.T) {
		unprofitableAction := make(map[string]any)
		for k, v := range current {
			unprofitableAction[k] = v
		}
		unprofitableAction["version"] = "usd-e12-cost-ceiling-1"
		unprofitableAction["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)

		// Lower lesson_content credits to 60 (cost is ~$0.1882, so 0.1882/60 = $0.0031/credit > 0.0015 ceiling)
		actionsRaw, _ := json.Marshal(unprofitableAction["actions"])
		var actions map[string]map[string]any
		_ = json.Unmarshal(actionsRaw, &actions)
		actions["lesson_content"]["credits"] = 60
		unprofitableAction["actions"] = actions

		payload, _ := json.Marshal(map[string]any{
			"config": unprofitableAction,
			"reason": "draft with cost ceiling violation",
		})
		w := request(http.MethodPost, "/admin/v1/pricing/drafts", author, payload)
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for cost ceiling violation without override, got %d body=%s", w.Code, w.Body)
		}
		if !strings.Contains(w.Body.String(), "protected cost ceiling") && !strings.Contains(w.Body.String(), "ceiling") {
			t.Fatalf("expected cost ceiling error, got %s", w.Body.String())
		}
	})

	t.Run("Valid draft with verified Stripe price and margin >= 70% can be approved", func(t *testing.T) {
		validDraft := make(map[string]any)
		for k, v := range current {
			validDraft[k] = v
		}
		validDraft["version"] = "usd-e12-valid-appr-1"
		validDraft["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)
		validDraft["stripe_mappings"] = mappingsWithPack("price_try_valid")
		payload, _ := json.Marshal(map[string]any{
			"config": validDraft,
			"reason": "valid economics and verified stripe mappings",
		})
		w := request(http.MethodPost, "/admin/v1/pricing/drafts", author, payload)
		if w.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", w.Code, w.Body)
		}
		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-e12-valid-appr-1/submit", author, []byte(`{"reason":"ready for review"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("submit status=%d body=%s", w.Code, w.Body)
		}
		w = request(http.MethodPost, "/admin/v1/pricing/drafts/usd-e12-valid-appr-1/approve", approver, []byte(`{"reason":"approved verified draft"}`))
		if w.Code != http.StatusOK {
			t.Fatalf("approve status=%d body=%s", w.Code, w.Body)
		}
	})

	t.Run("Rejection emits analytics and writes audit evidence without private reason", func(t *testing.T) {
		// Verify analytics captured pricing_version_rejected
		if closeErr := events.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		captureMu.Lock()
		defer captureMu.Unlock()

		var foundRejected bool
		for _, event := range captured {
			name, _ := event["event"].(string)
			if name == "pricing_version_rejected" {
				foundRejected = true
				props, _ := event["properties"].(map[string]any)
				if props["actor_id"] == "" || props["source"] != "api" || props["platform"] != "web" {
					t.Fatalf("missing common event properties: %+v", event)
				}
				if _, hasReason := props["reason"]; hasReason {
					t.Fatalf("private reason leaked into analytics: %+v", props)
				}
				raw, _ := json.Marshal(event)
				if strings.Contains(string(raw), "draft with wrong stripe price") || strings.Contains(string(raw), "draft with uneconomic pack margin") {
					t.Fatalf("private operator reason text in analytics payload: %s", raw)
				}
			}
		}
		if !foundRejected {
			t.Fatalf("pricing_version_rejected was not emitted: captured=%+v", captured)
		}

		// Verify audit table contains failure evidence
		var auditCount int
		err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM pricing_audit WHERE action='validation_failed'").Scan(&auditCount)
		if err != nil || auditCount < 1 {
			t.Fatalf("expected validation_failed audit rows, count=%d err=%v", auditCount, err)
		}
	})
}
