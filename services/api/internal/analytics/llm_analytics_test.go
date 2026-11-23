package analytics

import (
	"math"
	"testing"
)

func TestAIGenerationAllowsOnlyBoundedGatewayProperties(t *testing.T) {
	valid := map[string]any{
		"transport_route": "direct", "gateway_cache_status": "bypass",
		"gateway_fallback": true, "gateway_reason": "timeout",
	}
	got := safeProperties("$ai_generation", valid)
	for key, value := range valid {
		if got[key] != value {
			t.Errorf("%s = %v, want %v", key, got[key], value)
		}
	}
	for key := range valid {
		valid[key] = "untrusted response text"
	}
	got = safeProperties("$ai_generation", valid)
	for key := range valid {
		if _, ok := got[key]; ok {
			t.Errorf("unexpected property %s", key)
		}
	}
}

// PostHog LLM analytics expects $ai_latency in seconds, a trace id and a USD cost.
func TestAIGenerationMapsToLLMAnalyticsSchema(t *testing.T) {
	got := safeProperties("$ai_generation", map[string]any{
		"source": "api", "platform": "web", "app_version": "0.1.0",
		"feature": "lesson", "provider": "openrouter", "model": "tencent/hy4-flash",
		"input_tokens": uint32(1200), "output_tokens": uint32(400), "latency_ms": uint32(2500),
		"provider_cost_nanos": uint64(63000), "succeeded": true, "fallback_used": false,
		"trace_id": "gen-abc123",
	})
	if v, ok := got["$ai_latency"].(float64); !ok || math.Abs(v-2.5) > 1e-9 {
		t.Fatalf("$ai_latency must be seconds, got %v", got["$ai_latency"])
	}
	if got["latency_ms"] != uint32(2500) {
		t.Fatalf("latency_ms kept for operators, got %v", got["latency_ms"])
	}
	if v, ok := got["$ai_total_cost_usd"].(float64); !ok || math.Abs(v-0.000063) > 1e-12 {
		t.Fatalf("$ai_total_cost_usd, got %v", got["$ai_total_cost_usd"])
	}
	if got["$ai_trace_id"] != "gen-abc123" || got["$ai_model"] != "tencent/hy4-flash" || got["$ai_provider"] != "openrouter" {
		t.Fatalf("trace/model/provider: %v", got)
	}
}

func TestAIGenerationGeneratesTraceIDAndSkipsUnknownCost(t *testing.T) {
	got := safeProperties("$ai_generation", map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "feature": "chat", "model": "z-ai/glm-5.3-flash"})
	if id, _ := got["$ai_trace_id"].(string); id == "" {
		t.Fatalf("missing generated trace id: %v", got)
	}
	if _, ok := got["$ai_total_cost_usd"]; ok {
		t.Fatal("cost must be absent when unknown")
	}
	if _, ok := got["$ai_latency"]; ok {
		t.Fatal("latency must be absent when unknown")
	}
}
