package pricing_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

type reviewVerifier struct {
	environments []string
	rejectLive   bool
}

func (v *reviewVerifier) VerifyStripePrice(_ context.Context, env, _ string, _ pricing.Offer, _ string) error {
	v.environments = append(v.environments, env)
	if env == "live" && v.rejectLive {
		return errors.New("live amount mismatch")
	}
	return nil
}
func (*reviewVerifier) VerifyStoreMapping(_ context.Context, channel, id string) error {
	return pricing.ValidateStoreProductID(channel, id)
}
func reviewMappings(cfg map[string]any) {
	mappings := map[string]any{}
	for sku := range cfg["development_mappings"].(map[string]any) {
		mappings[sku] = map[string]any{"test": "price_test_" + sku, "live": "price_live_" + sku}
	}
	cfg["stripe_mappings"] = mappings
}
func TestE12MappingGuards(t *testing.T) {
	t.Run("nil verifier", func(t *testing.T) {
		cfg := loadApprovedConfig(t)
		raw, _ := json.Marshal(cfg)
		err := pricing.ValidateEconomics(raw, nil, "")
		if err == nil || !strings.Contains(err.Error(), "verifier") {
			t.Fatalf("nil verifier error=%v", err)
		}
	})
	t.Run("every SKU requires mapping", func(t *testing.T) {
		cfg := loadApprovedConfig(t)
		cfg["stripe_mappings"] = map[string]any{}
		raw, _ := json.Marshal(cfg)
		err := pricing.ValidateEconomics(raw, &reviewVerifier{}, "")
		if err == nil || !strings.Contains(err.Error(), "mapping") {
			t.Fatalf("missing mappings error=%v", err)
		}
	})
	t.Run("live checked as well as test", func(t *testing.T) {
		cfg := loadApprovedConfig(t)
		reviewMappings(cfg)
		raw, _ := json.Marshal(cfg)
		v := &reviewVerifier{rejectLive: true}
		err := pricing.ValidateEconomics(raw, v, "")
		if err == nil || !strings.Contains(err.Error(), "live") {
			t.Fatalf("live verification error=%v checked=%v", err, v.environments)
		}
	})
}
func TestE12PlatformEconomicsCannotBeLowered(t *testing.T) {
	for _, field := range []string{"ceiling", "fees", "floor", "raised_action_ceiling"} {
		t.Run(field, func(t *testing.T) {
			cfg := loadApprovedConfig(t)
			reviewMappings(cfg)
			cfg["packs"].([]any)[0].(map[string]any)["credits"] = 940
			limits := cfg["limits"].(map[string]any)
			switch field {
			case "ceiling":
				limits["protected_cost_ceiling_usd_per_credit"] = 0.00001
			case "fees":
				cfg["processing_fees"] = map[string]any{"pack_fee_rate": 0, "pack_fixed_fee_usd": 0, "subscription_fee_rate": 0, "subscription_fixed_fee_usd": 0, "refund_reserve_rate": 0}
			case "floor":
				cfg["packs"].([]any)[0].(map[string]any)["credits"] = 100
				limits["minimum_paid_ai_margin_percent"] = 99
			case "raised_action_ceiling":
				cfg["packs"] = []any{}
				cfg["plans"] = []any{}
				limits["protected_cost_ceiling_usd_per_credit"] = 0.01
				cfg["actions"].(map[string]any)["lesson_content"].(map[string]any)["credits"] = 60
			}
			raw, _ := json.Marshal(cfg)
			override := ""
			if field == "ceiling" {
				override = "review action cost independently of SKU margin"
			}
			err := pricing.ValidateEconomics(raw, &reviewVerifier{}, override)
			if err == nil {
				t.Fatal("draft lowered platform economics protection")
			}
		})
	}
}
