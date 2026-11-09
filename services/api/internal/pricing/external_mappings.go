package pricing

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	// ErrStripePriceNotFound is returned when a price ID does not exist in Stripe.
	ErrStripePriceNotFound = errors.New("stripe price not found")

	appleProductIDRegex  = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	googleProductIDRegex = regexp.MustCompile(`^[a-z0-9._-]+$`)
)

// StripePriceRecord models the verified state of a Stripe price from the provider API.
type StripePriceRecord struct {
	ID            string `json:"id"`
	Active        bool   `json:"active"`
	Currency      string `json:"currency"`
	AmountMinor   int    `json:"amount_minor"`
	RecurringType string `json:"recurring_type"` // "one_time" or "recurring"
	Interval      string `json:"interval,omitempty"`
	IntervalCount int    `json:"interval_count,omitempty"`
	Environment   string `json:"environment"` // "test" or "live"
}

// Matches checks that the Stripe price satisfies all requirements for the given offer and environment.
func (p StripePriceRecord) Matches(targetEnv string, expected Offer, expectedInterval string) error {
	if !p.Active {
		return fmt.Errorf("stripe price %s is inactive", p.ID)
	}
	if !strings.EqualFold(p.Environment, targetEnv) {
		return fmt.Errorf("stripe price %s environment mismatch: expected %s, got %s", p.ID, targetEnv, p.Environment)
	}
	if !strings.EqualFold(p.Currency, expected.Currency) {
		return fmt.Errorf("stripe price %s currency %s does not match catalogue currency %s", p.ID, p.Currency, expected.Currency)
	}
	if p.AmountMinor != expected.AmountMinor {
		return fmt.Errorf("stripe price %s amount %d does not match catalogue amount %d", p.ID, p.AmountMinor, expected.AmountMinor)
	}
	if expectedInterval == "one_time" {
		if p.RecurringType == "recurring" {
			return fmt.Errorf("stripe price %s is recurring; pack requires one-time price", p.ID)
		}
	} else {
		if p.RecurringType != "recurring" {
			return fmt.Errorf("stripe price %s is one-time; subscription requires recurring price", p.ID)
		}
		if !strings.EqualFold(p.Interval, expectedInterval) {
			return fmt.Errorf("stripe price %s interval %s does not match required %s", p.ID, p.Interval, expectedInterval)
		}
		if p.IntervalCount != 1 {
			return fmt.Errorf("stripe price %s interval count %d must be 1", p.ID, p.IntervalCount)
		}
	}
	return nil
}

// ExternalVerifier verifies Stripe prices and store product mappings against provider APIs or fakes.
type ExternalVerifier interface {
	VerifyStripePrice(ctx context.Context, env, priceID string, expected Offer, interval string) error
	VerifyStoreMapping(ctx context.Context, channel, productID string) error
}

// ValidateStoreProductID validates store-localized product identifiers per channel conventions.
func ValidateStoreProductID(channel, productID string) error {
	productID = strings.TrimSpace(productID)
	if productID == "" {
		return fmt.Errorf("store product ID cannot be empty for channel %s", channel)
	}
	switch strings.ToLower(channel) {
	case "apple":
		if !appleProductIDRegex.MatchString(productID) {
			return fmt.Errorf("invalid store product ID format for channel apple: %s", productID)
		}
	case "google":
		if !googleProductIDRegex.MatchString(productID) {
			return fmt.Errorf("invalid store product ID format for channel google: %s", productID)
		}
	default:
		return fmt.Errorf("unknown store channel: %s", channel)
	}
	return nil
}

// OfficialModel holds official catalogue rate evidence.
type OfficialModel struct {
	Provider                    string  `json:"provider"`
	ModelID                     string  `json:"model_id"`
	InputUSDPerMillion          float64 `json:"input_usd_per_million"`
	OutputUSDPerMillion         float64 `json:"output_usd_per_million"`
	CacheWriteUSDPerMillion     float64 `json:"cache_write_usd_per_million"`
	CacheReadUSDPerMillion      float64 `json:"cache_read_usd_per_million"`
	NarrationUSDPerThousandChar float64 `json:"narration_usd_per_thousand_char"`
}

// OfficialModelCatalogueEvidence returns dated evidence retrieved from official provider catalogues.
func OfficialModelCatalogueEvidence() map[string]OfficialModel {
	return map[string]OfficialModel{
		"claude-sonnet-5-5": {
			Provider:                "anthropic",
			ModelID:                 "claude-sonnet-5-5",
			InputUSDPerMillion:      2.00,
			OutputUSDPerMillion:     10.00,
			CacheWriteUSDPerMillion: 2.50,
			CacheReadUSDPerMillion:  0.10,
		},
		"claude-haiku-5-5": {
			Provider:                "anthropic",
			ModelID:                 "claude-haiku-5-5",
			InputUSDPerMillion:      0.10,
			OutputUSDPerMillion:     0.50,
			CacheWriteUSDPerMillion: 0.125,
			CacheReadUSDPerMillion:  0.01,
		},
		"anthropic/claude-sonnet-5.5": {
			Provider:                "openrouter",
			ModelID:                 "anthropic/claude-sonnet-5.5",
			InputUSDPerMillion:      2.00,
			OutputUSDPerMillion:     10.00,
			CacheWriteUSDPerMillion: 2.50,
			CacheReadUSDPerMillion:  0.10,
		},
		"anthropic/claude-haiku-5.5": {
			Provider:                "openrouter",
			ModelID:                 "anthropic/claude-haiku-5.5",
			InputUSDPerMillion:      0.10,
			OutputUSDPerMillion:     0.50,
			CacheWriteUSDPerMillion: 0.125,
			CacheReadUSDPerMillion:  0.01,
		},
		"eleven_flash_v2_5": {
			Provider:                    "elevenlabs",
			ModelID:                     "eleven_flash_v2_5",
			NarrationUSDPerThousandChar: 0.04,
		},
	}
}
