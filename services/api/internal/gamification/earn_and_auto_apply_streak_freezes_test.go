package gamification

import (
	"slices"
	"testing"
)

func days(from string, n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		d, _ := addDays(from, i)
		out = append(out, d)
	}
	return out
}

func TestFreezeRulesValidate(t *testing.T) {
	for rules, ok := range map[FreezeRules]bool{{7, 2}: true, {1, 0}: true, {0, 2}: false, {7, -1}: false} {
		if (rules.Validate() == nil) != ok {
			t.Fatal(rules, ok)
		}
	}
}

func TestSimulateStreakFreezes(t *testing.T) {
	rules := FreezeRules{MilestoneDays: 7, Cap: 2}
	for _, tc := range []struct {
		name         string
		dates        []string
		decided      string
		rules        FreezeRules
		cur, longest int
		held         int
		grants       []Grant
		protected    []Protection
		broken       []StreakBreak
	}{
		{"below milestone", days("2026-11-01", 6), "2026-11-06", rules, 6, 6, 0, nil, nil, nil},
		{"milestone grants once", days("2026-11-01", 7), "2026-11-07", rules, 7, 7, 1,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}}, nil, nil},
		{"two milestones fill cap", days("2026-11-01", 14), "2026-11-14", rules, 14, 14, 2,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}, {14, "2026-11-01", "2026-11-14", true}}, nil, nil},
		{"overflow keeps a record but grants nothing", days("2026-11-01", 21), "2026-11-21", FreezeRules{7, 2}, 21, 21, 2,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}, {14, "2026-11-01", "2026-11-14", true}, {21, "2026-11-01", "2026-11-21", false}}, nil, nil},
		{"cap zero never grants", days("2026-11-01", 7), "2026-11-07", FreezeRules{7, 0}, 7, 7, 0,
			[]Grant{{7, "2026-11-01", "2026-11-07", false}}, nil, nil},
		{"missed day consumes one freeze and preserves", days("2026-11-01", 7), "2026-11-08", rules, 7, 7, 0,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}}, []Protection{{"2026-11-08", 0, 7}}, nil},
		{"preserved streak resumes without a bonus day", append(days("2026-11-01", 7), "2026-11-09"), "2026-11-09", rules, 8, 8, 0,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}}, []Protection{{"2026-11-08", 0, 7}}, nil},
		{"second consecutive miss breaks", days("2026-11-01", 7), "2026-11-09", rules, 0, 7, 0,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}}, []Protection{{"2026-11-08", 0, 7}}, []StreakBreak{{"2026-11-09", 7}}},
		{"two freezes cover two days", days("2026-11-01", 14), "2026-11-16", rules, 14, 14, 0,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}, {14, "2026-11-01", "2026-11-14", true}}, []Protection{{"2026-11-15", 1, 14}, {"2026-11-16", 0, 14}}, nil},
		{"no freeze breaks at first missed day", days("2026-11-01", 3), "2026-11-05", rules, 0, 3, 0,
			nil, nil, []StreakBreak{{"2026-11-04", 3}}},
		{"undecided day is never judged", days("2026-11-01", 7), "2026-11-07", rules, 7, 7, 1,
			[]Grant{{7, "2026-11-01", "2026-11-07", true}}, nil, nil},
		{"freeze is not spent without a live streak", nil, "2026-11-10", rules, 0, 0, 0, nil, nil, nil},
		{"month boundary", days("2026-10-26", 7), "2026-11-02", rules, 7, 7, 0,
			[]Grant{{7, "2026-10-26", "2026-11-01", true}}, []Protection{{"2026-11-02", 0, 7}}, nil},
		{"new run after break earns again", append(days("2026-11-01", 3), days("2026-11-10", 7)...), "2026-11-16", rules, 7, 7, 1,
			[]Grant{{7, "2026-11-10", "2026-11-16", true}}, nil, []StreakBreak{{"2026-11-04", 3}}},
	} {
		got := SimulateStreak(tc.dates, tc.decided, tc.rules)
		if got.Current != tc.cur || got.Longest != tc.longest || got.Held != tc.held ||
			!slices.Equal(got.Grants, tc.grants) || !slices.Equal(got.Protections, tc.protected) || !slices.Equal(got.Breaks, tc.broken) {
			t.Fatalf("%s: got %+v", tc.name, got)
		}
	}
}

func TestComputeStreakIsSimulationWithoutFreezes(t *testing.T) {
	got := ComputeStreak(days("2026-11-01", 7), "2026-11-08")
	if got.Current != 0 || len(got.Breaks) != 1 || got.Breaks[0].BrokenOn != "2026-11-08" {
		t.Fatal(got)
	}
}
