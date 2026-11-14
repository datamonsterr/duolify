package narration

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
)

func TestVoiceConfigIsTheApprovedCuratedSet(t *testing.T) {
	prod, err := LoadVoices(false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(prod.Keys(), ","); got != "default" {
		t.Fatalf("production exposes only the configured default voice (ADR 0012), got %s", got)
	}
	test, err := LoadVoices(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(test.Keys(), ","); got != "default,fixture-warm,fixture-calm" {
		t.Fatal(got)
	}
	if prod.Hash() == test.Hash() || prod.Hash() == "" || len(test.Hash()) != 64 {
		t.Fatal("the approved set must have its own fingerprint", prod.Hash(), test.Hash())
	}
	again, _ := LoadVoices(true)
	if again.Hash() != test.Hash() {
		t.Fatal("fingerprint must be stable")
	}
	if v, ok := test.Find("fixture-warm"); !ok || !v.Curated {
		t.Fatal("fixture voices are curated")
	}
	if v, ok := test.Find("default"); !ok || v.Curated {
		t.Fatal("the default voice is never curated")
	}
	if _, ok := prod.Find("fixture-warm"); ok {
		t.Fatal("fixture voices must stay out of production")
	}
	if _, ok := test.Find("rachel"); ok {
		t.Fatal("unapproved voices must not resolve")
	}
}

func TestVoiceConfigHoldsNoVendorIDsOrPrices(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal(voicesJSON, &doc); err != nil {
		t.Fatal(err)
	}
	raw := strings.ToLower(string(voicesJSON))
	for _, banned := range []string{"voice_id", "price", "credits", "usd", "api_key", "eleven"} {
		if strings.Contains(raw, banned) {
			t.Fatalf("voice config must hold logical keys only: %q", banned)
		}
	}
}

// The Engine resolves each logical key to a vendor voice; both configs must list the same keys.
func TestEngineVoiceConfigListsTheSameKeys(t *testing.T) {
	raw, err := os.ReadFile("../../../../engine/crates/engine-core/src/narration/voices.json")
	if err != nil {
		t.Fatal(err)
	}
	var engine struct {
		Voices []struct {
			Key string `json:"key"`
		} `json:"voices"`
	}
	if err = json.Unmarshal(raw, &engine); err != nil {
		t.Fatal(err)
	}
	set, _ := LoadVoices(true)
	var keys []string
	for _, v := range engine.Voices {
		keys = append(keys, v.Key)
	}
	if strings.Join(keys, ",") != strings.Join(set.Keys(), ",") {
		t.Fatalf("engine voices %v differ from API voices %v", keys, set.Keys())
	}
}

func TestEntitlementNeedsTheVerifiedActiveBenefit(t *testing.T) {
	plus := &credits.FundedPlan{Active: true, Benefits: []string{"Offline course library downloads", BenefitNoUpsell, BenefitCuratedVoices}}
	learn := &credits.FundedPlan{Active: true, Benefits: []string{BenefitNoUpsell, "Learning history export"}}
	free := (*credits.FundedPlan)(nil)
	revoked := &credits.FundedPlan{Active: true, Revoked: true, Benefits: plus.Benefits}
	expired := &credits.FundedPlan{Active: false, Benefits: plus.Benefits}
	cases := []struct {
		name            string
		plan            *credits.FundedPlan
		entitled, quiet bool
	}{
		{"plus", plus, true, true}, {"learn", learn, false, true}, {"free", free, false, false},
		{"revoked", revoked, false, false}, {"expired", expired, false, false},
	}
	for _, c := range cases {
		if got := EntitledToCuratedVoices(c.plan); got != c.entitled {
			t.Errorf("%s entitled=%v", c.name, got)
		}
		if got := UpsellSuppressed(c.plan); got != c.quiet {
			t.Errorf("%s upsell suppressed=%v", c.name, got)
		}
	}
}

func TestVoiceChangeHashesDifferPerVoiceAndNormalizedTextIsShared(t *testing.T) {
	text := NormalizeText("  The sky   is a map. ")
	a := ContentHash(text, "voice-a", "model", "fp1")
	b := ContentHash(text, "voice-b", "model", "fp1")
	if a == b {
		t.Fatal("another voice is another asset")
	}
	if a != ContentHash(NormalizeText("The sky is a map."), "voice-a", "model", "fp1") {
		t.Fatal("replaying the same voice reuses the stored asset")
	}
	if TextHash(text) != TextHash(NormalizeText("The sky is a map.")) {
		t.Fatal("text hash links the voices of one narration")
	}
}
