package pricing

import "testing"

func TestPolicyCoversActionsRejectsUnratedRoutes(t *testing.T) {
	rate := ModelRate{Provider: "openrouter", Model: "m", ContextMax: 1, Input: 1, Output: 1, CacheWrite: 1, CacheRead: 1}
	cfg := Config{Actions: map[string]Action{"preview": {}, "narration": {}, "cached_replay": {}}}
	ok := QuotePolicy{ActionModels: map[string]string{"preview": "a"}, Models: map[string]ModelRate{"a": rate, "b": rate}, FallbackModels: map[string]string{"a": "b", "b": "a"}}
	if err := policyCoversActions(ok, cfg); err != nil {
		t.Fatalf("rated route with a fallback cycle: %v", err)
	}
	for name, p := range map[string]QuotePolicy{
		"no model for the action": {Models: map[string]ModelRate{"a": rate}},
		"unrated primary":         {ActionModels: map[string]string{"preview": "x"}, Models: map[string]ModelRate{"a": rate}},
		"unrated fallback":        {ActionModels: map[string]string{"preview": "a"}, Models: map[string]ModelRate{"a": rate}, FallbackModels: map[string]string{"a": "gone"}},
	} {
		if err := policyCoversActions(p, cfg); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestQuotePolicySourceIsNotAPriceTerm(t *testing.T) {
	before := []byte(`{"version":"a","actions":{"preview":{"credits":1}}}`)
	after := []byte(`{"version":"b","quote_policy_source":"usd-hy4","actions":{"preview":{"credits":1}}}`)
	adverse, err := classifyAdverse(before, after)
	if err != nil || adverse {
		t.Fatalf("adding a policy source must not be adverse: %v %v", adverse, err)
	}
	if got := (Config{QuotePolicySource: "usd-hy4"}).Public().QuotePolicySource; got != "" {
		t.Fatalf("public catalogue exposes the policy source: %q", got)
	}
}
