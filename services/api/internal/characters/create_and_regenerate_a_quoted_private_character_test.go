package characters_test

import (
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"strings"
	"testing"
)

func TestCharacterCreatePromptBoundary(t *testing.T) {
	for _, p := range []string{"", "   ", strings.Repeat("x", 1501), "hello\x00", "<script>bad</script>"} {
		if characters.ValidCreatePrompt(p) {
			t.Errorf("invalid prompt accepted %q", p)
		}
	}
	if !characters.ValidCreatePrompt(strings.Repeat("🌱", 1500)) {
		t.Fatal("Unicode character limit counted bytes")
	}
	for _, state := range []string{"succeeded", "failed", "rejected", "cancelled"} {
		if !characters.CreateTerminal(state) {
			t.Error("nonterminal", state)
		}
	}
	for _, state := range []string{"screening", "quoted", "queued", "running", "unknown"} {
		if characters.CreateTerminal(state) {
			t.Error("terminal", state)
		}
	}
}
