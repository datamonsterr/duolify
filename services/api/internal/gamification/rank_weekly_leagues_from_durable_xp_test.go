package gamification

import (
	"fmt"
	"testing"
)

func TestLeagueWeekIsMondayToSunday(t *testing.T) {
	for date, want := range map[string]string{
		"2026-11-09": "2026-11-09", "2026-11-12": "2026-11-09", "2026-11-15": "2026-11-09",
		"2026-11-16": "2026-11-16", "2026-01-01": "2025-12-29", "2024-02-29": "2024-02-26",
	} {
		got, err := WeekStart(date)
		if err != nil || got != want {
			t.Fatal(date, got, err)
		}
	}
	if end, _ := WeekEnd("2026-11-09"); end != "2026-11-15" {
		t.Fatal(end)
	}
	if _, err := WeekStart("not a date"); err == nil {
		t.Fatal("accepted invalid date")
	}
}

func TestLeagueScoreOrdersByXPThenEarlierSlotAndRoundTrips(t *testing.T) {
	if !(LeagueScore(240, 1) > LeagueScore(240, 2) && LeagueScore(241, 999) > LeagueScore(240, 1)) {
		t.Fatal("composite score does not order xp desc then slot asc")
	}
	for _, tc := range [][2]int64{{0, 1}, {240, 5}, {99_999_999, 999}} {
		xp, slot := SplitLeagueScore(LeagueScore(tc[0], int(tc[1])))
		if xp != tc[0] || slot != int(tc[1]) {
			t.Fatal(tc, xp, slot)
		}
	}
}

func TestLeagueZones(t *testing.T) {
	rules := LeagueRules{Tiers: []string{"Dawn", "Sun", "Sky"}, GroupSize: 30, PromoteTop: 5, DemoteBottom: 5}
	for _, tc := range []struct {
		name          string
		tier, members int
		rank          int
		want          string
	}{
		{"top of middle", 2, 30, 1, "promotion"},
		{"promotion edge", 2, 30, 5, "promotion"},
		{"middle", 2, 30, 6, "none"},
		{"above demotion", 2, 30, 25, "none"},
		{"demotion edge", 2, 30, 26, "demotion"},
		{"last", 2, 30, 30, "demotion"},
		{"top tier cannot promote", 3, 30, 1, "none"},
		{"top tier demotes", 3, 30, 30, "demotion"},
		{"lowest tier cannot demote", 1, 30, 30, "none"},
		{"lowest tier promotes", 1, 30, 2, "promotion"},
		{"small group promotion capped at half", 2, 7, 3, "promotion"},
		{"small group middle", 2, 7, 4, "none"},
		{"small group demotion capped at half", 2, 7, 5, "demotion"},
		{"tiny group top", 2, 3, 1, "promotion"},
		{"tiny group middle", 2, 3, 2, "none"},
		{"tiny group bottom", 2, 3, 3, "demotion"},
		{"lone learner has no zones", 2, 1, 1, "none"},
	} {
		if got := rules.Zone(tc.tier, tc.members, tc.rank); got != tc.want {
			t.Fatal(tc.name, got, tc.want)
		}
	}
	if from := rules.DemotionFrom(2, 30); from != 26 {
		t.Fatal(from)
	}
	if from := rules.DemotionFrom(1, 30); from != 0 {
		t.Fatal("lowest tier has no demotion zone", from)
	}
}

func TestLeagueRulesValidate(t *testing.T) {
	ok := LeagueRules{Tiers: []string{"A"}, GroupSize: 30, PromoteTop: 5, DemoteBottom: 5}
	if ok.Validate() != nil {
		t.Fatal("valid rules rejected")
	}
	for name, bad := range map[string]LeagueRules{
		"no tiers": {GroupSize: 30}, "tiny group": {Tiers: []string{"A"}, GroupSize: 1},
		"huge group": {Tiers: []string{"A"}, GroupSize: 1000}, "overlap": {Tiers: []string{"A"}, GroupSize: 8, PromoteTop: 5, DemoteBottom: 5},
		"negative": {Tiers: []string{"A"}, GroupSize: 30, PromoteTop: -1},
	} {
		if bad.Validate() == nil {
			t.Fatal(name)
		}
	}
}

func TestRankLeagueOrdersTiesAndPages(t *testing.T) {
	entries := []LeagueEntry{{"d", 240, 4}, {"a", 420, 3}, {"b", 240, 2}, {"c", 10, 1}}
	ranked := RankLeague(entries)
	order := ""
	for _, e := range ranked {
		order += fmt.Sprintf("%s%d/%v/%v ", e.Learner, e.Rank, e.Tie, e.XP)
	}
	if order != "a1/false/420 b2/true/240 d3/true/240 c4/false/10 " {
		t.Fatal(order)
	}
	if len(RankLeague(nil)) != 0 {
		t.Fatal("empty league must stay empty")
	}
	page, next, prev := PageLeague(ranked, 1, 2)
	if len(page) != 2 || page[0].Learner != "b" || next == nil || *next != 3 || prev == nil || *prev != 0 {
		t.Fatal(page, next, prev)
	}
	if page, next, prev := PageLeague(ranked, 3, 10); len(page) != 1 || next != nil || prev == nil || *prev != 0 {
		t.Fatal(page, next, prev)
	}
	if page, _, _ := PageLeague(ranked, 99, 10); len(page) != 0 {
		t.Fatal("offset beyond end must be empty")
	}
}
