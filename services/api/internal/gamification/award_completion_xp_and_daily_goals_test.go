package gamification

import (
	"testing"
	"time"
)

func TestConfiguredAwardsAndGoals(t *testing.T) {
	cfg := Rules{BaseXP: 17, PerfectBonusXP: 6, DefaultGoal: 23, GoalOptions: []int{23, 46}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.Award(false) != 17 || cfg.Award(true) != 23 || cfg.AllowsGoal(24) || !cfg.AllowsGoal(46) {
		t.Fatal("configuration ignored")
	}
	for _, bad := range []Rules{{}, {BaseXP: -1, DefaultGoal: 1, GoalOptions: []int{1}}, {BaseXP: 1, PerfectBonusXP: -1, DefaultGoal: 1, GoalOptions: []int{1}}, {BaseXP: 1, DefaultGoal: 2, GoalOptions: []int{1}}, {BaseXP: 1, DefaultGoal: 1, GoalOptions: []int{1, 1}}} {
		if bad.Validate() == nil {
			t.Fatal("invalid rules accepted", bad)
		}
	}
}

func TestLearnerLocalDateDSTAndMidnight(t *testing.T) {
	for _, tc := range []struct{ at, zone, want string }{
		{"2026-03-08T04:59:59Z", "America/New_York", "2026-03-07"},
		{"2026-03-08T05:00:00Z", "America/New_York", "2026-03-08"},
		{"2026-03-08T06:59:59Z", "America/New_York", "2026-03-08"},
		{"2026-03-08T07:00:00Z", "America/New_York", "2026-03-08"},
		{"2026-11-01T05:30:00Z", "America/New_York", "2026-11-01"},
		{"2026-11-01T06:30:00Z", "America/New_York", "2026-11-01"},
		{"2026-11-02T04:59:59Z", "America/New_York", "2026-11-01"},
		{"2026-11-02T05:00:00Z", "America/New_York", "2026-11-02"},
		{"2026-11-01T10:00:00Z", "Pacific/Kiritimati", "2026-11-02"},
	} {
		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatal(err)
		}
		got, err := LocalDate(at, tc.zone)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := LocalDate(time.Now(), "invalid/timezone"); err == nil {
		t.Fatal("invalid timezone silently accepted")
	}
}
