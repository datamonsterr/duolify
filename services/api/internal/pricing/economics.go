package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

type policyReader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func economicsRoute(ctx context.Context, db policyReader, source string) (QuotePolicy, error) {
	var p QuotePolicy
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT policy FROM pricing_quote_policies WHERE version=$1`, source).Scan(&raw); err != nil {
		return p, fmt.Errorf("pricing route unavailable: %w", err)
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, fmt.Errorf("invalid pricing route: %w", err)
	}
	return p, nil
}

func economicsSource(explicit, parent string) string {
	if explicit != "" {
		return explicit
	}
	return parent
}

// routeProtectedCost prices every configured attempt at the highest input and
// output rate in the pinned route, including the existing reliability reserve.
func routeProtectedCost(p QuotePolicy, action string, input, output int64) (float64, error) {
	key := p.ActionModels[action]
	if key == "" || p.MaxAttempts <= 0 || input <= 0 || output <= 0 {
		return 0, fmt.Errorf("missing economics route or bounds for %s", action)
	}
	seen := map[string]bool{}
	var inRate, outRate int64
	for key != "" {
		if seen[key] {
			return 0, fmt.Errorf("cyclic economics route for %s", action)
		}
		seen[key] = true
		rate, ok := p.Models[key]
		if !ok || rate.Provider == "" || rate.Model == "" || rate.Input <= 0 || rate.Output <= 0 {
			return 0, fmt.Errorf("missing economics rate for %s", action)
		}
		inRate, outRate = max(inRate, rate.Input), max(outRate, rate.Output)
		key = p.FallbackModels[key]
	}
	return (float64(input)*float64(inRate) + float64(output)*float64(outRate)) * float64(p.MaxAttempts) * 1.2066 / 1e9, nil
}

func enforceRouteBudget(p QuotePolicy, action string, input, output, credits int64, ceiling float64) error {
	cost, err := routeProtectedCost(p, action, input, output)
	if err != nil {
		return err
	}
	if cost > float64(credits)*ceiling {
		return fmt.Errorf("standard action %s protected cost %.9f exceeds budget %.9f", action, cost, float64(credits)*ceiling)
	}
	return nil
}

// Text action costs come from the version-pinned route. Narration is billed by
// character and retains its separately costed protected standard envelope.
var standardActions = []string{"preview", "outline", "outline_replan", "lesson_content", "narration", "tutor_short", "tutor_long", "quick_note"}

const standardNarrationProtectedCostUSD = 0.06336

// ProviderRateConfig defines the payment provider processing fees and refund reserve
// used for catalogue margin economics calculations.
type ProviderRateConfig struct {
	PackFeeRate             float64 `json:"pack_fee_rate"`
	PackFixedFeeUSD         float64 `json:"pack_fixed_fee_usd"`
	SubscriptionFeeRate     float64 `json:"subscription_fee_rate"`
	SubscriptionFixedFeeUSD float64 `json:"subscription_fixed_fee_usd"`
	RefundReserveRate       float64 `json:"refund_reserve_rate"`
}

// DevProposedProviderRates represents the development-proposed payment processing fee rates
// and refund reserve (Stripe standard web fee 2.9% + $0.30 for packs, 3.6% + $0.30 for recurring billing,
// and 2.0% refund reserve), pending final owner review.
var DevProposedProviderRates = ProviderRateConfig{
	PackFeeRate:             0.029,
	PackFixedFeeUSD:         0.30,
	SubscriptionFeeRate:     0.036,
	SubscriptionFixedFeeUSD: 0.30,
	RefundReserveRate:       0.020,
}

// ResolveProviderRates resolves provider processing fee rates and refund reserve from the
// pricing config's processing_fees object if present, falling back to DevProposedProviderRates.
func ResolveProviderRates(config map[string]any) ProviderRateConfig {
	rates := DevProposedProviderRates
	if pf, ok := config["processing_fees"].(map[string]any); ok {
		if v, ok := number(pf["pack_fee_rate"]); ok && v >= 0 {
			rates.PackFeeRate = math.Max(rates.PackFeeRate, v)
		}
		if v, ok := number(pf["pack_fixed_fee_usd"]); ok && v >= 0 {
			rates.PackFixedFeeUSD = math.Max(rates.PackFixedFeeUSD, v)
		}
		if v, ok := number(pf["subscription_fee_rate"]); ok && v >= 0 {
			rates.SubscriptionFeeRate = math.Max(rates.SubscriptionFeeRate, v)
		}
		if v, ok := number(pf["subscription_fixed_fee_usd"]); ok && v >= 0 {
			rates.SubscriptionFixedFeeUSD = math.Max(rates.SubscriptionFixedFeeUSD, v)
		}
		if v, ok := number(pf["refund_reserve_rate"]); ok && v >= 0 {
			rates.RefundReserveRate = math.Max(rates.RefundReserveRate, v)
		}
	}
	return rates
}

// ValidateEconomics performs complete catalogue economics, margin, and external mapping checks.
func ValidateEconomics(raw []byte, verifier ExternalVerifier, overrideReason string, routes ...QuotePolicy) error {
	return ValidateEconomicsWithContext(context.Background(), raw, verifier, overrideReason, routes...)
}

// ValidateEconomicsWithContext performs complete catalogue economics, margin, and external mapping checks with context.
func ValidateEconomicsWithContext(ctx context.Context, raw []byte, verifier ExternalVerifier, overrideReason string, routes ...QuotePolicy) error {
	if len(routes) != 1 {
		return errors.New("exactly one version-pinned pricing route is required")
	}
	_, err := checkEconomics(ctx, raw, verifier, overrideReason, defaultEconomicsPolicy(), false, routes[0])
	return err
}

type economicsPolicy struct {
	ProtectedCostCeiling float64            `json:"protected_cost_ceiling_usd_per_credit"`
	MarginFloor          float64            `json:"minimum_paid_ai_margin_percent"`
	Fees                 ProviderRateConfig `json:"processing_fees"`
}

func defaultEconomicsPolicy() economicsPolicy {
	return economicsPolicy{0.0015, 70, DevProposedProviderRates}
}

type verificationEvidence struct {
	MarginFloorVerified bool             `json:"margin_floor_verified"`
	EconomicsVerified   bool             `json:"economics_verified"`
	MappingsVerified    bool             `json:"mappings_verified"`
	Policy              economicsPolicy  `json:"platform_minimums"`
	Checks              []map[string]any `json:"checks"`
}
type evidenceVerifier struct {
	ExternalVerifier
	evidence *verificationEvidence
}

func (v evidenceVerifier) VerifyStripePrice(ctx context.Context, env, id string, expected Offer, interval string) error {
	err := v.ExternalVerifier.VerifyStripePrice(ctx, env, id, expected, interval)
	v.evidence.Checks = append(v.evidence.Checks, map[string]any{"check": "stripe_price", "environment": env, "price_id": id, "expected": expected, "interval": interval, "verified": err == nil})
	return err
}
func (v evidenceVerifier) VerifyStoreMapping(ctx context.Context, channel, id string) error {
	err := v.ExternalVerifier.VerifyStoreMapping(ctx, channel, id)
	v.evidence.Checks = append(v.evidence.Checks, map[string]any{"check": "store_mapping", "channel": channel, "product_id": id, "verified": err == nil})
	return err
}
func checkEconomics(ctx context.Context, raw []byte, verifier ExternalVerifier, overrideReason string, policy economicsPolicy, draftOnly bool, route QuotePolicy) (verificationEvidence, error) {
	evidence := verificationEvidence{Policy: policy}
	if _, fake := verifier.(*DevelopmentVerifier); fake {
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return evidence, err
		}
		mappings, _ := cfg["stripe_mappings"].(map[string]any)
		if mappings == nil {
			mappings = map[string]any{}
		}
		development, _ := cfg["development_mappings"].(map[string]any)
		for sku, id := range development {
			if _, exists := mappings[sku]; !exists {
				mappings[sku] = map[string]any{"test": id}
			}
		}
		cfg["stripe_mappings"] = mappings
		raw, _ = json.Marshal(cfg)
		evidence.Checks = append(evidence.Checks, map[string]any{"check": "explicit_development_fake", "verified": false})
	}
	var wrapped ExternalVerifier
	if verifier != nil {
		wrapped = evidenceVerifier{verifier, &evidence}
	}
	err := validateEconomics(ctx, raw, wrapped, overrideReason, policy, draftOnly, &evidence, route)
	return evidence, err
}
func validateEconomics(ctx context.Context, raw []byte, verifier ExternalVerifier, overrideReason string, policy economicsPolicy, draftOnly bool, evidence *verificationEvidence, route QuotePolicy) error {
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		return err
	}

	limits, _ := config["limits"].(map[string]any)
	ceiling, _ := number(limits["protected_cost_ceiling_usd_per_credit"])
	if ceiling <= 0 {
		return errors.New("missing protected cost ceiling")
	}

	// A lower draft cost estimate cannot lower SKU cost. A higher draft
	// ceiling cannot relax the standard action ceiling.
	marginCost := math.Max(ceiling, policy.ProtectedCostCeiling)
	ceiling = math.Min(ceiling, policy.ProtectedCostCeiling)
	floor, _ := number(limits["minimum_paid_ai_margin_percent"])
	floor = math.Max(math.Max(floor, policy.MarginFloor), 70)
	if strings.TrimSpace(overrideReason) != "" {
		if err := validateReason(overrideReason); err != nil {
			return fmt.Errorf("override reason: %w", err)
		}
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
	for _, actionName := range standardActions {
		protectedCost := standardNarrationProtectedCostUSD
		credits := actionCredits(actions, actionName)
		if credits <= 0 {
			continue
		}
		if actionName != "narration" {
			p := route
			key := actionName
			class := ""
			if actionName == "tutor_short" || actionName == "tutor_long" {
				key, class = "tutor", strings.TrimPrefix(actionName, "tutor_")
			}
			bounds, _ := limits[key].(map[string]any)
			in, _ := number(bounds["input_tokens"])
			out, _ := number(bounds["output_tokens"])
			if class != "" {
				// Large-context quotes can use the whole configured prompt limit;
				// price it uncached to protect the worst permitted request.
				in, _ = number(bounds["prompt_tokens"])
				out, _ = number(bounds[class+"_output_tokens"])
			}
			if attempts, ok := number(bounds["max_attempts"]); ok {
				p.MaxAttempts = int64(attempts)
			}
			if err := enforceRouteBudget(p, actionName, int64(in), int64(out), int64(credits), ceiling); err != nil {
				return err
			}
			protectedCost, _ = routeProtectedCost(p, actionName, int64(in), int64(out))
		}
		costPerCredit := protectedCost / float64(credits)
		evidence.Checks = append(evidence.Checks, map[string]any{"check": "action_cost", "action": actionName, "cost_per_credit": costPerCredit, "ceiling": ceiling, "overridden": costPerCredit > ceiling && hasOverride, "verified": costPerCredit <= ceiling})
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
		cost := float64(credits) * marginCost
		return 100.0 * (netReceipts - cost) / netReceipts, nil
	}

	rates := ResolveProviderRates(config)
	rates.PackFeeRate = math.Max(rates.PackFeeRate, policy.Fees.PackFeeRate)
	rates.PackFixedFeeUSD = math.Max(rates.PackFixedFeeUSD, policy.Fees.PackFixedFeeUSD)
	rates.SubscriptionFeeRate = math.Max(rates.SubscriptionFeeRate, policy.Fees.SubscriptionFeeRate)
	rates.SubscriptionFixedFeeUSD = math.Max(rates.SubscriptionFixedFeeUSD, policy.Fees.SubscriptionFixedFeeUSD)
	rates.RefundReserveRate = math.Max(rates.RefundReserveRate, policy.Fees.RefundReserveRate)

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

		mgn, err := calcMargin(priceUSD, rates.PackFeeRate, rates.PackFixedFeeUSD, rates.RefundReserveRate, int(credits))
		evidence.Checks = append(evidence.Checks, map[string]any{"check": "sku_margin", "sku": sku, "margin_percent": mgn, "floor_percent": floor, "protected_cost_per_credit": marginCost, "fees": rates, "verified": err == nil && mgn >= floor})
		if err != nil || mgn < floor {
			return fmt.Errorf("sku %s is below %.0f%% margin (got %.2f%%)", sku, floor, mgn)
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
			mgn, err := calcMargin(priceUSD, rates.SubscriptionFeeRate, rates.SubscriptionFixedFeeUSD, rates.RefundReserveRate, int(monthlyCredits))
			evidence.Checks = append(evidence.Checks, map[string]any{"check": "sku_margin", "sku": sku, "margin_percent": mgn, "floor_percent": floor, "protected_cost_per_credit": marginCost, "fees": rates, "verified": err == nil && mgn >= floor})
			if err != nil || mgn < floor {
				return fmt.Errorf("sku %s is below %.0f%% margin (got %.2f%%)", sku, floor, mgn)
			}
		}

		// Yearly offer (12 months of credits)
		if yearlyOffer, ok := m["yearly"].(map[string]any); ok {
			sku, _ := yearlyOffer["sku"].(string)
			priceMinor, _ := number(yearlyOffer["amount_minor"])
			priceUSD := priceMinor / 100.0
			mgn, err := calcMargin(priceUSD, rates.SubscriptionFeeRate, rates.SubscriptionFixedFeeUSD, rates.RefundReserveRate, int(monthlyCredits*12))
			evidence.Checks = append(evidence.Checks, map[string]any{"check": "sku_margin", "sku": sku, "margin_percent": mgn, "floor_percent": floor, "protected_cost_per_credit": marginCost, "fees": rates, "verified": err == nil && mgn >= floor})
			if err != nil || mgn < floor {
				return fmt.Errorf("sku %s is below %.0f%% margin (got %.2f%%)", sku, floor, mgn)
			}
		}
	}

	evidence.MarginFloorVerified = true
	evidence.EconomicsVerified = true
	if draftOnly {
		return nil
	}
	if verifier == nil {
		return errors.New("external mapping verifier is required")
	}
	if err := verifyExternalMappings(ctx, config, verifier); err != nil {
		return err
	}
	evidence.MappingsVerified = true

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

	// Every SKU must have an explicit mapping; every declared environment is checked.
	stripeMappings, _ := config["stripe_mappings"].(map[string]any)
	skus := make([]string, 0, len(skuMap))
	for sku := range skuMap {
		skus = append(skus, sku)
	}
	sort.Strings(skus)
	for _, sku := range skus {
		detail := skuMap[sku]
		environments := map[string]string{}
		switch value := stripeMappings[sku].(type) {
		case string:
			environments["test"] = value
		case map[string]any:
			for env, id := range value {
				text, _ := id.(string)
				environments[env] = text
			}
		}
		if len(environments) == 0 {
			return fmt.Errorf("missing stripe mapping for SKU %s", sku)
		}
		envs := make([]string, 0, len(environments))
		for env := range environments {
			envs = append(envs, env)
		}
		sort.Strings(envs)
		for _, env := range envs {
			id := environments[env]
			if (env != "test" && env != "live") || strings.TrimSpace(id) == "" {
				return fmt.Errorf("invalid stripe mapping environment %s for SKU %s", env, sku)
			}
			if err := verifier.VerifyStripePrice(ctx, env, id, detail.offer, detail.interval); err != nil {
				return fmt.Errorf("stripe price verification failed for SKU %s (%s): %w", sku, env, err)
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
