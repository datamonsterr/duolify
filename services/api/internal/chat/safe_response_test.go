package chat

import (
	"strings"
	"testing"
)

func TestSafeResponseCopyFollowsTheLocaleAndDefaultsToEnglish(t *testing.T) {
	for header, want := range map[string]string{"vi-VN,vi;q=0.9": "Bạn xứng đáng được một người thật hỗ trợ", "es": "Mereces el apoyo de una persona real", "en-US": "You deserve support from a real person", "": "You deserve support from a real person", "fr": "You deserve support from a real person"} {
		if got := SafeResponseCopy(header); !strings.Contains(got, want) {
			t.Fatalf("%q: %q", header, got)
		}
	}
}

// The placeholder names no organisation and no number: the owner supplies both in #423.
func TestSafeResponseCopyHasNoDigitsOrLinks(t *testing.T) {
	for locale, c := range safeResponseCopy {
		if strings.ContainsAny(c, "0123456789") || strings.Contains(c, "http") {
			t.Fatalf("%s: placeholder must not carry numbers or links", locale)
		}
	}
}
