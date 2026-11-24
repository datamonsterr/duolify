package narration

import (
	"testing"
	"time"
)

func TestOpenRouterCostAndUTCMonthReset(t *testing.T) {
	for _, c := range []struct{ characters, rate, want int64 }{
		{1, 990, 990}, {100, 4000, 400000}, {100, 15000, 1500000}, {0, 15000, 0}, {1 << 62, 15000, 0},
	} {
		if got := openRouterCostNanos(c.characters, c.rate); got != c.want {
			t.Fatalf("cost for %d at %d: %d, want %d", c.characters, c.rate, got, c.want)
		}
	}
	local := time.Date(2026, 12, 1, 5, 30, 0, 0, time.FixedZone("ICT", 7*3600))
	if got := openRouterMonthStart(local); got != time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC) {
		t.Fatalf("UTC bucket: %s", got)
	}
}

func TestProviderHashSeparatesRoutesModelsAndLegacyKeys(t *testing.T) {
	text := NormalizeText("Cafe\u0301    sky")
	keys := []string{
		ContentHash(text, "voice", "model", "fp"),
		ProviderContentHash(text, "elevenlabs", "model", "voice", "fp"),
		ProviderContentHash(text, "openrouter", "model", "voice", "fp"),
		ProviderContentHash(text, "openrouter", "model-two", "voice", "fp"),
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if len(key) != 64 || seen[key] {
			t.Fatalf("duplicate or invalid key %q", key)
		}
		seen[key] = true
	}
}
