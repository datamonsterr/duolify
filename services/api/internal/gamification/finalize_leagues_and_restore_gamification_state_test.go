package gamification

import (
	"fmt"
	"testing"
	"time"
)

func rulesFixture() LeagueRules {
	return LeagueRules{Tiers: []string{"Dawn", "Sun", "Sky"}, GroupSize: 30, PromoteTop: 2, DemoteBottom: 2}
}

func entries(xp ...int64) []LeagueEntry {
	out := make([]LeagueEntry, len(xp))
	for i, v := range xp {
		out[i] = LeagueEntry{Learner: fmt.Sprintf("l%d", i+1), XP: v, Slot: i + 1}
	}
	return out
}

func outcomes(rs []LeagueResult) string {
	s := ""
	for _, r := range rs {
		s += fmt.Sprintf("%s:%d:%s:%d ", r.Learner, r.Rank, r.Outcome, r.ToTier)
	}
	return s
}

func TestFinalizeLeagueMiddleTierMovesBothWays(t *testing.T) {
	got := outcomes(FinalizeLeague(rulesFixture(), 2, entries(50, 40, 30, 20, 10, 5)))
	want := "l1:1:promoted:3 l2:2:promoted:3 l3:3:stayed:2 l4:4:stayed:2 l5:5:demoted:1 l6:6:demoted:1 "
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestFinalizeLeagueEdgeTiersNeverLeaveConfiguredRange(t *testing.T) {
	if got := outcomes(FinalizeLeague(rulesFixture(), 3, entries(50, 40, 30, 20, 10, 5))); got != "l1:1:stayed:3 l2:2:stayed:3 l3:3:stayed:3 l4:4:stayed:3 l5:5:demoted:2 l6:6:demoted:2 " {
		t.Fatal("top tier", got)
	}
	if got := outcomes(FinalizeLeague(rulesFixture(), 1, entries(50, 40, 30, 20, 10, 5))); got != "l1:1:promoted:2 l2:2:promoted:2 l3:3:stayed:1 l4:4:stayed:1 l5:5:stayed:1 l6:6:stayed:1 " {
		t.Fatal("bottom tier", got)
	}
}

func TestFinalizeLeagueTiedBoundaryUsesEarlierSlotAndZeroXPNeverPromotes(t *testing.T) {
	// Ranks 2 and 3 tie at 40: the earlier slot takes the last promotion place.
	got := FinalizeLeague(rulesFixture(), 2, entries(50, 40, 40, 20, 10, 5))
	if got[1].Learner != "l2" || got[1].Outcome != "promoted" || got[2].Learner != "l3" || got[2].Outcome != "stayed" || !got[1].Tie || !got[2].Tie {
		t.Fatal(outcomes(got))
	}
	// An idle member inside the promotion zone stays; demotion is unaffected.
	idle := FinalizeLeague(rulesFixture(), 2, entries(50, 0, 0, 0, 0, 0))
	if idle[0].Outcome != "promoted" || idle[1].Outcome != "stayed" || idle[5].Outcome != "demoted" {
		t.Fatal(outcomes(idle))
	}
}

func TestFinalizeLeagueSmallAndEmptyGroups(t *testing.T) {
	if got := FinalizeLeague(rulesFixture(), 2, nil); len(got) != 0 {
		t.Fatal("empty group produced results", got)
	}
	if got := outcomes(FinalizeLeague(rulesFixture(), 2, entries(10))); got != "l1:1:stayed:2 " {
		t.Fatal("single member", got)
	}
	// Zones are halves at most, so two members never both move in opposite zones incorrectly.
	if got := outcomes(FinalizeLeague(rulesFixture(), 2, entries(10, 5))); got != "l1:1:promoted:3 l2:2:demoted:1 " {
		t.Fatal("pair", got)
	}
}

func TestLeagueCloseWaitsForEveryTimeZone(t *testing.T) {
	end := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC) // Monday after the week 2026-11-09
	if LeagueClosed("2026-11-09", end.Add(11*time.Hour+59*time.Minute)) || !LeagueClosed("2026-11-09", end.Add(12*time.Hour)) {
		t.Fatal("close boundary must be Monday 12:00 UTC (UTC-12 reaches Monday)")
	}
}

func TestLeagueResultEventsCarryOnlySafeProperties(t *testing.T) {
	events := leagueEvents("Dawn", []LeagueResult{
		{Learner: "l1", Outcome: "promoted", Rank: 1, FromTier: 1, ToTier: 2, ID: "e1", WeekStart: "2026-11-02"},
		{Learner: "l2", Outcome: "stayed", Rank: 2, FromTier: 1, ToTier: 1, ID: "e2", WeekStart: "2026-11-02"},
		{Learner: "l3", Outcome: "demoted", Rank: 3, FromTier: 2, ToTier: 1, ID: "e3", WeekStart: "2026-11-02"},
	})
	if len(events) != 2 || events[0].owner != "l1" || events[0].name != "league_promoted" || events[1].owner != "l3" || events[1].name != "league_demoted" {
		t.Fatal(events)
	}
	for _, e := range events {
		for key := range e.props {
			switch key {
			case "event_id", "week_start", "league", "rank", "from_tier", "to_tier":
			default:
				t.Fatal("unexpected property", key)
			}
		}
	}
}
