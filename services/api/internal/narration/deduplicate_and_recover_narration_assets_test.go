package narration

import (
	"testing"
	"time"
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
	// 50 Credits per 1,440 characters: two 45-character slides cost ceil(90*50/1440)=4, not 2+2 per slide... and
	// three 29-character slides cost ceil(87*50/1440)=4 where per-slide rounding would charge 3.
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
