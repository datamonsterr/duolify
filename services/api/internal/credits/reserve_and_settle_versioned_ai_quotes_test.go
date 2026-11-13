package credits_test

import (
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

func settlementQuote() pricing.Quote {
	return pricing.Quote{Action: "lesson_content", PricingVersion: "original", ContentCeiling: 140, ContentMinimum: 100, AudioCeiling: 50, NarrationNumerator: 50, NarrationDenominator: 1440,
		Bounds: map[string]int64{"input_tokens": 8000, "output_tokens": 14000, "narration_characters": 1440, "max_attempts": 2, "timeout_ms": 180000, "max_cost_nanos": 500000000},
		Rate:   pricing.ModelRate{Provider: "anthropic", Model: "sonnet", ContextMax: 100000, Input: 2000, Output: 10000, CacheWrite: 2500, CacheRead: 100},
		Policy: pricing.QuotePolicy{USDNanosPerCredit: 1500000, AudioUSDNanosPerCharacter: 40000, AudioModel: "tts"}, CreatedAt: time.Now()}
}
func TestVersionedSettlementAmounts(t *testing.T) {
	q := settlementQuote()
	cases := []struct {
		name           string
		usage          []*enginev1.Usage
		content, audio int64
		bad            bool
	}{
		{name: "failure releases", usage: nil},
		{name: "minimum", usage: []*enginev1.Usage{{Provider: "anthropic", Model: "sonnet", Attempt: 1, Component: "content", Delivered: true, InputTokens: 10}}, content: 100},
		{name: "ceiling", usage: []*enginev1.Usage{{Provider: "anthropic", Model: "sonnet", Attempt: 1, Component: "content", Delivered: true, OutputTokens: 14000}}, content: 100},
		{name: "audio rounds aggregate once", usage: []*enginev1.Usage{{Provider: "elevenlabs", Model: "tts", Attempt: 1, Component: "audio", Delivered: true, NarrationCharacters: 1}, {Provider: "elevenlabs", Model: "tts", Attempt: 1, Component: "audio", Delivered: true, NarrationCharacters: 1}}, audio: 1},
		{name: "cached replay free", usage: []*enginev1.Usage{{Component: "content", Cached: true, Delivered: true}}},
		{name: "unknown route", usage: []*enginev1.Usage{{Provider: "invented", Model: "sonnet", Attempt: 1, Component: "content", Delivered: true}}, bad: true},
		{name: "output overflow", usage: []*enginev1.Usage{{Provider: "anthropic", Model: "sonnet", Attempt: 1, Component: "content", Delivered: true, OutputTokens: 14001}}, bad: true},
		{name: "attempt overflow", usage: []*enginev1.Usage{{Provider: "anthropic", Model: "sonnet", Attempt: 3, Component: "content"}}, bad: true},
		{name: "failed content billed to platform only", usage: []*enginev1.Usage{{Provider: "anthropic", Model: "sonnet", Attempt: 1, Component: "content", InputTokens: 100}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := credits.CalculateSettlement(q, tc.usage)
			if (err != nil) != tc.bad {
				t.Fatalf("error: %v", err)
			}
			if err == nil && (result.ContentSpent != tc.content || result.AudioSpent != tc.audio) {
				t.Fatalf("result: %+v", result)
			}
		})
	}
}

func TestSettlementCeilingAndPlatformAttemptCosts(t *testing.T) {
	q := settlementQuote()
	q.Policy.USDNanosPerCredit = 500000
	result, err := credits.CalculateSettlement(q, []*enginev1.Usage{{Provider: "anthropic", Model: "sonnet", Attempt: 1, Component: "content", Delivered: true, OutputTokens: 14000}})
	if err != nil || result.ContentSpent != 140 {
		t.Fatalf("ceiling=%+v %v", result, err)
	}
	result, err = credits.CalculateSettlement(q, []*enginev1.Usage{{Provider: "anthropic", Model: "sonnet", Attempt: 1, Component: "content", InputTokens: 100, UsageUnavailable: true}})
	if err != nil || result.ContentSpent != 0 || result.CostNanos != 160000000 {
		t.Fatalf("unknown attempt=%+v %v", result, err)
	}
}
