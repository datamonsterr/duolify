//go:build integration

package pricing_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type fakeProviderAPI struct {
	prices        map[string]pricing.StripePriceRecord
	storeProducts map[string]bool
}

func newFakeProviderAPI() *fakeProviderAPI {
	return &fakeProviderAPI{
		prices: map[string]pricing.StripePriceRecord{
			"price_try_valid": {
				ID:            "price_try_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   500,
				RecurringType: "one_time",
				Environment:   "test",
			},
			"price_build_valid": {
				ID:            "price_build_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   1500,
				RecurringType: "one_time",
				Environment:   "test",
			},
			"price_deep_valid": {
				ID:            "price_deep_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   4000,
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
			"price_learn_y_valid": {
				ID:            "price_learn_y_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   9600,
				RecurringType: "recurring",
				Interval:      "year",
				IntervalCount: 1,
				Environment:   "test",
			},
			"price_plus_m_valid": {
				ID:            "price_plus_m_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   1999,
				RecurringType: "recurring",
				Interval:      "month",
				IntervalCount: 1,
				Environment:   "test",
			},
			"price_plus_y_valid": {
				ID:            "price_plus_y_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   19200,
				RecurringType: "recurring",
				Interval:      "year",
				IntervalCount: 1,
				Environment:   "test",
			},
			"price_pro_m_valid": {
				ID:            "price_pro_m_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   3999,
				RecurringType: "recurring",
				Interval:      "month",
				IntervalCount: 1,
				Environment:   "test",
			},
			"price_pro_y_valid": {
				ID:            "price_pro_y_valid",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   38400,
				RecurringType: "recurring",
				Interval:      "year",
				IntervalCount: 1,
				Environment:   "test",
			},
			"price_wrong_amount": {
				ID:            "price_wrong_amount",
				Active:        true,
				Currency:      "USD",
				AmountMinor:   450,
				RecurringType: "one_time",
				Environment:   "test",
			},
		},
		storeProducts: map[string]bool{
			"apple:com.duolify.pack_try":    true,
			"google:pack_try":               true,
			"apple:com.duolify.pack_build":  true,
			"google:pack_build":             true,
			"apple:com.duolify.pack_deep":   true,
			"google:pack_deep":              true,
			"apple:com.duolify.learn.month": true,
			"google:learn_monthly":          true,
		},
	}
}

func (f *fakeProviderAPI) VerifyStripePrice(_ context.Context, env, priceID string, expected pricing.Offer, interval string) error {
	p, ok := f.prices[priceID]
	if !ok {
		return pricing.ErrStripePriceNotFound
	}
	return p.Matches(env, expected, interval)
}

func (f *fakeProviderAPI) VerifyStoreMapping(_ context.Context, channel, productID string) error {
	if err := pricing.ValidateStoreProductID(channel, productID); err != nil {
		return err
	}
	return nil
}

func TestVerifyExternalMappingsAndCatalogueEconomicsIntegration(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("pricing_e12_07_int"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	var actor string
	if err = db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('pricing admin e12') RETURNING id::text").Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1::uuid)", actor); err != nil {
		t.Fatal(err)
	}

	fakeAPI := newFakeProviderAPI()
	admin := pricing.NewAdmin(db.Pool, nil, actor)
	admin.SetVerifier(fakeAPI)

	var current map[string]any
	if err = db.Pool.QueryRow(ctx, "SELECT config FROM pricing_versions WHERE version='usd-launch-2026-11-08'").Scan(&current); err != nil {
		t.Fatal(err)
	}

	t.Run("Valid draft with full external mappings passes economics and verification", func(t *testing.T) {
		validDraft := make(map[string]any)
		for k, v := range current {
			validDraft[k] = v
		}
		validDraft["version"] = "usd-e12-valid-int"
		validDraft["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)
		validDraft["stripe_mappings"] = map[string]any{
			"pack_try":      "price_try_valid",
			"pack_build":    "price_build_valid",
			"pack_deep":     "price_deep_valid",
			"learn_monthly": "price_learn_m_valid",
			"learn_yearly":  "price_learn_y_valid",
			"plus_monthly":  "price_plus_m_valid",
			"plus_yearly":   "price_plus_y_valid",
			"pro_monthly":   "price_pro_m_valid",
			"pro_yearly":    "price_pro_y_valid",
		}
		validDraft["store_mappings"] = map[string]map[string]string{
			"pack_try": {
				"apple":  "com.duolify.pack_try",
				"google": "pack_try",
			},
		}

		configBytes, _ := json.Marshal(validDraft)
		payload, _ := json.Marshal(map[string]any{
			"config": json.RawMessage(configBytes),
			"reason": "verified catalogue economics and provider mappings",
		})
		status, resp := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts", payload)
		if status != http.StatusCreated {
			t.Fatalf("create draft failed status=%d body=%s", status, resp)
		}

		validateBody, _ := json.Marshal(map[string]string{"reason": "validate economics"})
		status, resp = admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/usd-e12-valid-int/validate", validateBody)
		if status != http.StatusOK {
			t.Fatalf("validate failed status=%d body=%s", status, resp)
		}

		// Ensure audit record was inserted with evidence
		var auditReason string
		err = db.Pool.QueryRow(ctx, "SELECT reason FROM pricing_audit WHERE version='usd-e12-valid-int' AND action='validation_passed' ORDER BY id DESC LIMIT 1").Scan(&auditReason)
		if err != nil {
			t.Fatalf("validation_passed audit missing: %v", err)
		}
	})

	t.Run("Draft with wrong Stripe price amount is rejected in validate", func(t *testing.T) {
		badDraft := make(map[string]any)
		for k, v := range current {
			badDraft[k] = v
		}
		badDraft["version"] = "usd-e12-bad-stripe-int"
		badDraft["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)
		badDraft["stripe_mappings"] = map[string]any{
			"pack_try": "price_wrong_amount",
		}

		configBytes, _ := json.Marshal(badDraft)
		payload, _ := json.Marshal(map[string]any{
			"config": json.RawMessage(configBytes),
			"reason": "bad stripe mapping",
		})
		status, resp := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts", payload)
		if status != http.StatusCreated {
			t.Fatalf("create draft failed status=%d body=%s", status, resp)
		}

		validateBody, _ := json.Marshal(map[string]string{"reason": "validate wrong stripe price"})
		status, resp = admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts/usd-e12-bad-stripe-int/validate", validateBody)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for wrong stripe price, got %d body=%s", status, resp)
		}
		if !strings.Contains(strings.ToLower(string(resp)), "stripe price") {
			t.Fatalf("expected Stripe price mismatch error, got %s", resp)
		}
	})

	t.Run("Draft with inexact daily allowance sizing is rejected", func(t *testing.T) {
		inexactDaily := make(map[string]any)
		for k, v := range current {
			inexactDaily[k] = v
		}
		inexactDaily["version"] = "usd-e12-bad-daily-int"
		inexactDaily["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)
		// Correct is 46 = 3*2 + 8*5. Set to 50.
		dailyRaw, _ := json.Marshal(inexactDaily["daily_free"])
		var daily map[string]any
		_ = json.Unmarshal(dailyRaw, &daily)
		daily["credits"] = 50
		inexactDaily["daily_free"] = daily

		configBytes, _ := json.Marshal(inexactDaily)
		payload, _ := json.Marshal(map[string]any{
			"config": json.RawMessage(configBytes),
			"reason": "bad daily allowance sizing",
		})
		status, resp := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts", payload)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for inexact daily allowance sizing, got %d body=%s", status, resp)
		}
		if !strings.Contains(string(resp), "daily") && !strings.Contains(string(resp), "sizing") && !strings.Contains(string(resp), "allowance") {
			t.Fatalf("expected daily allowance sizing error message, got %s", resp)
		}
	})

	t.Run("Draft with standard action cost above ceiling blocked unless override reason is supplied", func(t *testing.T) {
		ceilingDraft := make(map[string]any)
		for k, v := range current {
			ceilingDraft[k] = v
		}
		ceilingDraft["version"] = "usd-e12-ceiling-int"
		ceilingDraft["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)

		// Set course_outline credits to 10 (cost is $0.0820, so 0.0820/10 = 0.0082 > 0.0015 ceiling)
		actionsRaw, _ := json.Marshal(ceilingDraft["actions"])
		var actions map[string]map[string]any
		_ = json.Unmarshal(actionsRaw, &actions)
		actions["outline"]["credits"] = 10
		ceilingDraft["actions"] = actions

		configBytes, _ := json.Marshal(ceilingDraft)
		payload, _ := json.Marshal(map[string]any{
			"config": json.RawMessage(configBytes),
			"reason": "ceiling check draft",
		})
		status, resp := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts", payload)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for ceiling check draft, got %d body=%s", status, resp)
		}

		// When explicit override reason is provided in payload, draft is created
		payloadWithOverride, _ := json.Marshal(map[string]any{
			"config":          json.RawMessage(configBytes),
			"reason":          "ceiling check draft",
			"override_reason": "explicit operator promotional override for outline",
		})
		status, resp = admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts", payloadWithOverride)
		if status != http.StatusCreated {
			t.Fatalf("expected 201 for ceiling check draft with override, got %d body=%s", status, resp)
		}
	})

	t.Run("SKU below 70% margin is rejected EVEN WITH override reason", func(t *testing.T) {
		lowMarginDraft := make(map[string]any)
		for k, v := range current {
			lowMarginDraft[k] = v
		}
		lowMarginDraft["version"] = "usd-e12-low-margin-int"
		lowMarginDraft["effective_from"] = time.Now().UTC().Add(74 * time.Hour).Format(time.RFC3339Nano)

		// Make pack_build uneconomic: 10,000 credits for $5.00
		packsRaw, _ := json.Marshal(lowMarginDraft["packs"])
		var packs []map[string]any
		_ = json.Unmarshal(packsRaw, &packs)
		packs[0]["credits"] = 10000
		lowMarginDraft["packs"] = packs

		configBytes, _ := json.Marshal(lowMarginDraft)
		payloadWithOverride, _ := json.Marshal(map[string]any{
			"config":          json.RawMessage(configBytes),
			"reason":          "low margin attempt",
			"override_reason": "attempt to override margin floor",
		})
		status, resp := admin.ExecuteCLI(ctx, actor, http.MethodPost, "/admin/v1/pricing/drafts", payloadWithOverride)
		if status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422 for SKU below 70%% margin even with override, got %d body=%s", status, resp)
		}
		if !strings.Contains(string(resp), "below 70% margin") {
			t.Fatalf("expected below 70%% margin error message, got %s", resp)
		}
	})
}
