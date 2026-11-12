package gamification

import "testing"

func TestComputeStreakRuns(t *testing.T) {
	for _, tc := range []struct {
		name          string
		dates         []string
		decided       string
		cur, longest  int
		broken        []string
		brokenLengths []int
	}{
		{"none", nil, "2026-11-05", 0, 0, nil, nil},
		{"single day alive", []string{"2026-11-05"}, "2026-11-05", 1, 1, nil, nil},
		{"yesterday alive", []string{"2026-11-04", "2026-11-05"}, "2026-11-05", 2, 2, nil, nil},
		{"missed after run", []string{"2026-11-01", "2026-11-02"}, "2026-11-04", 0, 2, []string{"2026-11-03"}, []int{2}},
		{"gap resets keeps longest", []string{"2026-11-01", "2026-11-02", "2026-11-03", "2026-11-06"}, "2026-11-06", 1, 3, []string{"2026-11-04"}, []int{3}},
		{"month and year boundary", []string{"2026-12-31", "2027-01-01"}, "2027-01-01", 2, 2, nil, nil},
		{"leap day", []string{"2028-02-28", "2028-02-29", "2028-03-01"}, "2028-03-01", 3, 3, nil, nil},
		{"undecided last day stays alive", []string{"2026-11-04"}, "2026-11-03", 1, 1, nil, nil},
	} {
		got := ComputeStreak(tc.dates, tc.decided)
		if got.Current != tc.cur || got.Longest != tc.longest || len(got.Breaks) != len(tc.broken) {
			t.Fatal(tc.name, got)
		}
		for i, b := range got.Breaks {
			if b.BrokenOn != tc.broken[i] || b.PreviousLength != tc.brokenLengths[i] {
				t.Fatal(tc.name, got.Breaks)
			}
		}
	}
}

func TestPreviousDayIsCalendarArithmetic(t *testing.T) {
	for in, want := range map[string]string{"2026-03-09": "2026-03-08", "2026-11-02": "2026-11-01", "2027-01-01": "2026-12-31", "2028-03-01": "2028-02-29"} {
		got, err := addDays(in, -1)
		if err != nil || got != want {
			t.Fatal(in, got, err)
		}
	}
	if _, err := addDays("not-a-date", -1); err == nil {
		t.Fatal("bad date accepted")
	}
}
