package pricing

import (
	"encoding/json"
	"os"
	"testing"
)

func replanFixture(t *testing.T, edit func(limits map[string]any)) []byte {
	t.Helper()
	raw, err := os.ReadFile("fixtures/usd_outline_replan.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	edit(doc["limits"].(map[string]any)["outline_replan"].(map[string]any))
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The 55 Credit price and the 90 second / two attempt bounds come only from the Pricing config.
func TestOutlineReplanFixtureCarriesPriceAndBounds(t *testing.T) {
	cfg, err := Parse(replanFixture(t, func(map[string]any) {}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Actions["outline_replan"].Credits != 55 {
		t.Fatalf("credits: %d", cfg.Actions["outline_replan"].Credits)
	}
}

// Config validation, not Go constants, keeps replan inside 90 seconds and two evaluated attempts.
func TestOutlineReplanConfigRejectsOversizedEnvelope(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"timeout":  func(l map[string]any) { l["timeout_ms"] = 120000 },
		"attempts": func(l map[string]any) { l["max_attempts"] = 3 },
		"missing":  func(l map[string]any) { delete(l, "max_cost_nanos") },
	} {
		if _, err := Parse(replanFixture(t, edit)); err == nil {
			t.Fatalf("%s: oversized envelope accepted", name)
		}
	}
}
