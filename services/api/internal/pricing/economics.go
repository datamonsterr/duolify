package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Standard action protected costs from monetization.md (using official rates and reliability multipliers).
var standardProtectedCostsUSD = map[string]float64{
	"preview":        0.03499140,
	"outline":        0.08204880,
	"lesson_content": 0.18822960,
	"narration":      0.06336000,
	"tutor_short":    0.0006889686,
	"tutor_long":     0.0009906186,
	"quick_note":     0.0003257820,
}

// ValidateEconomics performs complete catalogue economics, margin, and external mapping checks.
func ValidateEconomics(raw []byte, verifier ExternalVerifier, overrideReason string) error {
	return ValidateEconomicsWithContext(context.Background(), raw, verifier, overrideReason)
}

// ValidateEconomicsWithContext performs complete catalogue economics, margin, and external mapping checks with context.
func ValidateEconomicsWithContext(ctx context.Context, raw []byte, verifier ExternalVerifier, overrideReason string) error {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}

	limits, _ := config["limits"].(map[string]any)
	ceiling, _ := number(limits["protected_cost_ceiling_usd_per_credit"])
	if ceiling <= 0 {
		return errors.New("missing protected cost ceiling")
	}

	// 1. Exact daily allowance sizing check
	dailyFree, _ := config["daily_free"].(map[string]any)
	dailyCredits, _ := number(dailyFree["credits"])
	actions, _ := config["actions"].(map[string]any)

	quickNoteCredits := actionCredits(actions, "quick_note")
	tutorShortCredits := actionCredits(actions, "tutor_short")
	tutorLongCredits := actionCredits(actions, "tutor_long")

	if quickNoteCredits > 0 && tutorShortCredits > 0 && tutorLongCredits > 0 {
		expectedShort := 3*quickNoteCredits + 8*tutorShortCredits
		expectedLong := 3*quickNoteCredits + 5*tutorLongCredits
		if int(dailyCredits) != expectedShort || int(dailyCredits) != expectedLong {
			return fmt.Errorf("daily_free.credits must match exact sizing: 3 quick notes and 8 short (or 5 long) tutor messages (expected %d, got %d)", expectedShort, int(dailyCredits))
		}
	}

	// 2. Standard action cost ceiling check (E12-04 override rules)
	hasOverride := strings.TrimSpace(overrideReason) != ""
	for actionName, protectedCost := range standardProtectedCostsUSD {
		credits := actionCredits(actions, actionName)
		if credits <= 0 {
			continue
		}
		costPerCredit := protectedCost / float64(credits)
		if costPerCredit > ceiling {
			if !hasOverride {
				return fmt.Errorf("standard action %s cost per credit %.6f exceeds protected cost ceiling %.6f", actionName, costPerCredit, ceiling)
			}
		}
	}

	// Helper for SKU margin:
	// net_receipts = price - fees - refunds
	// margin = 100 * (net_receipts - protected_ai_cost) / net_receipts
	// Under ADR 0011, 70% margin floor ALWAYS blocks activation (override cannot bypass it).
	calcMargin := func(priceUSD float64, feeRate float64, fixedFeeUSD float64, refundRate float64, credits int) (float64, error) {
		netReceipts := priceUSD*(1.0-feeRate-refundRate) - fixedFeeUSD
		if netReceipts <= 0 {
			return 0, errors.New("net receipts non-positive")
		}
		cost := float64(credits) * ceiling
		return 100.0 * (netReceipts - cost) / netReceipts, nil
	}

	// 3. Packs SKU margin check
	packs, _ := config["packs"].([]any)
	for _, item := range packs {
		m, _ := item.(map[string]any)
		sku, _ := m["sku"].(string)
		priceMinor, _ := number(m["amount_minor"])
		if priceMinor == 0 {
			priceMinor, _ = number(m["price_minor"])
		}
		credits, _ := number(m["credits"])
		priceUSD := priceMinor / 100.0

		// Web pack fee: 2.9% + $0.30; refund reserve: 2.0%
		mgn, err := calcMargin(priceUSD, 0.029, 0.30, 0.02, int(credits))
		if err != nil || mgn < 70.0 {
			return fmt.Errorf("sku %s is below 70%% margin (got %.2f%%)", sku, mgn)
		}
	}

	// 4. Plans SKU margin check
	plans, _ := config["plans"].([]any)
	for _, item := range plans {
		m, _ := item.(map[string]any)
		monthlyCredits, _ := number(m["monthly_credits"])

		// Monthly offer
		if monthlyOffer, ok := m["monthly"].(map[string]any); ok {
			sku, _ := monthlyOffer["sku"].(string)
			priceMinor, _ := number(monthlyOffer["amount_minor"])
			priceUSD := priceMinor / 100.0
			// Subscription web fee: 3.6% + $0.30; refund reserve: 2.0%
			mgn, err := calcMargin(priceUSD, 0.036, 0.30, 0.02, int(monthlyCredits))
			if err != nil || mgn < 70.0 {
				return fmt.Errorf("sku %s is below 70%% margin (got %.2f%%)", sku, mgn)
			}
		}

		// Yearly offer (12 months of credits)
		if yearlyOffer, ok := m["yearly"].(map[string]any); ok {
			sku, _ := yearlyOffer["sku"].(string)
			priceMinor, _ := number(yearlyOffer["amount_minor"])
			priceUSD := priceMinor / 100.0
			// Subscription web fee: 3.6% + $0.30; refund reserve: 2.0%
			mgn, err := calcMargin(priceUSD, 0.036, 0.30, 0.02, int(monthlyCredits*12))
			if err != nil || mgn < 70.0 {
				return fmt.Errorf("sku %s is below 70%% margin (got %.2f%%)", sku, mgn)
			}
		}
	}

	// 5. External payment mappings verification (if verifier is provided)
	if verifier != nil {
		if err := verifyExternalMappings(ctx, config, verifier); err != nil {
			return err
		}
	}

	return nil
}

func actionCredits(actions map[string]any, name string) int {
	if a, ok := actions[name].(map[string]any); ok {
		c, _ := number(a["credits"])
		return int(c)
	}
	return 0
}

func verifyExternalMappings(ctx context.Context, config map[string]any, verifier ExternalVerifier) error {
	currency, _ := config["currency"].(string)
	if currency == "" {
		currency = "USD"
	}

	// Build map of SKUs to expected Offer and interval
	type skuDetail struct {
		offer    Offer
		interval string
	}
	skuMap := map[string]skuDetail{}

	packs, _ := config["packs"].([]any)
	for _, item := range packs {
		m, _ := item.(map[string]any)
		sku, _ := m["sku"].(string)
		price, _ := number(m["amount_minor"])
		if price == 0 {
			price, _ = number(m["price_minor"])
		}
		skuMap[sku] = skuDetail{
			offer:    Offer{SKU: sku, Currency: currency, AmountMinor: int(price)},
			interval: "one_time",
		}
	}

	plans, _ := config["plans"].([]any)
	for _, item := range plans {
		m, _ := item.(map[string]any)
		if monthlyOffer, ok := m["monthly"].(map[string]any); ok {
			sku, _ := monthlyOffer["sku"].(string)
			price, _ := number(monthlyOffer["amount_minor"])
			skuMap[sku] = skuDetail{
				offer:    Offer{SKU: sku, Currency: currency, AmountMinor: int(price)},
				interval: "month",
			}
		}
		if yearlyOffer, ok := m["yearly"].(map[string]any); ok {
			sku, _ := yearlyOffer["sku"].(string)
			price, _ := number(yearlyOffer["amount_minor"])
			skuMap[sku] = skuDetail{
				offer:    Offer{SKU: sku, Currency: currency, AmountMinor: int(price)},
				interval: "year",
			}
		}
	}

	// 1. Check stripe_mappings (top-level or per SKU)
	stripeMappings, _ := config["stripe_mappings"].(map[string]any)
	for sku, detail := range skuMap {
		priceID := resolvePriceID(stripeMappings[sku], "test")
		if priceID != "" {
			if err := verifier.VerifyStripePrice(ctx, "test", priceID, detail.offer, detail.interval); err != nil {
				return fmt.Errorf("stripe price verification failed for SKU %s: %w", sku, err)
			}
		}
	}

	// 2. Check store_mappings
	storeMappings, _ := config["store_mappings"].(map[string]any)
	seenStoreProducts := map[string]string{} // "channel:productID" -> SKU
	for sku := range skuMap {
		if channels, ok := storeMappings[sku].(map[string]any); ok {
			for channel, prodVal := range channels {
				prodID, _ := prodVal.(string)
				if err := verifier.VerifyStoreMapping(ctx, channel, prodID); err != nil {
					return fmt.Errorf("store mapping verification failed for SKU %s: %w", sku, err)
				}
				key := strings.ToLower(channel) + ":" + prodID
				if existingSKU, seen := seenStoreProducts[key]; seen && existingSKU != sku {
					return fmt.Errorf("duplicate store product ID %s in channel %s for SKUs %s and %s", prodID, channel, existingSKU, sku)
				}
				seenStoreProducts[key] = sku
			}
		}
	}

	return nil
}

func resolvePriceID(v any, env string) string {
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if id, ok := val[env].(string); ok {
			return id
		}
	case map[string]string:
		if id, ok := val[env]; ok {
			return id
		}
	}
	return ""
}
