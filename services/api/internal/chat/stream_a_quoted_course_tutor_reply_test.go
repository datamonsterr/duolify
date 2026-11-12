package chat

import (
	"net/http/httptest"
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

func TestValidateMessage(t *testing.T) {
	for name, tc := range map[string]struct {
		class, msg string
		ok         bool
	}{
		"short ok":       {"short", "hi", true},
		"blank":          {"short", "  \n", false},
		"nul":            {"short", "a\x00b", false},
		"bad class":      {"huge", "hi", false},
		"short too long": {"short", strings.Repeat("a", shortMessageBytes+1), false},
		"long ok":        {"long", strings.Repeat("a", shortMessageBytes+1), true},
		"long too long":  {"long", strings.Repeat("a", longMessageBytes+1), false},
		"invalid utf8":   {"short", "\xff\xfe", false},
	} {
		if err := validateMessage(tc.class, tc.msg); (err == nil) != tc.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func testQuote() pricing.Quote {
	rate := pricing.ModelRate{Provider: "anthropic", Model: "haiku", Input: 1000, Output: 5000, CacheWrite: 1250, CacheRead: 100}
	return pricing.Quote{ContentMinimum: 5, ContentCeiling: 5, Policy: pricing.QuotePolicy{USDNanosPerCredit: 1_000_000}, Input: pricing.QuoteInput{Model: "haiku"}, Rate: rate, Bounds: map[string]int64{"input_tokens": 12500, "output_tokens": 250, "max_attempts": 2, "timeout_ms": 30000, "max_cost_nanos": 100000000}}
}

func TestSettlementUsageNormalisesCompleteAndInterruptedReplies(t *testing.T) {
	q := testQuote()
	complete := settlementUsage(q, 4000, "Hello world", &enginev1.Usage{Provider: "anthropic", Model: "haiku", InputTokens: 120, OutputTokens: 30})
	if len(complete) != 1 || complete[0].Component != "content" || complete[0].Attempt != 1 || !complete[0].Delivered || complete[0].OutputTokens != 30 || complete[0].FallbackUsed {
		t.Fatalf("complete usage %+v", complete)
	}
	if _, err := credits.CalculateSettlement(q, complete); err != nil {
		t.Fatal(err)
	}
	interrupted := settlementUsage(q, 4000, strings.Repeat("x", 90000), nil)
	if !interrupted[0].Delivered || interrupted[0].UsageUnavailable || int64(interrupted[0].OutputTokens) != q.Bounds["output_tokens"] {
		t.Fatalf("estimate must clamp to quote bounds: %+v", interrupted[0])
	}
	res, err := credits.CalculateSettlement(q, interrupted)
	if err != nil || res.CostNanos <= 0 {
		t.Fatalf("interrupted usage must be settleable: %+v %v", res, err)
	}
	fb := settlementUsage(q, 100, "x", &enginev1.Usage{Provider: "openrouter", Model: "other"})
	if !fb[0].FallbackUsed {
		t.Fatal("a different provider must be marked as fallback")
	}
}

func TestSSEFramingEscapesNewlinesInData(t *testing.T) {
	rec := httptest.NewRecorder()
	writeEvent(rec, "delta", map[string]any{"text": "line1\nline2"})
	if got := rec.Body.String(); got != "event: delta\ndata: {\"text\":\"line1\\nline2\"}\n\n" {
		t.Fatalf("frame %q", got)
	}
}

func TestClassActionAndContextTokens(t *testing.T) {
	if classAction("short") != "tutor_short" || classAction("long") != "tutor_long" || classAction("x") != "" {
		t.Fatal("class mapping")
	}
	if estimateTokens(0) != 0 || estimateTokens(1) != 1 || estimateTokens(8) != 2 || estimateTokens(9) != 3 {
		t.Fatal("token estimate rounds up at 4 bytes per token")
	}
}
