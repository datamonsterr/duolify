package pricing

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"
)

func TestRouteProtectedCost(t *testing.T) {
	route := QuotePolicy{
		MaxAttempts:    2,
		ActionModels:   map[string]string{"preview": "primary"},
		FallbackModels: map[string]string{"primary": "fallback"},
		Models: map[string]ModelRate{
			"primary":  {Input: 35, Output: 141},
			"fallback": {Input: 40, Output: 79},
		},
	}
	// Input and output are independently protected at the highest rate in the route.
	got, err := routeProtectedCost(route, "preview", 2000, 6000)
	if err != nil || math.Abs(got-0.0022346232) > 1e-12 {
		t.Fatalf("Hy4/GLM protected cost = %g, %v", got, err)
	}
	if got > 25*0.0015 {
		t.Fatalf("preview exceeds protected budget: %g", got)
	}
	if err := enforceRouteBudget(route, "preview", 2000, 6000, 25, 0.0015); err != nil {
		t.Fatal(err)
	}
	route.MaxAttempts = 1
	route.Models["primary"] = ModelRate{Input: 2000, Output: 10000}
	got, err = routeProtectedCost(route, "preview", 2000, 2500)
	if err != nil || math.Abs(got-0.0349914) > 1e-12 {
		t.Fatalf("Sonnet protected cost = %g, %v", got, err)
	}
	route.MaxAttempts = 2
	got, err = routeProtectedCost(route, "preview", 2000, 6000)
	if err != nil || got <= 25*0.0015 {
		t.Fatalf("expensive route must exceed budget: %g, %v", got, err)
	}
	if err := enforceRouteBudget(route, "preview", 2000, 6000, 25, 0.0015); err == nil {
		t.Fatal("expensive route must be rejected")
	}
	delete(route.Models, "fallback")
	if _, err := routeProtectedCost(route, "preview", 2000, 6000); err == nil {
		t.Fatal("a missing fallback rate must fail closed")
	}
}

func TestEconomicsRequiresPinnedRoute(t *testing.T) {
	if err := ValidateEconomics([]byte(`{}`), &DevelopmentVerifier{}, ""); err == nil {
		t.Fatal("economics without a version-pinned route must fail closed")
	}
}

func TestRouteEconomicsRejectsExpensiveVersion(t *testing.T) {
	raw, err := os.ReadFile("fixtures/usd_character_create.json")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	config["limits"].(map[string]any)["preview"].(map[string]any)["output_tokens"] = 6000
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	policy := QuotePolicy{MaxAttempts: 2, ActionModels: map[string]string{}, FallbackModels: map[string]string{"primary": "fallback"}, Models: map[string]ModelRate{"primary": {Input: 35, Output: 141}, "fallback": {Input: 40, Output: 79}}}
	for _, name := range []string{"preview", "outline", "outline_replan", "lesson_content", "tutor_short", "tutor_long", "quick_note"} {
		policy.ActionModels[name] = "primary"
	}
	if _, err = checkEconomics(context.Background(), raw, &DevelopmentVerifier{}, "", defaultEconomicsPolicy(), false, policy); err != nil {
		t.Fatalf("approved route: %v", err)
	}
	policy.Models["primary"] = ModelRate{Input: 2000, Output: 10000}
	if _, err = checkEconomics(context.Background(), raw, &DevelopmentVerifier{}, "", defaultEconomicsPolicy(), false, policy); err == nil {
		t.Fatal("expensive route passed version economics")
	}
}
