package chat

import (
	"strings"
	"testing"
)

func TestSafeResponseCopyFollowsTheLocaleAndDefaultsToEnglish(t *testing.T) {
	for header, want := range map[string]string{"vi-VN,vi;q=0.9": "Bạn không đơn độc", "es": "No estás solo", "en-US": "You are not alone", "": "You are not alone", "fr": "You are not alone"} {
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
