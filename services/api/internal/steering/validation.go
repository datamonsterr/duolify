// Package steering stores screened instructions and immutable generation inputs.
package steering

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var urlPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*:|www\.|\b[a-z0-9-]+\.(com|org|net|io|dev|test)(\b|/))`)

// ValidateText runs before any provider call; limits count Unicode characters.
func ValidateText(text string, maximum int) string {
	if strings.TrimSpace(text) == "" {
		return "empty"
	}
	if utf8.RuneCountInString(text) > maximum {
		return "too_long"
	}
	for _, r := range text {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "control_characters"
		}
	}
	if urlPattern.MatchString(text) {
		return "urls_not_allowed"
	}
	lower := strings.ToLower(text)
	for _, phrase := range []string{"ignore previous", "ignore all", "system prompt", "developer message", "no quiz", "skip the test", "skip test", "make all answers", "change grading", "free credits"} {
		if strings.Contains(lower, phrase) {
			return "prompt_injection"
		}
	}
	return ""
}

// LengthBucket is the only text-derived analytics property.
func LengthBucket(n int) string {
	if n <= 100 {
		return "1_100"
	}
	if n <= 250 {
		return "101_250"
	}
	return "251_500"
}
