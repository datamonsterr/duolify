package pricing_test

import (
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

func TestTutorLargeContextQuoteIsExplicitAndBoundedByConfig(t *testing.T) {
	c, err := pricing.Parse(seed(t))
	if err != nil {
		t.Fatal(err)
	}
	p := pricing.QuotePolicy{TTLSeconds: 600, USDNanosPerCredit: 1500000, MaxAttempts: 2, Models: map[string]pricing.ModelRate{"haiku": {Provider: "anthropic", Model: "claude-haiku-5-5", ContextMax: 200000, Input: 1000, Output: 5000, CacheWrite: 1250, CacheRead: 100}}, ActionModels: map[string]string{"tutor_short": "haiku"}}
	standard, err := pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "tutor_short", InputTokens: 10, ContextTokens: 11000}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	cached, prompt := standard.Bounds["cached_context_tokens"], standard.Bounds["prompt_tokens"]
	if cached != 12000 || prompt != 48000 {
		t.Fatalf("config bounds %v", standard.Bounds)
	}
	if _, err = pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "tutor_short", InputTokens: 10, ContextTokens: cached + 1}, time.Now()); err == nil {
		t.Fatal("a context above the cached bound must need explicit approval")
	}
	large, err := pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "tutor_short", InputTokens: 10, ContextTokens: 30000, LargeContext: true}, time.Now())
	if err != nil || !large.Input.LargeContext {
		t.Fatal("approved quote", err)
	}
	if large.Bounds["input_tokens"] != standard.Bounds["input_tokens"]-cached+30000 {
		t.Fatalf("input bound must follow the approved context: %d vs %d", large.Bounds["input_tokens"], standard.Bounds["input_tokens"])
	}
	if large.ContentCeiling != standard.ContentCeiling {
		t.Fatal("the credit price must not change with approval")
	}
	if _, err = pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "tutor_short", InputTokens: 10, ContextTokens: prompt, LargeContext: true}, time.Now()); err == nil {
		t.Fatal("approval cannot exceed the configured prompt limit")
	}
	p.ActionModels["quick_note"] = "haiku"
	if _, err = pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "quick_note", InputTokens: 10}, time.Now()); err != nil {
		t.Fatal("control quote must succeed", err)
	}
	if _, err = pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "quick_note", InputTokens: 10, LargeContext: true}, time.Now()); err == nil {
		t.Fatal("large context applies to the tutor only")
	}
}
