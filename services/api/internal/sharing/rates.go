package sharing

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PricingRates reads screening rates from the active Pricing config: the platform screening route
// (haiku) and its fallback, taking the larger of each rate so the USD cap is conservative.
type PricingRates struct{ Pool *pgxpool.Pool }

// Rates returns the larger of the primary and fallback screening rates.
func (p PricingRates) Rates(ctx context.Context) (Rates, error) {
	row, err := db.New(p.Pool).ActiveQuoteConfig(ctx)
	if err != nil {
		return Rates{}, err
	}
	var policy pricing.QuotePolicy
	if json.Unmarshal(row.Policy, &policy) != nil {
		return Rates{}, errors.New("pricing policy unreadable")
	}
	a, b := policy.Models["haiku"], policy.Models[policy.FallbackModels["haiku"]]
	r := Rates{Input: boundedUint64(max(a.Input, b.Input)), Output: boundedUint64(max(a.Output, b.Output)), CacheWrite: boundedUint64(max(a.CacheWrite, b.CacheWrite)), CacheRead: boundedUint64(max(a.CacheRead, b.CacheRead))}
	if r.Input == 0 || r.Output == 0 || r.CacheWrite == 0 || r.CacheRead == 0 {
		return Rates{}, errors.New("screening rates unavailable")
	}
	return r, nil
}

// UnknownAge fails closed until an account age source exists (ADR 0012: publishing is 18+).
type UnknownAge struct{}

// MeetsMinAge is always false: publishing waits for a real age source.
func (UnknownAge) MeetsMinAge(context.Context, string, int) (bool, error) { return false, nil }
