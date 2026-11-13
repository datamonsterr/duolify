package social

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseQueryBoundaries(t *testing.T) {
	type want struct {
		text       string
		length     int
		searchable bool
		short      bool
	}
	for in, w := range map[string]want{
		"may":                   {"may", 3, true, false},
		"  MAY  ":               {"may", 3, true, false},
		"@Maya.Learns":          {"maya.learns", 11, true, false},
		"ＭＡＹ":                   {"may", 3, true, false},
		"ma":                    {"", 2, false, true},
		"@ma":                   {"", 2, false, true},
		"":                      {"", 0, false, true},
		"   ":                   {"", 0, false, true},
		"ma%":                   {"", 3, false, false},
		"ma y":                  {"", 4, false, false},
		"may'--":                {"", 6, false, false},
		"_ab":                   {"_ab", 3, true, false}, // an underscore is data, never a wildcard
		strings.Repeat("a", 20): {strings.Repeat("a", 20), 20, true, false},
		strings.Repeat("a", 21): {"", 21, false, false},
		"ünï":                   {"", 3, false, false},
		"may\x00":               {"", 4, false, false},
	} {
		got, err := ParseQuery(in)
		if (err != nil) != w.short || (err != nil && !errors.Is(err, ErrQueryTooShort)) {
			t.Errorf("ParseQuery(%q) err=%v want short=%v", in, err, w.short)
			continue
		}
		if got.Text != w.text || got.Length != w.length || got.Searchable != w.searchable {
			t.Errorf("ParseQuery(%q)=%+v want %+v", in, got, w)
		}
	}
}

func TestLikePrefixEscapesWildcards(t *testing.T) {
	for in, want := range map[string]string{"may": `may%`, "a_b": `a\_b%`, `a\b`: `a\\b%`, "a%b": `a\%b%`} {
		if got := likePrefix(in); got != want {
			t.Errorf("likePrefix(%q)=%q want %q", in, got, want)
		}
	}
}

func TestParseSettingsPatchIsStrictAndPartial(t *testing.T) {
	p, err := ParseSettingsPatch(strings.NewReader(`{"discoverable":false}`))
	if err != nil || p.Discoverable == nil || *p.Discoverable || p.RequestPolicy != nil || p.ShowStreakToFriends != nil {
		t.Fatalf("partial patch: %+v %v", p, err)
	}
	for _, policy := range []string{"everyone", "friends_of_friends", "nobody"} {
		if p, err = ParseSettingsPatch(strings.NewReader(`{"request_policy":"` + policy + `"}`)); err != nil || *p.RequestPolicy != policy {
			t.Errorf("policy %s: %v", policy, err)
		}
	}
	for _, body := range []string{``, `{}`, `[]`, `null`, `{"request_policy":"public"}`, `{"request_policy":""}`, `{"discoverable":"yes"}`, `{"discoverable":null}`, `{"x":1}`, `{"discoverable":true}{}`, `{"discoverable":true,"discoverable":false} x`, strings.Repeat(" ", 2000) + `{"discoverable":true}`} {
		if _, err = ParseSettingsPatch(strings.NewReader(body)); err == nil {
			t.Errorf("accepted %.40q", body)
		}
	}
}

func TestSettingEventValuesAreClosedEnums(t *testing.T) {
	if boolValue(true) != "on" || boolValue(false) != "off" {
		t.Fatal("boolean enum")
	}
}

func TestCooldownAndRateRejectionsAreNeutralAndTyped(t *testing.T) {
	end := time.Date(2026, 12, 2, 10, 0, 0, 0, time.UTC)
	r := &Rejection{Code: CodeCooldown, NextChangeAt: end}
	if r.Status() != 409 || !strings.Contains(r.Error(), "December 2, 2026") {
		t.Fatalf("cooldown: %d %q", r.Status(), r.Error())
	}
	l := &Rejection{Code: CodeRateLimited, RetryAfter: 42 * time.Second}
	if l.Status() != 429 || l.RetryAfterSeconds() != 42 || strings.Contains(l.Error(), "username") {
		t.Fatalf("rate limit: %d %q", l.Status(), l.Error())
	}
	if (&Rejection{Code: CodeRateLimited, RetryAfter: 200 * time.Millisecond}).RetryAfterSeconds() != 1 {
		t.Fatal("sub-second waits round up to one second")
	}
	if (&Rejection{Code: CodeNotSet}).Status() != 409 || (&Rejection{Code: CodeTooShort}).Status() != 422 {
		t.Fatal("statuses")
	}
}
