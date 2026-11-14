package sharing

import "testing"

// Phone rule (#199). Flag only phone-shaped text: leading "+CC", "(area) ...", trunk-"0" groups,
// bare trunk-"0" runs of 9-15 digits, or NNN-NNN-NNNN with one separator kind. Digits must be
// 7-15 and the match must not sit inside a longer alphanumeric / hex / dashed token.
// Bare unseparated runs without "+" or leading "0" (years, IDs, ZIPs) are NOT flagged (conservative).
func TestPhoneRule(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"+1 (415) 555-2671", true},
		{"+44 20 7946 0958", true},
		{"+84 912 345 678", true},
		{"0912 345 678", true},
		{"415-555-2671", true},
		{"415.555.2671", true},
		{"415 555 2671", true},
		{"(415) 555 2671", true},
		{"+49 30 901820", true},
		{"+14155552671", true},
		{"0912345678", true},
		{"020 7946 0958", true},
		{"Call me: 415-555-2671.", true},
		{"hotline (+84) 912 345 678 now", true},
		{"UUID 3412-5678", false},
		{"550e8400-e29b-41d4-a716-446655440000", false},
		{"id 12345678-1234-5678-1234-567812345678", false},
		{"deadbeef1234567890", false},
		{"2026", false},
		{"in 2026 we", false},
		{"1990-2020", false},
		{"2026-11-14", false},
		{"01-02-2026", false},
		{"3.14159265", false},
		{"v1.2.3", false},
		{"10.0.19045", false},
		{"978-3-16-148410-0", false},
		{"3x + 2 = 14", false},
		{"12 * 34 = 408", false},
		{"SKU-12345678", false},
		{"order A415-555-2671", false},
		{"1234567890", false},
		{"123456789012", false},
		{"Chapter 12, page 345", false},
		{"", false},
	}
	for _, c := range cases {
		if got := hasPhone(c.in); got != c.want {
			t.Errorf("hasPhone(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
