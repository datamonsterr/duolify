package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
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

// StripeVerifier retrieves canonical Stripe prices; it never trusts draft evidence.
type StripeVerifier struct {
	keys   map[string]string
	client *http.Client
}

// NewStripeVerifier creates a canonical-price verifier with separate test/live credentials.
func NewStripeVerifier(testKey, liveKey string, client *http.Client) *StripeVerifier {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &StripeVerifier{map[string]string{"test": testKey, "live": liveKey}, client}
}

// VerifyStripePrice checks the canonical Stripe response against the configured offer.
func (v *StripeVerifier) VerifyStripePrice(ctx context.Context, env, id string, expected Offer, interval string) error {
	key := v.keys[env]
	if key == "" {
		return fmt.Errorf("stripe credentials missing for %s", env)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.stripe.com/v1/prices/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	request.SetBasicAuth(key, "")
	request.Header.Set("Stripe-Version", "2026-10-28.endive")
	response, err := v.client.Do(request)
	if err != nil {
		return errors.New("stripe price lookup unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return ErrStripePriceNotFound
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("stripe price lookup failed (HTTP %d)", response.StatusCode)
	}
	var price struct {
		ID        string `json:"id"`
		Active    *bool  `json:"active"`
		Currency  string `json:"currency"`
		Amount    *int   `json:"unit_amount"`
		Live      *bool  `json:"livemode"`
		Type      string `json:"type"`
		Recurring *struct {
			Interval string `json:"interval"`
			Count    int    `json:"interval_count"`
			Usage    string `json:"usage_type"`
		} `json:"recurring"`
		BillingScheme string          `json:"billing_scheme"`
		Transform     json.RawMessage `json:"transform_quantity"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&price); err != nil {
		return errors.New("invalid Stripe price response")
	}
	if price.ID != id || price.Active == nil || price.Live == nil || price.Amount == nil || price.BillingScheme != "per_unit" || (len(price.Transform) > 0 && string(price.Transform) != "null") {
		return errors.New("unsupported or incomplete Stripe price")
	}
	actualEnv := "test"
	if *price.Live {
		actualEnv = "live"
	}
	record := StripePriceRecord{ID: price.ID, Active: *price.Active, Currency: price.Currency, AmountMinor: *price.Amount, RecurringType: price.Type, Environment: actualEnv}
	if price.Recurring != nil {
		record.Interval = price.Recurring.Interval
		record.IntervalCount = price.Recurring.Count
		if price.Recurring.Usage != "licensed" {
			return errors.New("unsupported Stripe usage type")
		}
	}
	if record.RecurringType != "one_time" && record.RecurringType != "recurring" {
		return errors.New("unsupported Stripe price type")
	}
	return record.Matches(env, expected, interval)
}

// VerifyStoreMapping validates store identifier syntax.
func (*StripeVerifier) VerifyStoreMapping(_ context.Context, channel, id string) error {
	return ValidateStoreProductID(channel, id)
}

// DevelopmentVerifier only recognizes explicit synthetic mappings. Startup must
// opt in through ALLOW_FAKE_PRICING_VERIFIER in development/test.
type DevelopmentVerifier struct{}

// VerifyStripePrice recognizes only explicit synthetic test identifiers.
func (*DevelopmentVerifier) VerifyStripePrice(_ context.Context, env, id string, expected Offer, _ string) error {
	if env != "test" || id != "synthetic:"+expected.SKU {
		return errors.New("fake pricing verifier accepts only synthetic test mappings")
	}
	return nil
}

// VerifyStoreMapping validates store identifier syntax in development.
func (*DevelopmentVerifier) VerifyStoreMapping(_ context.Context, channel, id string) error {
	return ValidateStoreProductID(channel, id)
}

// ConfigureVerification wires both HTTP and CLI through the same operator policy.
func (a *Admin) ConfigureVerification(getenv func(string) string) error {
	verifier, err := VerifierFromEnvironment(getenv)
	if err != nil {
		return err
	}
	a.verifier = verifier
	policy := defaultEconomicsPolicy()
	if raw := getenv("PRICING_ECONOMICS_MINIMUMS"); raw != "" {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&policy); err != nil {
			return errors.New("invalid PRICING_ECONOMICS_MINIMUMS")
		}
		defaults := defaultEconomicsPolicy()
		if math.IsNaN(policy.ProtectedCostCeiling) || policy.ProtectedCostCeiling < defaults.ProtectedCostCeiling || policy.MarginFloor < 70 || policy.MarginFloor > 100 || policy.Fees.PackFeeRate < defaults.Fees.PackFeeRate || policy.Fees.PackFixedFeeUSD < defaults.Fees.PackFixedFeeUSD || policy.Fees.SubscriptionFeeRate < defaults.Fees.SubscriptionFeeRate || policy.Fees.SubscriptionFixedFeeUSD < defaults.Fees.SubscriptionFixedFeeUSD || policy.Fees.RefundReserveRate < defaults.Fees.RefundReserveRate {
			return errors.New("platform economics cannot weaken minimum protections")
		}
	}
	a.policy = policy
	return nil
}

// VerifierFromEnvironment selects real Stripe verification or an explicitly gated fake.
func VerifierFromEnvironment(getenv func(string) string) (ExternalVerifier, error) {
	fake := false
	if raw := getenv("ALLOW_FAKE_PRICING_VERIFIER"); raw != "" {
		var err error
		fake, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, errors.New("invalid ALLOW_FAKE_PRICING_VERIFIER")
		}
	}
	if fake {
		env := getenv("DUOLIFY_ENV")
		if env != "development" && env != "test" {
			return nil, errors.New("fake pricing verifier requires explicit development or test environment")
		}
		if getenv("STRIPE_PRICING_TEST_SECRET_KEY") != "" || getenv("STRIPE_PRICING_LIVE_SECRET_KEY") != "" {
			return nil, errors.New("fake pricing verifier cannot be combined with Stripe credentials")
		}
		return &DevelopmentVerifier{}, nil
	}
	if getenv("STRIPE_PRICING_TEST_SECRET_KEY") == "" && getenv("STRIPE_PRICING_LIVE_SECRET_KEY") == "" {
		return nil, nil
	}
	return NewStripeVerifier(getenv("STRIPE_PRICING_TEST_SECRET_KEY"), getenv("STRIPE_PRICING_LIVE_SECRET_KEY"), nil), nil
}
