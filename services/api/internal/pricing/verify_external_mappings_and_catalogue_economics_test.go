package pricing_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

func loadApprovedConfig(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../../../apps/client/src/features/pricing/fixtures/approved.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestExactDailyAllowanceSizing(t *testing.T) {
	cfg := loadApprovedConfig(t)
	raw, _ := json.Marshal(cfg)

	// Approved seed has 46 daily credits, which exactly equals:
	// 3 * 2 (quick_note) + 8 * 5 (tutor_short) = 46
	// 3 * 2 (quick_note) + 5 * 8 (tutor_long) = 46
	if err := pricing.ValidateEconomics(raw, &pricing.DevelopmentVerifier{}, ""); err != nil {
		t.Fatalf("approved config economics failed: %v", err)
	}

	// Inexact daily free credits: change from 46 to 45
	daily := cfg["daily_free"].(map[string]any)
	daily["credits"] = 45
	badRaw, _ := json.Marshal(cfg)
	err := pricing.ValidateEconomics(badRaw, &pricing.DevelopmentVerifier{}, "")
	if err == nil || !strings.Contains(err.Error(), "daily_free.credits must match exact sizing") {
		t.Fatalf("expected exact daily allowance sizing error, got %v", err)
	}
}

func TestStandardActionCostCeilingAndOverride(t *testing.T) {
	cfg := loadApprovedConfig(t)

	// Lower lesson_content credits to 60 (standard cost is ~$0.1882, cost/credit = 0.0031 > 0.0015 ceiling)
	actions := cfg["actions"].(map[string]any)
	actions["lesson_content"] = map[string]any{"credits": 60}
	raw, _ := json.Marshal(cfg)

	// Without override, it must fail
	err := pricing.ValidateEconomics(raw, &pricing.DevelopmentVerifier{}, "")
	if err == nil || (!strings.Contains(err.Error(), "protected cost ceiling") && !strings.Contains(err.Error(), "ceiling")) {
		t.Fatalf("expected cost ceiling error, got %v", err)
	}

	// With explicit override reason, it is accepted
	errWithOverride := pricing.ValidateEconomics(raw, &pricing.DevelopmentVerifier{}, "approved promotional reduction for launch testing")
	if errWithOverride != nil {
		t.Fatalf("expected override to be permitted for cost ceiling, got: %v", errWithOverride)
	}
}

func TestMarginFloor70PercentAlwaysBlocks(t *testing.T) {
	cfg := loadApprovedConfig(t)

	// Make pack_try uneconomic: 1000 credits for $5.00
	// At $0.0015 ceiling cost, 1000 credits = $1.50.
	// Net receipts = 5.00 - 0.445 (fees) - 0.10 (refund) = $4.455.
	// Margin = (4.455 - 1.50) / 4.455 = 66.33% < 70%.
	packs := cfg["packs"].([]any)
	packs[0].(map[string]any)["credits"] = 1000
	raw, _ := json.Marshal(cfg)

	// Must fail without override
	err := pricing.ValidateEconomics(raw, &pricing.DevelopmentVerifier{}, "")
	if err == nil || !strings.Contains(err.Error(), "below 70% margin") {
		t.Fatalf("expected below 70%% margin error, got %v", err)
	}

	// Even with override, 70% margin floor ALWAYS BLOCKS (ADR 0011)
	errWithOverride := pricing.ValidateEconomics(raw, &pricing.DevelopmentVerifier{}, "trying to override 70% margin floor")
	if errWithOverride == nil || !strings.Contains(errWithOverride.Error(), "below 70% margin") {
		t.Fatalf("70%% margin floor must ALWAYS block activation, got %v", errWithOverride)
	}
}

func TestStripePriceVerification(t *testing.T) {
	price := pricing.StripePriceRecord{
		ID:            "price_test_123",
		Active:        true,
		Currency:      "USD",
		AmountMinor:   500,
		RecurringType: "one_time",
		Environment:   "test",
	}

	offer := pricing.Offer{
		SKU:         "pack_try",
		Currency:    "USD",
		AmountMinor: 500,
	}

	// Valid matching price
	if err := price.Matches("test", offer, "one_time"); err != nil {
		t.Fatalf("expected match, got %v", err)
	}

	// Amount mismatch
	badAmountOffer := pricing.Offer{SKU: "pack_try", Currency: "USD", AmountMinor: 600}
	if err := price.Matches("test", badAmountOffer, "one_time"); err == nil || !strings.Contains(err.Error(), "amount") {
		t.Fatalf("expected amount mismatch error, got %v", err)
	}

	// Currency mismatch
	badCurrencyOffer := pricing.Offer{SKU: "pack_try", Currency: "EUR", AmountMinor: 500}
	if err := price.Matches("test", badCurrencyOffer, "one_time"); err == nil || !strings.Contains(err.Error(), "currency") {
		t.Fatalf("expected currency mismatch error, got %v", err)
	}

	// Inactive price
	inactivePrice := price
	inactivePrice.Active = false
	if err := inactivePrice.Matches("test", offer, "one_time"); err == nil || !strings.Contains(err.Error(), "inactive") {
		t.Fatalf("expected inactive error, got %v", err)
	}

	// Recurring interval mismatch: pack matched against recurring price
	recurringPrice := price
	recurringPrice.RecurringType = "recurring"
	recurringPrice.Interval = "month"
	recurringPrice.IntervalCount = 1
	if err := recurringPrice.Matches("test", offer, "one_time"); err == nil || !strings.Contains(err.Error(), "recurring") {
		t.Fatalf("expected recurring interval mismatch error, got %v", err)
	}

	// Monthly plan matched against yearly price
	monthlyOffer := pricing.Offer{SKU: "learn_monthly", Currency: "USD", AmountMinor: 999}
	yearlyPrice := pricing.StripePriceRecord{
		ID:            "price_yearly",
		Active:        true,
		Currency:      "USD",
		AmountMinor:   999,
		RecurringType: "recurring",
		Interval:      "year",
		IntervalCount: 1,
		Environment:   "test",
	}
	if err := yearlyPrice.Matches("test", monthlyOffer, "month"); err == nil || !strings.Contains(err.Error(), "interval") {
		t.Fatalf("expected month interval error, got %v", err)
	}

	// Environment mismatch: test vs live
	if err := price.Matches("live", offer, "one_time"); err == nil || !strings.Contains(err.Error(), "environment") {
		t.Fatalf("expected environment mismatch error, got %v", err)
	}
}

func TestStoreProductIDValidation(t *testing.T) {
	// Apple valid and invalid
	if err := pricing.ValidateStoreProductID("apple", "com.duolify.pack_try"); err != nil {
		t.Fatalf("valid apple ID failed: %v", err)
	}
	if err := pricing.ValidateStoreProductID("apple", ""); err == nil {
		t.Fatal("empty apple ID accepted")
	}

	// Google valid and invalid
	if err := pricing.ValidateStoreProductID("google", "pack_try"); err != nil {
		t.Fatalf("valid google ID failed: %v", err)
	}
	if err := pricing.ValidateStoreProductID("google", ""); err == nil {
		t.Fatal("empty google ID accepted")
	}

	// Unknown channel
	if err := pricing.ValidateStoreProductID("unknown_channel", "prod_123"); err == nil {
		t.Fatal("unknown store channel accepted")
	}
}

func TestOfficialModelCatalogueEvidence(t *testing.T) {
	evidence := pricing.OfficialModelCatalogueEvidence()
	if len(evidence) == 0 {
		t.Fatal("missing official model catalogue evidence")
	}
	// Verify Anthropic primary models
	if evidence["claude-sonnet-5-5"].Provider != "anthropic" {
		t.Fatalf("expected anthropic provider for claude-sonnet-5-5: %+v", evidence["claude-sonnet-5-5"])
	}
	if evidence["claude-haiku-5-5"].Provider != "anthropic" {
		t.Fatalf("expected anthropic provider for claude-haiku-5-5: %+v", evidence["claude-haiku-5-5"])
	}
	// Verify OpenRouter official decimal aliases
	if evidence["anthropic/claude-sonnet-5.5"].Provider != "openrouter" {
		t.Fatalf("expected openrouter provider for anthropic/claude-sonnet-5.5: %+v", evidence["anthropic/claude-sonnet-5.5"])
	}
	if evidence["anthropic/claude-haiku-5.5"].Provider != "openrouter" {
		t.Fatalf("expected openrouter provider for anthropic/claude-haiku-5.5: %+v", evidence["anthropic/claude-haiku-5.5"])
	}
}

func TestConfigurableProcessingFees(t *testing.T) {
	cfg := loadApprovedConfig(t)
	// Add custom processing fees with higher fee rates making pack_try fall below 70%
	cfg["processing_fees"] = map[string]any{
		"pack_fee_rate":              0.60, // 60% fee
		"pack_fixed_fee_usd":         1.00,
		"subscription_fee_rate":      0.036,
		"subscription_fixed_fee_usd": 0.30,
		"refund_reserve_rate":        0.05,
	}
	raw, _ := json.Marshal(cfg)
	err := pricing.ValidateEconomics(raw, &pricing.DevelopmentVerifier{}, "")
	if err == nil || !strings.Contains(err.Error(), "below 70% margin") {
		t.Fatalf("expected margin rejection with higher configured fee rates, got %v", err)
	}

	// Verify ResolveProviderRates returns dev-proposed defaults when omitted
	rates := pricing.ResolveProviderRates(map[string]any{})
	if rates.PackFeeRate != 0.029 || rates.SubscriptionFeeRate != 0.036 || rates.RefundReserveRate != 0.020 {
		t.Fatalf("unexpected dev proposed rates: %+v", rates)
	}
}
