package credits_test

import (
	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"testing"
	"time"
)

func TestE7ReviewDailyWalletByGrantDate(t *testing.T) {
	at := time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)
	old := map[string]any{"bucket": "daily_free", "date": "2026-11-09"}
	today := map[string]any{"bucket": "daily_free", "date": "2026-11-10"}
	entries := []credits.Entry{
		{Kind: "daily_grant", Amount: 46, Provenance: old},
		{Kind: "reserve", Amount: -15, Reference: "old-job", Provenance: old},
		{Kind: "expire", Amount: -31, Provenance: old},
		{Kind: "daily_grant", Amount: 46, Provenance: today},
		{Kind: "release", Amount: 15, Reference: "old-job", Provenance: old},
		{Kind: "spend", Amount: -8, Provenance: old},
		{Kind: "expire", Amount: -7, Provenance: old},
	}
	got := credits.CalculateWalletAt(entries, "UTC", at)
	if got.DailyFree.Available != 46 || got.Reserved != 0 {
		t.Fatalf("yesterday reduced today: %+v", got)
	}
	// The projection must not depend on row ordering inside another grant date.
	entries[2], entries[3] = entries[3], entries[2]
	got = credits.CalculateWalletAt(entries, "UTC", at)
	if got.DailyFree.Available != 46 {
		t.Fatalf("reordered expiry reduced today: %+v", got)
	}
}

func TestE7ReviewDeliveredUnknownUsageRetainsQuote(t *testing.T) {
	q := settlementQuote()
	q.ContentMinimum = 1
	got, err := credits.CalculateSettlement(q, []*enginev1.Usage{{Provider: q.Rate.Provider, Model: q.Rate.Model, Component: "content", Attempt: 1, Delivered: true, UsageUnavailable: true}})
	if err != nil || got.ContentSpent != q.ContentCeiling || got.CostNanos <= 0 {
		t.Fatalf("unavailable usage discounted delivery: %+v %v", got, err)
	}
}
