package narration

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
)

func TestNormalizeTextCollapsesWhitespaceAndComposesUnicode(t *testing.T) {
	for in, want := range map[string]string{
		"  The sky \n is\ta map. ": "The sky is a map.",
		"Café":                    "Café", // NFC
		"   ":                      "",
	} {
		if got := NormalizeText(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestEquivalentTextsShareContentAndTextHash(t *testing.T) {
	a, b := NormalizeText("The sky  is a map."), NormalizeText("The sky is a map. \n")
	if ContentHash(a, "v", "m", "fp") != ContentHash(b, "v", "m", "fp") || TextHash(a) != TextHash(b) {
		t.Fatal("normalized texts must hash identically")
	}
	if TextHash(a) == TextHash("Another sentence.") {
		t.Fatal("different text must not share a text hash")
	}
	// Settings change the content hash (a new version) but not the text hash that links versions.
	if ContentHash(a, "v", "m", "fp1") == ContentHash(a, "v", "m", "fp2") {
		t.Fatal("settings fingerprint must version the asset")
	}
}

func TestAggregateCharacterPriceRoundsUpOnce(t *testing.T) {
	// 50 Credits per 1,440 characters: three 29-character slides cost ceil(87*50/1440)=4 in total,
	// where rounding each slide up would charge 2+2+2=6.
	if got := AggregateCredits([]int{29, 29, 29}, 50, 1440); got != 4 {
		t.Fatal(got)
	}
	if got := AggregateCredits([]int{1, 1, 1}, 50, 1440); got != 1 {
		t.Fatal(got)
	}
	if got := AggregateCredits(nil, 50, 1440); got != 0 {
		t.Fatal(got)
	}
	if got := AggregateCredits([]int{1440}, 50, 1440); got != 50 {
		t.Fatal(got)
	}
}

func TestRetentionDefaultsAndExpiry(t *testing.T) {
	if DefaultRetention != 30*24*time.Hour {
		t.Fatal(DefaultRetention)
	}
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	if RetentionExpired(now.Add(-29*24*time.Hour), now, DefaultRetention) || !RetentionExpired(now.Add(-31*24*time.Hour), now, DefaultRetention) {
		t.Fatal("retention boundary")
	}
}

func TestRecoveryStatesAreClosed(t *testing.T) {
	for _, s := range []string{RecoveryOK, RecoveryRestored, RecoveryRegenerating, RecoveryPending} {
		if s == "" {
			t.Fatal("empty recovery state")
		}
	}
}

// The recorded golden fixture and the Go twin of the engine agree on the resolved identity inputs.
func TestRecordedDedupeFixtureMatchesResolvedConfig(t *testing.T) {
	raw, err := os.ReadFile("../../../../evals/fixtures/narration.dedupe.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		VoiceID     string `json:"voice_id"`
		ModelID     string `json:"model_id"`
		Fingerprint string `json:"settings_fingerprint"`
		Cases       []struct {
			ID    string   `json:"id"`
			Texts []string `json:"texts"`
		} `json:"cases"`
	}
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	resolved, err := narrationtest.Synthesize(&enginev1.SynthesizeNarrationRequest{VoiceKey: VoiceKey, ResolveOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.VoiceId != fixture.VoiceID || resolved.ModelId != fixture.ModelID || resolved.SettingsFingerprint != fixture.Fingerprint {
		t.Fatal("fixture drifted from the resolved config", resolved.SettingsFingerprint)
	}
	if len(fixture.Cases) != 3 {
		t.Fatal("cache, retry and alignment-failure cases are required", len(fixture.Cases))
	}
	cache := fixture.Cases[0].Texts
	if ContentHash(NormalizeText(cache[0]), fixture.VoiceID, fixture.ModelID, fixture.Fingerprint) != ContentHash(NormalizeText(cache[1]), fixture.VoiceID, fixture.ModelID, fixture.Fingerprint) {
		t.Fatal("cache case texts must share one content hash")
	}
}
