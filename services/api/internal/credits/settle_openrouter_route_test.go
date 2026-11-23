package credits_test

import (
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
)

// routeQuote quotes lesson_content on tencent/hy4-flash with z-ai/glm-5.3-flash as fallback (ADR 0015).
func routeQuote() pricing.Quote {
	hy4 := pricing.ModelRate{Provider: "openrouter", Model: "tencent/hy4-flash", ContextMax: 1048576, Input: 35, Output: 141, CacheWrite: 35, CacheRead: 2}
	glm := pricing.ModelRate{Provider: "openrouter", Model: "z-ai/glm-5.3-flash", ContextMax: 1048576, Input: 40, Output: 79, CacheWrite: 40, CacheRead: 30}
	q := settlementQuote()
	q.Rate = hy4
	q.Input.Model = "hy4_flash"
	q.Policy.Models = map[string]pricing.ModelRate{"hy4_flash": hy4, "glm_flash": glm}
	q.Policy.FallbackModels = map[string]string{"hy4_flash": "glm_flash"}
	return q
}

func contentUsage(model string, fallback bool, in, out uint32) *enginev1.Usage {
	return &enginev1.Usage{Provider: "openrouter", Model: model, Attempt: 1, Component: "content", Delivered: true, FallbackUsed: fallback, InputTokens: in, OutputTokens: out}
}

func TestSettlementChargesTheServedModelOfTheQuotedRoute(t *testing.T) {
	q := routeQuote()
	cases := []struct {
		name  string
		usage []*enginev1.Usage
		cost  int64
		bad   bool
	}{
		{name: "primary served", usage: []*enginev1.Usage{contentUsage("tencent/hy4-flash", false, 1000, 400)}, cost: 1000*35 + 400*141},
		{name: "fallback served at its own rate", usage: []*enginev1.Usage{contentUsage("z-ai/glm-5.3-flash", true, 1000, 400)}, cost: 1000*40 + 400*79},
		{name: "reasoning tokens bill as output", usage: []*enginev1.Usage{contentUsage("tencent/hy4-flash", false, 100, 1822)}, cost: 100*35 + 1822*141},
		{name: "cache read uses the served model rate", usage: []*enginev1.Usage{{Provider: "openrouter", Model: "z-ai/glm-5.3-flash", Attempt: 1, Component: "content", Delivered: true, FallbackUsed: true, CacheReadTokens: 1000}}, cost: 1000 * 30},
		{name: "unknown model rejected", usage: []*enginev1.Usage{contentUsage("anthropic/claude-haiku-5.5", true, 10, 10)}, bad: true},
		{name: "known model on another provider rejected", usage: []*enginev1.Usage{{Provider: "anthropic", Model: "z-ai/glm-5.3-flash", Attempt: 1, Component: "content", FallbackUsed: true}}, bad: true},
		{name: "fallback without the fallback flag rejected", usage: []*enginev1.Usage{contentUsage("z-ai/glm-5.3-flash", false, 10, 10)}, bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := credits.CalculateSettlement(q, tc.usage)
			if (err != nil) != tc.bad {
				t.Fatalf("error: %v", err)
			}
			if err == nil && got.CostNanos != tc.cost {
				t.Fatalf("cost %d, want %d", got.CostNanos, tc.cost)
			}
		})
	}
}

func TestSettlementAcceptsEveryModelOfALongerRouteAndIgnoresCycles(t *testing.T) {
	q := routeQuote()
	third := pricing.ModelRate{Provider: "openrouter", Model: "vendor/third", ContextMax: 1048576, Input: 50, Output: 90, CacheWrite: 50, CacheRead: 5}
	q.Policy.Models["third"] = third
	q.Policy.FallbackModels["glm_flash"] = "third"
	q.Policy.FallbackModels["third"] = "hy4_flash" // cycle back to the primary
	got, err := credits.CalculateSettlement(q, []*enginev1.Usage{contentUsage("vendor/third", true, 100, 100)})
	if err != nil || got.CostNanos != 100*50+100*90 {
		t.Fatalf("third hop: %+v %v", got, err)
	}
	// Unknown usage reserves the dearest input and output rate of the whole route.
	got, err = credits.CalculateSettlement(q, []*enginev1.Usage{{Provider: "openrouter", Model: "tencent/hy4-flash", Attempt: 1, Component: "content", UsageUnavailable: true}})
	if err != nil || got.CostNanos != 8000*50+14000*141 {
		t.Fatalf("worst case: %+v %v", got, err)
	}
}

func TestGatewayRetryReservesPossibleUpstreamCostWithoutChangingCredits(t *testing.T) {
	q := routeQuote()
	u := contentUsage("tencent/hy4-flash", false, 1000, 400)
	usage := []*enginev1.Usage{u}
	baseline, err := credits.CalculateSettlement(q, usage)
	if err != nil {
		t.Fatal(err)
	}
	u.OpenrouterTransport = &enginev1.OpenRouterTransport{
		Route: enginev1.OpenRouterTransport_ROUTE_DIRECT, GatewayFallback: true,
		GatewayReason: enginev1.OpenRouterTransport_REASON_TIMEOUT, GatewayMayBill: true,
	}
	worst := q.WorstRate()
	possibleCost := q.Bounds["input_tokens"]*max(worst.Input, worst.CacheWrite, worst.CacheRead) + q.Bounds["output_tokens"]*worst.Output
	got, err := credits.CalculateSettlement(q, usage)
	if err != nil || got.CostNanos != baseline.CostNanos+possibleCost || got.ContentSpent != baseline.ContentSpent {
		t.Fatalf("transport fallback settlement: %+v, %v; baseline %+v", got, err, baseline)
	}
	q.Bounds["max_cost_nanos"] = got.CostNanos - 1
	if _, err := credits.CalculateSettlement(q, usage); err == nil {
		t.Fatal("expected the existing cost ceiling to include the possible Gateway charge")
	}
}
