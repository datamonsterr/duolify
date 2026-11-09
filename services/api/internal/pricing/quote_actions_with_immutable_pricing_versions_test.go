package pricing_test

import (
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"testing"
	"time"
)

func TestQuoteRationalAndBounds(t *testing.T) {
	c, err := pricing.Parse(seed(t))
	if err != nil {
		t.Fatal(err)
	}
	p := pricing.QuotePolicy{TTLSeconds: 600, AudioUSDNanosPerCharacter: 40000, USDNanosPerCredit: 1500000, Models: map[string]pricing.ModelRate{"sonnet": {Provider: "anthropic", Model: "claude-sonnet-5-5", ContextMax: 100000, Input: 2000, Output: 10000, CacheWrite: 2500, CacheRead: 100}}, ActionModels: map[string]string{"lesson_content": "sonnet"}, Voices: []string{"default"}, AudioModel: "eleven_flash_v2_5"}
	for _, test := range []struct{ chars, want int64 }{{0, 0}, {1, 1}, {1440, 50}, {1439, 50}} {
		q, err := pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: test.chars}, time.Now())
		if err != nil || q.AudioCeiling != test.want {
			t.Fatalf("ceil %+v %v", q, err)
		}
	}
	for _, input := range []pricing.QuoteInput{{Action: "missing"}, {Action: "lesson_content", NarrationCharacters: -1}, {Action: "lesson_content", NarrationCharacters: 1441}, {Action: "lesson_content", InputTokens: 8001}, {Action: "lesson_content", Model: "unknown"}, {Action: "lesson_content", Voice: "premium", NarrationCharacters: 1}} {
		if _, err := pricing.CalculateQuote(c, p, input, time.Now()); err == nil {
			t.Fatalf("accepted invalid %+v", input)
		}
	}
	// Huge rational denominators cannot overflow the single rounding step.
	c.Narration.Characters = int(^uint(0) >> 1)
	q, err := pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: 1440}, time.Now())
	if err != nil || q.AudioCeiling != 1 {
		t.Fatal("rational overflow", q.AudioCeiling, err)
	}
	p.Models = nil
	if _, err := pricing.CalculateQuote(c, p, pricing.QuoteInput{Action: "lesson_content"}, time.Now()); err == nil {
		t.Fatal("invented missing rate")
	}
}
