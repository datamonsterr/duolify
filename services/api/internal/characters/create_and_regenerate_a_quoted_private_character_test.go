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

func TestValidateStartupPricing(t *testing.T) {
	tests := []struct {
		name           string
		env            string
		allow          bool
		hasApprovedKey bool
		wantErr        bool
	}{
		// When ALLOW_UNAPPROVED_PRICING is false (default):
		{"disabled in production without approved key", "production", false, false, false},
		{"disabled in production with approved key", "production", false, true, false},
		{"disabled with empty env without approved key", "", false, false, false},
		{"disabled with misspelled env without approved key", "devlopment", false, false, false},
		{"disabled in development without approved key", "development", false, false, false},
		{"disabled in test without approved key", "test", false, false, false},

		// When ALLOW_UNAPPROVED_PRICING is true:
		{"enabled in production without approved key fails", "production", true, false, true},
		{"enabled in production with approved key fails", "production", true, true, true},
		{"enabled with empty env fails (treated as production)", "", true, false, true},
		{"enabled with empty env and approved key fails", "", true, true, true},
		{"enabled with misspelled devlopment fails", "devlopment", true, false, true},
		{"enabled with misspelled staging fails", "staging", true, false, true},
		{"enabled with misspelled prod fails", "prod", true, false, true},
		{"enabled with uppercase Development fails", "Development", true, false, true},
		{"enabled in development succeeds without approved key", "development", true, false, false},
		{"enabled in development succeeds with approved key", "development", true, true, false},
		{"enabled in test succeeds without approved key", "test", true, false, false},
		{"enabled in test succeeds with approved key", "test", true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := characters.ValidateStartupPricing(tt.env, tt.allow, tt.hasApprovedKey)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateStartupPricing(%q, %v, %v) error = %v, wantErr %v", tt.env, tt.allow, tt.hasApprovedKey, err, tt.wantErr)
			}
		})
	}
}
