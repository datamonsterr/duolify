package credits

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
)

func TestCheckoutInputBoundaries(t *testing.T) {
	for _, target := range []string{"/credits", "/learn", "/notes", "/courses"} {
		if !safeCheckoutReturn(target) {
			t.Fatalf("safe target refused: %s", target)
		}
	}
	for _, target := range []string{"", "//evil.test", "/\\evil.test", "/credits?draft=secret", "/credits#secret", "/credits/../notes", "https://evil.test", "/notes/private"} {
		if safeCheckoutReturn(target) {
			t.Fatalf("unsafe target accepted: %s", target)
		}
	}
	cfg := pricing.Config{Version: "configured", Packs: []pricing.Pack{{Offer: pricing.Offer{SKU: "changed-pack", Currency: "EUR", AmountMinor: 723}, Credits: 519}}, Plans: []pricing.Plan{{ID: "test-plan", Monthly: pricing.Offer{SKU: "changed-month", Currency: "EUR", AmountMinor: 1773}, Yearly: pricing.Offer{SKU: "changed-year", Currency: "EUR", AmountMinor: 17579}, MonthlyCredits: 873}}}
	for _, tc := range []struct {
		sku, period     string
		amount, credits int
	}{{"changed-pack", "one_time", 723, 519}, {"changed-month", "monthly", 1773, 873}, {"changed-year", "yearly", 17579, 873}} {
		order, err := checkoutTerms(cfg, CheckoutInput{RequestID: uuid.NewString(), SKU: tc.sku, PricingVersion: cfg.Version, Platform: "desktop", ReturnTo: "/credits"})
		if err != nil || order.AmountMinor != tc.amount || order.Credits != tc.credits || order.BillingPeriod != tc.period || order.Currency != "EUR" {
			t.Fatalf("terms invented: %+v %v", order, err)
		}
	}
	for _, input := range []CheckoutInput{{SKU: "missing", PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"}, {SKU: "changed-pack", PricingVersion: "old", Platform: "web", ReturnTo: "/credits"}, {SKU: "changed-pack", PricingVersion: cfg.Version, Platform: "ios", ReturnTo: "/credits"}} {
		input.RequestID = uuid.NewString()
		if _, err := checkoutTerms(cfg, input); err == nil {
			t.Fatalf("invalid input accepted: %+v", input)
		}
	}
}
