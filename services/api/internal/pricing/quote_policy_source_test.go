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

func TestQuotePolicySourceDoesNotByItselfDescribeTerms(t *testing.T) {
	before := []byte(`{"version":"a","actions":{"preview":{"credits":1}}}`)
	after := []byte(`{"version":"b","quote_policy_source":"usd-hy4","actions":{"preview":{"credits":1}}}`)
	adverse, err := classifyAdverse(before, after)
	if err != nil || adverse {
		t.Fatalf("source name alone does not describe its terms: %v %v", adverse, err)
	}
	if got := (Config{QuotePolicySource: "usd-hy4"}).Public().QuotePolicySource; got != "" {
		t.Fatalf("public catalogue exposes the policy source: %q", got)
	}
}

func TestEffectiveQuotePolicyAdverseTerms(t *testing.T) {
	rate := ModelRate{Provider: "a", Model: "a", ContextMax: 100, Input: 10, Output: 10, CacheWrite: 10, CacheRead: 10}
	base := QuotePolicy{FreeConcurrency: 2, MaxAttempts: 2, TTLSeconds: 300, USDNanosPerCredit: 100, AudioUSDNanosPerCharacter: 10, Voices: []string{"default"}, ActionModels: map[string]string{"preview": "a"}, Models: map[string]ModelRate{"a": rate}}
	cases := []struct {
		name   string
		mutate func(*QuotePolicy)
		want   bool
	}{
		{"higher provider rate", func(p *QuotePolicy) { r := p.Models["a"]; r.Input++; p.Models["a"] = r }, true},
		{"less credit value", func(p *QuotePolicy) { p.USDNanosPerCredit-- }, true},
		{"fewer attempts", func(p *QuotePolicy) { p.MaxAttempts-- }, true},
		{"invalid rate", func(p *QuotePolicy) { r := p.Models["a"]; r.Output = 0; p.Models["a"] = r }, true},
		{"less context", func(p *QuotePolicy) { r := p.Models["a"]; r.ContextMax--; p.Models["a"] = r }, true},
		{"route only", func(p *QuotePolicy) {
			r := p.Models["a"]
			r.Provider = "b"
			r.Model = "b"
			p.Models = map[string]ModelRate{"b": r}
			p.ActionModels = map[string]string{"preview": "b"}
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			p.Models = map[string]ModelRate{"a": rate}
			tc.mutate(&p)
			if got := adverseQuotePolicy(base, p); got != tc.want {
				t.Fatalf("adverse=%v want %v", got, tc.want)
			}
		})
	}
}

func TestEffectivePolicyIncludesImplicitDesignChatRoute(t *testing.T) {
	c := Config{Actions: map[string]Action{"setup_chat": {}}}
	p := QuotePolicy{Models: map[string]ModelRate{"haiku": {Provider: "p", Model: "m", Input: 1, Output: 1, CacheRead: 1, CacheWrite: 1, ContextMax: 1}}}
	got := effectivePolicyActions(p, c)
	if got.ActionModels["setup_chat"] != "haiku" {
		t.Fatalf("implicit route omitted: %+v", got.ActionModels)
	}
}
