package pricing

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func characterPricingConfig() map[string]any {
	cfg := sampleValidConfig()
	cfg["actions"].(map[string]any)["character_create"] = map[string]any{"credits": 25}
	cfg["limits"].(map[string]any)["character_create"] = map[string]any{
		"input_tokens": 4000, "output_tokens": 2000, "timeout_ms": 90000, "max_attempts": 2, "max_cost_nanos": 50000000,
		"count": 10, "day_attempts": 5, "minute_attempts": 2, "prompt_chars": 1500,
		"screen_input_tokens": 18000, "screen_output_tokens": 200, "screen_timeout_ms": 10000, "screen_max_attempts": 2,
	}
	return cfg
}

func TestCharacterPricingMissingDecisionHidesAction(t *testing.T) {
	cfg, err := Parse(mustJSON(t, characterPricingConfig()))
	if err != nil {
		t.Fatal(err)
	}
	if FromConfig(cfg).HasAction("character_create") {
		t.Fatal("unapproved character price advertised as available")
	}
}

func TestCharacterPricingRejectsDailyFreeEligibility(t *testing.T) {
	cfg := characterPricingConfig()
	cfg["daily_free"].(map[string]any)["eligible_actions"] = []string{"quick_note", "tutor_short", "tutor_long", "character_create"}
	if _, err := Parse(mustJSON(t, cfg)); err == nil {
		t.Fatal("character_create accepted daily-free funding")
	}
}

func TestCharacterPricingEnforcesApprovedBounds(t *testing.T) {
	for key, max := range map[string]int{"count": 10, "day_attempts": 5, "minute_attempts": 2, "prompt_chars": 1500, "output_tokens": 2000, "timeout_ms": 90000, "max_attempts": 2, "screen_input_tokens": 18000, "screen_output_tokens": 200, "screen_timeout_ms": 10000, "screen_max_attempts": 2} {
		for _, value := range []int{0, max + 1} {
			cfg := characterPricingConfig()
			cfg["limits"].(map[string]any)["character_create"].(map[string]any)[key] = value
			if _, err := Parse(mustJSON(t, cfg)); err == nil {
				t.Errorf("accepted %s=%d", key, value)
			}
		}
	}
	cfg := characterPricingConfig()
	delete(cfg["limits"].(map[string]any), "character_create")
	if _, err := Parse(mustJSON(t, cfg)); err == nil {
		t.Fatal("accepted key without envelope")
	}
}

func TestCharacterPricingFixtureAndConfigOwnedQuote(t *testing.T) {
	raw, err := os.ReadFile("fixtures/usd_character_create.json")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Version != "usd-character-create-2026-11-16" || cfg.Actions["character_create"].Credits != 25 || !FromConfig(cfg).HasAction("character_create") {
		t.Fatalf("fixture=%+v", cfg)
	}
	p := QuotePolicy{TTLSeconds: 300, USDNanosPerCredit: 1500000, MaxAttempts: 2, FreeConcurrency: 1,
		Models:       map[string]ModelRate{"haiku": {Provider: "mock", Model: "fixture", ContextMax: 100000, Input: 100, Output: 500, CacheWrite: 125, CacheRead: 10}},
		ActionModels: map[string]string{"character_create": "haiku"},
	}
	for _, price := range []int{25, 31} {
		cfg.Actions["character_create"] = Action{Credits: price}
		q, err := CalculateQuote(cfg, p, QuoteInput{Action: "character_create"}, time.Now())
		if err != nil || q.ContentCeiling != int64(price) || q.DailyFreeEligible || q.Bounds["count"] != 10 || q.Bounds["day_attempts"] != 5 || q.Bounds["minute_attempts"] != 2 {
			t.Fatalf("quote=%+v %v", q, err)
		}
	}
	var fixture map[string]any
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture["character_create_decision"] != "owner_approved" {
		t.Fatal("fixture lacks owner decision")
	}
	if _, ok := fixture["actions"].(map[string]any)["setup_chat"]; !ok {
		t.Fatal("character fixture dropped approved design-chat pricing")
	}
	baseRaw, err := os.ReadFile("fixtures/usd_design_chat_owner_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	var base map[string]any
	if err := json.Unmarshal(baseRaw, &base); err != nil {
		t.Fatal(err)
	}
	delete(fixture["actions"].(map[string]any), "character_create")
	delete(fixture["limits"].(map[string]any), "character_create")
	delete(fixture, "character_create_decision")
	for _, doc := range []map[string]any{base, fixture} {
		delete(doc, "version")
		delete(doc, "effective_from")
	}
	if !jsonEqual(base, fixture) {
		t.Fatal("character fixture changed existing approved pricing terms")
	}
}

func TestCharacterPricingSupersetPreservesProductionDesignChatGate(t *testing.T) {
	characterRaw, err := os.ReadFile("fixtures/usd_character_create.json")
	if err != nil {
		t.Fatal(err)
	}
	designRaw, err := os.ReadFile("fixtures/usd_design_chat_owner_approved.json")
	if err != nil {
		t.Fatal(err)
	}
	character, err := Parse(characterRaw)
	if err != nil {
		t.Fatal(err)
	}
	design, err := Parse(designRaw)
	if err != nil {
		t.Fatal(err)
	}
	if character.EffectiveFrom.Before(design.EffectiveFrom) {
		t.Errorf("character pricing takes effect before its design-chat predecessor: %s < %s", character.EffectiveFrom, design.EffectiveFrom)
	}
	for action, expected := range design.Actions {
		if got, ok := character.Actions[action]; !ok || got != expected {
			t.Errorf("predecessor action changed or missing: %s got=%+v present=%v want=%+v", action, got, ok, expected)
		}
	}
	var characterDoc, designDoc map[string]any
	if err := json.Unmarshal(characterRaw, &characterDoc); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(designRaw, &designDoc); err != nil {
		t.Fatal(err)
	}
	delete(characterDoc["actions"].(map[string]any), "character_create")
	delete(characterDoc["limits"].(map[string]any), "character_create")
	delete(characterDoc, "character_create_decision")
	for _, doc := range []map[string]any{characterDoc, designDoc} {
		delete(doc, "version")
		delete(doc, "effective_from")
	}
	if !jsonEqual(characterDoc, designDoc) {
		t.Error("character snapshot must preserve every predecessor action, limit, policy and commercial term unchanged")
	}
	if character.DesignChatDecision != "owner_approved" || !HasDesignChatKeys(character) {
		t.Error("character snapshot lost the owner-approved design-chat decision/actions")
	}
	const owner = "17020000-0000-4000-8000-000000000001"
	if err := CheckDesignChatDecision(character, "production", owner, owner); err != nil {
		t.Errorf("owner-approved superset failed the design-chat production gate: %v", err)
	}
	if CheckDesignChatDecision(character, "production", "", owner) == nil {
		t.Error("superset bypassed production owner review")
	}
}

func TestCharacterPricingAdditionIsNonAdverse(t *testing.T) {
	adverse, err := classifyAdverse(mustJSON(t, sampleValidConfig()), mustJSON(t, characterPricingConfig()))
	if err != nil || adverse {
		t.Fatalf("additive character key classified adverse: %v %v", adverse, err)
	}
}

func TestCharacterPricingOwnerDecisionGate(t *testing.T) {
	c, err := Parse(mustJSON(t, characterPricingConfig()))
	if err != nil {
		t.Fatal(err)
	}
	owner := "17020000-0000-4000-8000-000000000001"
	if CheckCharacterCreateDecision(c, owner, owner) == nil {
		t.Fatal("missing decision accepted")
	}
	c.CharacterCreateDecision = "owner_approved"
	for _, pair := range [][2]string{{"", ""}, {owner, ""}, {"", owner}, {"other", owner}} {
		if CheckCharacterCreateDecision(c, pair[0], pair[1]) == nil {
			t.Fatalf("missing/mismatched owner accepted: %v", pair)
		}
	}
	if err := CheckCharacterCreateDecision(c, owner, owner); err != nil {
		t.Fatal(err)
	}
}
