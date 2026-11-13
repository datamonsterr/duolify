package pricing

import "testing"

func TestClassifyAdverseAcrossPricingFields(t *testing.T) {
	base := []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
	cases := []struct{ name, changed string }{
		{"action cost", `"credits":141`},
		{"daily allowance", `"credits":45`},
		{"pack price", `"amount_minor":1600`},
		{"pack credits", `"credits":1300`},
		{"plan price", `"amount_minor":2099`},
		{"plan credits", `"monthly_credits":1500`},
		{"removed entitlement", `"entitlements":["offline"]`},
		{"tightened limit", `"generation_concurrency":1`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := base
			switch tc.name {
			case "action cost":
				next = []byte(`{"actions":{"lesson_content":{"credits":141}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
			case "daily allowance":
				next = []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":45},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
			case "pack price":
				next = []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1600}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
			case "pack credits":
				next = []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1300,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
			case "plan price":
				next = []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":2099,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
			case "plan credits":
				next = []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1500,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
			case "removed entitlement":
				next = []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline"]}],"limits":{"generation_concurrency":2}}`)
			case "tightened limit":
				next = []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1350,"amount_minor":1500}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":1}}`)
			}
			adverse, err := classifyAdverse(base, next)
			if err != nil || !adverse {
				t.Fatalf("adverse=%v err=%v", adverse, err)
			}
		})
	}
	// Mixed favorable/adverse fields remain adverse.
	mixed := []byte(`{"actions":{"lesson_content":{"credits":139}},"daily_free":{"credits":46},"packs":[{"sku":"pack_build","credits":1300,"amount_minor":1600}],"plans":[{"sku":"plus_monthly","amount_minor":1999,"monthly_credits":1600,"entitlements":["offline","export"]}],"limits":{"generation_concurrency":2}}`)
	if adverse, err := classifyAdverse(base, mixed); err != nil || !adverse {
		t.Fatalf("mixed change adverse=%v err=%v", adverse, err)
	}
}

func TestValidatePricingAdminReason(t *testing.T) {
	for _, value := range []string{"", "   ", "\n"} {
		if validateReason(value) == nil {
			t.Fatalf("accepted blank reason %q", value)
		}
	}
	if err := validateReason("owner-approved price adjustment"); err != nil {
		t.Fatalf("rejected useful reason: %v", err)
	}
}

func TestLowerActionCostIsNonAdverseAndMarginFloorIsExact(t *testing.T) {
	before := []byte(`{"actions":{"lesson_content":{"credits":140}},"daily_free":{"credits":46},"packs":[],"plans":[],"limits":{"generation_concurrency":2}}`)
	after := []byte(`{"actions":{"lesson_content":{"credits":139}},"daily_free":{"credits":46},"packs":[],"plans":[],"limits":{"generation_concurrency":2}}`)
	if adverse, err := classifyAdverse(before, after); err != nil || adverse {
		t.Fatalf("lower action cost adverse=%v err=%v", adverse, err)
	}
	if err := validateMargins([]byte(`{"limits":{"protected_cost_ceiling_usd_per_credit":0.0015},"packs":[{"sku":"edge","amount_minor":50,"credits":100}],"plans":[]}`)); err != nil {
		t.Fatalf("70%% margin rejected: %v", err)
	}
	if err := validateMargins([]byte(`{"limits":{"protected_cost_ceiling_usd_per_credit":0.0015},"packs":[{"sku":"below","amount_minor":40,"credits":100}],"plans":[]}`)); err == nil {
		t.Fatal("SKU below 70% margin passed")
	}
}
