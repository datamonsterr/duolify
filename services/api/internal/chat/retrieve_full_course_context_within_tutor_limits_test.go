package chat

import (
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

func TestSearchTermsAreWordsOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want string
	}{
		"plain":        {"What is Zorblax?", "what | zorblax"},
		"operators":    {"a & b | c ! (zorblax) :* <-> 'x'", "zorblax"},
		"sql":          {"sprocket'); DROP TABLE lesson_slides;--", "sprocket | drop | table | lesson | slides"},
		"dedupe":       {"moon Moon MOON phases", "moon | phases"},
		"unicode":      {"сprocket 日本語 ☃", "сprocket | 日本語"},
		"only symbols": {"&|!():*<->", ""},
		"short words":  {"a an of it", ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := searchTerms(tc.in); got != tc.want {
				t.Fatalf("%q: got %q want %q", tc.in, got, tc.want)
			}
		})
	}
	if n := len(strings.Split(searchTerms(strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi omicron ", 2)), " | ")); n > maxSearchTerms {
		t.Fatalf("terms unbounded: %d", n)
	}
}

func TestBudgetVerdict(t *testing.T) {
	for name, tc := range map[string]struct {
		mandatory, standard, ceiling int64
		want                         verdict
	}{
		"fits":            {5000, 12500, 48000, fits},
		"exactly fits":    {12500, 12500, 48000, fits},
		"needs approval":  {12501, 12500, 48000, needsApproval},
		"at the ceiling":  {48000, 12500, 48000, needsApproval},
		"above ceiling":   {48001, 12500, 48000, tooLarge},
		"approved bigger": {20000, 20000, 48000, fits},
	} {
		if got := judge(tc.mandatory, tc.standard, tc.ceiling); got != tc.want {
			t.Fatalf("%s: got %v want %v", name, got, tc.want)
		}
	}
}

func TestFitExcerptsKeepsWholeRankedExcerptsOnly(t *testing.T) {
	mk := func(n int, c byte) *enginev1.SourceExcerpt {
		return &enginev1.SourceExcerpt{LessonId: "l", LessonTitle: "T", Text: strings.Repeat(string(c), n)}
	}
	in := []*enginev1.SourceExcerpt{mk(100, 'a'), mk(400, 'b'), mk(100, 'c')}
	got := fitExcerpts(in, 1000)
	if len(got) != 1 || got[0].Text[0] != 'a' {
		t.Fatalf("stop at the first excerpt that does not fit, rank order kept: %v", got)
	}
	if len(fitExcerpts(in, 0)) != 0 || len(fitExcerpts(nil, 1000)) != 0 {
		t.Fatal("no room means no excerpts")
	}
	if got := fitExcerpts(in, 100000); len(got) != 3 {
		t.Fatal("all fit")
	}
}

func TestCacheClass(t *testing.T) {
	for name, tc := range map[string]struct {
		u    *enginev1.Usage
		want string
	}{
		"cold":              {&enginev1.Usage{CacheWriteTokens: 10}, "cold"},
		"warm":              {&enginev1.Usage{CacheReadTokens: 10}, "warm"},
		"warm with refresh": {&enginev1.Usage{CacheReadTokens: 10, CacheWriteTokens: 2}, "warm"},
		"none":              {&enginev1.Usage{InputTokens: 5}, "none"},
		"nil":               {nil, "none"},
	} {
		if got := cacheClass(tc.u); got != tc.want {
			t.Fatalf("%s: %s", name, got)
		}
	}
}

func TestTrimExcerptCapsRunesAndStripsMarkup(t *testing.T) {
	got := trimExcerpt("  <p>héllo</p>\n\n  " + strings.Repeat("é", 5000))
	if strings.Contains(got, "<p>") || len([]rune(got)) > maxExcerptRunes || !strings.HasPrefix(got, "héllo") {
		t.Fatalf("bad excerpt: %d", len([]rune(got)))
	}
}
