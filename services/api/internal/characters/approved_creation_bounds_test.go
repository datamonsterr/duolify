package characters_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

func TestCharacterPricingSpecBytesAndCastBounds(t *testing.T) {
	raw, err := json.Marshal(characters.DefaultGuideFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{8192, 8193} {
		padded := append(append([]byte{}, raw...), []byte(strings.Repeat(" ", size-len(raw)))...)
		_, err := characters.ValidateSpecJSON(padded)
		if (err == nil) != (size == 8192) {
			t.Fatalf("spec size %d: %v", size, err)
		}
	}
	for _, tc := range []struct {
		supporting []string
		valid      bool
	}{
		{[]string{}, true},
		{[]string{"a"}, true},
		{[]string{"a", "b"}, true},
		{[]string{"a", "a"}, false},
		{[]string{"a", "b", "c"}, false},
		{[]string{"guide"}, false},
	} {
		err := characters.ValidateCastSelection(characters.CastSelection{Guide: "guide", Supporting: tc.supporting})
		if (err == nil) != tc.valid {
			t.Fatalf("supporting=%v: %v", tc.supporting, err)
		}
	}
	for _, prompt := range []string{"x", strings.Repeat("🌱", 1500)} {
		if !characters.ValidCreatePrompt(prompt) {
			t.Fatal("boundary prompt rejected")
		}
	}
}
