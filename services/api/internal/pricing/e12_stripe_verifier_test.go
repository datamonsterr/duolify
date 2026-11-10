package pricing_test

import (
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"io"
	"net/http"
	"strings"
	"testing"
)

type pricingTransport func(*http.Request) (*http.Response, error)

func (f pricingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestStripeVerifierUsesCanonicalProviderState(t *testing.T) {
	base := `{"id":"price_fixture","active":true,"currency":"usd","unit_amount":500,"livemode":false,"type":"one_time","billing_scheme":"per_unit","transform_quantity":null}`
	cases := []struct {
		name, body string
		status     int
		valid      bool
	}{
		{"valid", base, 200, true},
		{"live mismatch", strings.Replace(base, `"livemode":false`, `"livemode":true`, 1), 200, false},
		{"inactive", strings.Replace(base, `"active":true`, `"active":false`, 1), 200, false},
		{"wrong amount", strings.Replace(base, `"unit_amount":500`, `"unit_amount":501`, 1), 200, false},
		{"missing amount", strings.Replace(base, `"unit_amount":500,`, "", 1), 200, false},
		{"unavailable", `{"error":"private-provider-details"}`, 503, false},
		{"unknown", `{}`, 404, false},
		{"malformed", `{`, 200, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: pricingTransport(func(r *http.Request) (*http.Response, error) {
				key, password, ok := r.BasicAuth()
				if !ok || key != "fixture-test-key" || password != "" {
					t.Fatal("missing environment credentials")
				}
				if r.URL.String() != "https://api.stripe.com/v1/prices/price_fixture" || r.Header.Get("Stripe-Version") == "" {
					t.Fatalf("unexpected request %s", r.URL)
				}
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			verifier := pricing.NewStripeVerifier("fixture-test-key", "", client)
			err := verifier.VerifyStripePrice(t.Context(), "test", "price_fixture", pricing.Offer{SKU: "pack", Currency: "USD", AmountMinor: 500}, "one_time")
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
			if err != nil && (strings.Contains(err.Error(), "private-provider-details") || strings.Contains(err.Error(), "fixture-test-key")) {
				t.Fatal("provider secrets/details leaked")
			}
			if err := verifier.VerifyStripePrice(t.Context(), "live", "price_fixture", pricing.Offer{}, "one_time"); err == nil {
				t.Fatal("missing live credentials accepted")
			}
		})
	}
}
func TestPricingVerifierEnvironmentGates(t *testing.T) {
	for _, env := range []string{"", "production", "prod", "Development", "test", "development"} {
		t.Run(env, func(t *testing.T) {
			verifier, err := pricing.VerifierFromEnvironment(func(k string) string {
				switch k {
				case "DUOLIFY_ENV":
					return env
				case "ALLOW_FAKE_PRICING_VERIFIER":
					return "true"
				}
				return ""
			})
			allowed := env == "test" || env == "development"
			if (err == nil) != allowed || (verifier != nil) != allowed {
				t.Fatalf("env=%q verifier=%T err=%v", env, verifier, err)
			}
		})
	}
	if verifier, err := pricing.VerifierFromEnvironment(func(string) string { return "" }); err != nil || verifier != nil {
		t.Fatalf("missing credentials must leave fail-closed verifier: %T %v", verifier, err)
	}
}

func TestPricingEconomicsOperatorMinimumsCannotBeWeakened(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{}`, true},
		{`{"minimum_paid_ai_margin_percent":80}`, true},
		{`{"minimum_paid_ai_margin_percent":69}`, false},
		{`{"protected_cost_ceiling_usd_per_credit":0.00001}`, false},
		{`{"processing_fees":{"pack_fee_rate":0}}`, false},
		{`{"processing_fees":{"subscription_fixed_fee_usd":0}}`, false},
		{`{"processing_fees":{"refund_reserve_rate":0}}`, false},
		{`{"unknown":1}`, false},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			admin := pricing.NewAdmin(nil, nil, "")
			err := admin.ConfigureVerification(func(k string) string {
				if k == "PRICING_ECONOMICS_MINIMUMS" {
					return tc.raw
				}
				return ""
			})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
