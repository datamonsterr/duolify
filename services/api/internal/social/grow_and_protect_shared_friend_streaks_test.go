package social

import (
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/gamification"
)

func set(dates ...string) map[string]bool {
	m := map[string]bool{}
	for _, d := range dates {
		m[d] = true
	}
	return m
}

var since = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

func auckland(decided string, active, prot []string) sideDays {
	return sideDays{Zone: "Pacific/Auckland", Active: set(active...), Protected: set(prot...), Decided: decided}
}
func angeles(decided string, active, prot []string) sideDays {
	return sideDays{Zone: "America/Los_Angeles", Active: set(active...), Protected: set(prot...), Decided: decided}
}

func TestDatesOverlapInRealTimeAcrossZones(t *testing.T) {
	cases := []struct {
		low, high string
		want      bool
	}{
		{"2026-11-14", "2026-11-13", true},
		{"2026-11-14", "2026-11-14", true}, // Auckland day still overlaps the start of the Los Angeles day
		{"2026-11-14", "2026-11-15", false},
		{"2026-11-14", "2026-11-12", false},
	}
	for _, c := range cases {
		if got := datesOverlap("Pacific/Auckland", c.low, "America/Los_Angeles", c.high); got != c.want {
			t.Errorf("overlap(%s,%s) = %v, want %v", c.low, c.high, got, c.want)
		}
	}
}

func TestEvaluateStartsOnEarliestOverlapAndGrowsOncePerDay(t *testing.T) {
	low := auckland("2026-11-14", []string{"2026-11-12", "2026-11-13", "2026-11-14"}, nil)
	high := angeles("2026-11-13", []string{"2026-11-11", "2026-11-12", "2026-11-13"}, nil)
	st, settled := evaluatePair(runState{}, low, high, since)
	if st.Days != 3 || st.Run != 1 || st.Longest != 3 || len(settled) != 3 {
		t.Fatalf("state %+v settled %v", st, settled)
	}
	if !settled[0].Started || settled[1].Started || settled[0].Low != "2026-11-12" || settled[0].High != "2026-11-11" {
		t.Fatalf("settled = %+v", settled)
	}
	again, more := evaluatePair(st, low, high, since)
	if again != st || len(more) != 0 {
		t.Fatalf("replay changed state: %+v %v", again, more)
	}
}

func TestEvaluateIgnoresDaysBeforeTheFriendship(t *testing.T) {
	low := auckland("2026-11-14", []string{"2026-11-12"}, nil)
	high := angeles("2026-11-13", []string{"2026-11-11"}, nil)
	late := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	if st, settled := evaluatePair(runState{}, low, high, late); st.Days != 0 || len(settled) != 0 {
		t.Fatalf("counted pre-friendship days: %+v %v", st, settled)
	}
}

func TestEvaluateWaitsForLaterDayEndThenBreaks(t *testing.T) {
	base := runState{Days: 2, Longest: 2, Run: 1, CursorLow: "2026-11-14", CursorHigh: "2026-11-13", StartLow: "2026-11-13", StartHigh: "2026-11-12"}
	low := auckland("2026-11-15", []string{"2026-11-13", "2026-11-14"}, nil) // missed 11-15
	high := angeles("2026-11-13", []string{"2026-11-12", "2026-11-13"}, nil) // 11-14 still open
	if st, settled := evaluatePair(base, low, high, since); st != base || len(settled) != 0 {
		t.Fatalf("decided before the later day-end: %+v %v", st, settled)
	}
	high.Decided = "2026-11-14"
	st, settled := evaluatePair(base, low, high, since)
	if st.Days != 0 || st.Longest != 2 || len(settled) != 1 || settled[0].Outcome != "broken" {
		t.Fatalf("state %+v settled %v", st, settled)
	}
}

func TestEvaluateLateCompletionStillExtendsBeforeDayEnd(t *testing.T) {
	base := runState{Days: 2, Longest: 2, Run: 1, CursorLow: "2026-11-14", CursorHigh: "2026-11-13"}
	low := auckland("2026-11-14", []string{"2026-11-15"}, nil)
	high := angeles("2026-11-13", []string{"2026-11-14"}, nil)
	if st, settled := evaluatePair(base, low, high, since); st.Days != 3 || len(settled) != 1 || settled[0].Outcome != "extended" {
		t.Fatalf("state %+v settled %v", st, settled)
	}
}

func TestEvaluateProtectionRules(t *testing.T) {
	base := runState{Days: 4, Longest: 4, Run: 1, CursorLow: "2026-11-14", CursorHigh: "2026-11-13"}
	cases := []struct {
		name                        string
		lowActive, lowProt, highAct bool
		highProt                    bool
		wantDays                    int
		wantOutcome                 string
	}{
		{"low missed with own freeze", false, true, true, false, 4, "protected"},
		{"low missed without freeze, high has one", false, false, false, true, 0, "broken"},
		{"both missed both protected", false, true, false, true, 4, "protected"},
		{"both missed one protected", false, true, false, false, 0, "broken"},
		{"high missed with own freeze", true, false, false, true, 4, "protected"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var la, lp, ha, hp []string
			if c.lowActive {
				la = []string{"2026-11-15"}
			}
			if c.lowProt {
				lp = []string{"2026-11-15"}
			}
			if c.highAct {
				ha = []string{"2026-11-14"}
			}
			if c.highProt {
				hp = []string{"2026-11-14"}
			}
			st, settled := evaluatePair(base, auckland("2026-11-15", la, lp), angeles("2026-11-14", ha, hp), since)
			if st.Days != c.wantDays || len(settled) != 1 || settled[0].Outcome != c.wantOutcome {
				t.Fatalf("state %+v settled %v", st, settled)
			}
		})
	}
}

func TestEvaluateStartsANewRunAfterABreak(t *testing.T) {
	base := runState{Days: 0, Longest: 3, Run: 1, CursorLow: "2026-11-15", CursorHigh: "2026-11-14"}
	low := auckland("2026-11-17", []string{"2026-11-17"}, nil)
	high := angeles("2026-11-16", []string{"2026-11-16"}, nil)
	st, settled := evaluatePair(base, low, high, since)
	if st.Days != 1 || st.Run != 2 || st.Longest != 3 || len(settled) != 1 || !settled[0].Started {
		t.Fatalf("state %+v settled %v", st, settled)
	}
}

func TestFriendStreakViewStatesAndOwnLocalDates(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC) // Auckland 11-15, Los Angeles 11-14
	st := runState{Days: 4, Longest: 4, Run: 1, CursorLow: "2026-11-14", CursorHigh: "2026-11-13"}
	// Auckland extended today's shared day (cursor = its 11-14 is yesterday there): both pending for 11-15/11-14.
	low := auckland("2026-11-14", []string{"2026-11-15"}, nil)
	high := angeles("2026-11-13", nil, nil)
	v := friendStreakView(st, low, high, true, now)
	if v.State != "waiting_friend" || v.You.LocalDate != "2026-11-15" || v.Friend.LocalDate != "2026-11-14" ||
		!v.You.Done || v.Friend.Done || v.You.TimeZone != "Pacific/Auckland" || v.Days != 4 {
		t.Fatalf("view = %+v", v)
	}
	both := friendStreakView(st, low, angeles("2026-11-13", []string{"2026-11-14"}, nil), true, now)
	if both.State != "both_done" {
		t.Fatalf("both = %+v", both)
	}
	// A's 11-15 decided without completion but Los Angeles' 11-14 is still open: checking.
	check := friendStreakView(st, auckland("2026-11-15", nil, nil), angeles("2026-11-13", nil, nil), true, now)
	if check.State != "checking" || check.CheckingUntil == nil {
		t.Fatalf("checking = %+v", check)
	}
	none := friendStreakView(runState{}, low, high, true, now)
	if none.State != "none" {
		t.Fatalf("none = %+v", none)
	}
	broken := friendStreakView(runState{Longest: 5, Run: 1, CursorLow: "2026-11-14", CursorHigh: "2026-11-13"}, low, high, true, now)
	if broken.State != "broken" || broken.Longest != 5 {
		t.Fatalf("broken = %+v", broken)
	}
	prot := friendStreakView(st, auckland("2026-11-15", nil, []string{"2026-11-15"}), angeles("2026-11-14", []string{"2026-11-14"}, nil), true, now)
	if prot.State != "protected" || !prot.You.Protected {
		t.Fatalf("protected = %+v", prot)
	}
}

// E5 protection is reused: a recorded friend milestone grant is an extra freeze in the
// same personal simulation, so one inventory serves the personal and every Friend streak.
func TestFriendMilestoneGrantIsUsableByThePersonalFreezeSimulation(t *testing.T) {
	dates := []string{"2026-11-01", "2026-11-02", "2026-11-04"}
	without := gamification.SimulateStreak(dates, "2026-11-04", gamification.FreezeRules{MilestoneDays: 7, Cap: 2})
	if len(without.Breaks) != 1 {
		t.Fatalf("baseline should break: %+v", without)
	}
	with := gamification.SimulateStreak(dates, "2026-11-04", gamification.FreezeRules{MilestoneDays: 7, Cap: 2, Extra: []string{"2026-11-02"}})
	if len(with.Breaks) != 0 || len(with.Protections) != 1 || with.Protections[0].Date != "2026-11-03" {
		t.Fatalf("friend grant not used: %+v", with)
	}
}
