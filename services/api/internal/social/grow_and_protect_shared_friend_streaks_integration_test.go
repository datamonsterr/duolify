//go:build integration

package social

import (
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// Auckland is 21 hours ahead of Los Angeles in November, so A's local date d overlaps
// B's d-1 in real time; the shared day pairs A d with B d-1 (ADR 0011, E6-05).
const (
	zoneA = "Pacific/Auckland"
	zoneB = "America/Los_Angeles"
)

func (r *rig) frozenClock(at string) {
	r.t.Helper()
	now, err := time.Parse(time.RFC3339, at)
	if err != nil {
		r.t.Fatal(err)
	}
	r.svc.now = func() time.Time { return now }
}

// side seeds one learner's E5 projection: time zone, completion days, protections and
// the last judged local date.
func (r *rig) side(p pair, zone, decided string, active, protected []string) {
	r.t.Helper()
	exec := func(sql string, args ...any) {
		r.t.Helper()
		if _, err := r.db.Pool.Exec(r.ctx, sql, args...); err != nil {
			r.t.Fatal(err)
		}
	}
	exec(`UPDATE learner_profiles SET time_zone=$2 WHERE learner_id=$1::uuid`, p.id, zone)
	for _, d := range active {
		exec(`INSERT INTO streak_days(learner_id,local_date,first_event_id) VALUES($1::uuid,$2::date,gen_random_uuid()) ON CONFLICT DO NOTHING`, p.id, d)
	}
	for _, d := range protected {
		exec(`INSERT INTO streak_day_protections(learner_id,protected_on,remaining,streak_length) VALUES($1::uuid,$2::date,0,3) ON CONFLICT DO NOTHING`, p.id, d)
	}
	exec(`INSERT INTO streak_state(learner_id,current_days,longest_days,decided_through,updated_at) VALUES($1::uuid,0,0,$2::date,now())
 ON CONFLICT(learner_id) DO UPDATE SET decided_through=EXCLUDED.decided_through`, p.id, decided)
}

func (r *rig) oldFriends(a, b pair) {
	r.t.Helper()
	r.befriend(a, b)
	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE friendships SET created_at='2026-11-01T00:00:00Z'`); err != nil {
		r.t.Fatal(err)
	}
}

type streakRow struct {
	Days, Longest, Run int
}

func (r *rig) streak(a, b pair) (streakRow, bool) {
	r.t.Helper()
	lo, hi := orderedPair(a.id, b.id)
	var s streakRow
	rows, err := r.db.Pool.Query(r.ctx, `SELECT current_days,longest_days,run_no FROM friend_streaks WHERE learner_low=$1::uuid AND learner_high=$2::uuid`, lo, hi)
	if err != nil {
		r.t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return s, false
	}
	if err = rows.Scan(&s.Days, &s.Longest, &s.Run); err != nil {
		r.t.Fatal(err)
	}
	return s, true
}

func (r *rig) rowCount(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.db.Pool.QueryRow(r.ctx, sql, args...).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

func (r *rig) evaluate(p pair) {
	r.t.Helper()
	if err := r.svc.EvaluateFriendStreaks(r.ctx, p.id); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) wantStreak(a, b pair, days int) {
	r.t.Helper()
	got, ok := r.streak(a, b)
	if !ok || got.Days != days {
		r.t.Fatalf("friend streak = %+v exists=%v, want %d days", got, ok, days)
	}
}

func (r *rig) view(p pair) map[string]any {
	r.t.Helper()
	friends := r.friends(p)
	if len(friends) == 0 {
		r.t.Fatal("no friends listed")
	}
	view, ok := friends[0]["friend_streak"].(map[string]any)
	if !ok {
		r.t.Fatalf("friend_streak missing: %v", friends[0])
	}
	return view
}

func (r *rig) eventProps(learner, name string) []map[string]any {
	var out []map[string]any
	for _, e := range r.sink.named(name) {
		if e["id"] == learner {
			out = append(out, e["props"].(map[string]any))
		}
	}
	return out
}

// Both complete on their own local dates in two zones: the streak grows exactly once
// per shared day however often (or concurrently) it is evaluated.
func TestBothCompleteGrowsOncePerSharedDayAcrossTwoZones(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Grow.Auckland"), r.friend("Grow.Angeles")
	r.oldFriends(a, b)
	r.frozenClock("2026-11-14T12:00:00Z") // Auckland 11-15 01:00, Los Angeles 11-14 04:00
	r.side(a, zoneA, "2026-11-14", []string{"2026-11-12", "2026-11-13", "2026-11-14"}, nil)
	r.side(b, zoneB, "2026-11-13", []string{"2026-11-11", "2026-11-12", "2026-11-13"}, nil)

	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(p pair) {
			defer wg.Done()
			_ = r.svc.EvaluateFriendStreaks(r.ctx, p.id)
		}([]pair{a, b}[i%2])
	}
	wg.Wait()
	r.evaluate(a)
	r.wantStreak(a, b, 3)
	if n := r.rowCount(`SELECT count(*) FROM friend_streak_days WHERE outcome='extended'`); n != 3 {
		t.Fatalf("pair-day rows = %d, want 3", n)
	}
	for _, p := range []pair{a, b} {
		if n := len(r.eventProps(p.id, "friend_streak_started")); n != 1 {
			t.Fatalf("started events for %s = %d", p.name, n)
		}
		if n := len(r.eventProps(p.id, "friend_streak_extended")); n != 2 {
			t.Fatalf("extended events for %s = %d", p.name, n)
		}
	}
}

// Each friend sees their own local date and the streak waits for the later day-end.
func TestWaitsForTheLaterDayEndBeforeBreaking(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Wait.Auckland"), r.friend("Wait.Angeles")
	r.oldFriends(a, b)
	r.frozenClock("2026-11-15T12:00:00Z") // Auckland 11-16 01:00, Los Angeles 11-15 04:00
	// Day 11-15 (A) / 11-14 (B): A missed and A's day is decided; B's 11-14 is decided
	// only when B is also past it.
	r.side(a, zoneA, "2026-11-15", []string{"2026-11-13", "2026-11-14"}, nil)
	r.side(b, zoneB, "2026-11-13", []string{"2026-11-12", "2026-11-13", "2026-11-14"}, nil)
	r.evaluate(a)
	r.wantStreak(a, b, 2)
	view := r.view(a)
	if view["state"] != "checking" || view["days"] != float64(2) {
		t.Fatalf("view = %v, want checking at 2 days", view)
	}
	you, _ := view["you"].(map[string]any)
	friend, _ := view["friend"].(map[string]any)
	if you["time_zone"] != zoneA || friend["time_zone"] != zoneB || you["local_date"] == friend["local_date"] {
		t.Fatalf("each friend must see their own local date: %v %v", you, friend)
	}
	if len(r.eventProps(a.id, "friend_streak_broken")) != 0 {
		t.Fatal("broke before the later day-end")
	}
	// The later day-end passes: B's 11-14 is decided and B is active, A missed it unprotected.
	r.side(b, zoneB, "2026-11-14", nil, nil)
	r.evaluate(b)
	r.wantStreak(a, b, 0)
	if n := len(r.eventProps(a.id, "friend_streak_broken")); n != 1 {
		t.Fatalf("broken events = %d", n)
	}
	if props := r.eventProps(b.id, "friend_streak_broken")[0]; props["reason"] != "missed" || props["previous_length"] != 2 {
		t.Fatalf("broken props = %v", props)
	}
}

// One missing side needs its own freeze; both missing need both protected. No freeze is
// spent from the other friend and no second inventory is created.
func TestProtectOneOrBothMissedSidesWithOwnFreeze(t *testing.T) {
	cases := []struct {
		name              string
		aProtected, bProt bool
		bActive           bool
		wantDays          int
		wantProtectedDays int
	}{
		{"a missed with own freeze", true, false, true, 2, 1},
		{"a missed without freeze while b holds one", false, true, true, 0, 0},
		{"both missed both protected", true, true, false, 2, 1},
		{"both missed only one protected", true, false, false, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			a, b := r.friend("Prot.Auckland"), r.friend("Prot.Angeles")
			r.oldFriends(a, b)
			r.frozenClock("2026-11-15T12:00:00Z") // Auckland 11-16, Los Angeles 11-15
			aDays, bDays := []string{"2026-11-13", "2026-11-14"}, []string{"2026-11-12", "2026-11-13"}
			var aProt, bProt []string
			if c.aProtected {
				aProt = []string{"2026-11-15"}
			}
			if c.bProt {
				bProt = []string{"2026-11-14"}
			}
			if c.bActive {
				bDays = append(bDays, "2026-11-14")
			}
			r.side(a, zoneA, "2026-11-15", aDays, aProt)
			r.side(b, zoneB, "2026-11-14", bDays, bProt)
			before := r.rowCount(`SELECT count(*) FROM streak_day_protections`)
			r.evaluate(a)
			r.wantStreak(a, b, c.wantDays)
			if n := r.rowCount(`SELECT count(*) FROM friend_streak_days WHERE outcome='protected'`); n != c.wantProtectedDays {
				t.Fatalf("protected days = %d, want %d", n, c.wantProtectedDays)
			}
			if after := r.rowCount(`SELECT count(*) FROM streak_day_protections`); after != before {
				t.Fatalf("friend streak consumed freezes: %d -> %d", before, after)
			}
			if r.rowCount(`SELECT count(*) FROM streak_freeze_inventory`) != 0 {
				t.Fatal("friend streak must not create an inventory")
			}
		})
	}
}

// One learner completing with many friends concurrently: one pair-day key per friend,
// a milestone freeze for the learner deduped across friends and capped.
func TestMultiFriendRaceAndMilestoneFreezeDedupe(t *testing.T) {
	r := newRig(t)
	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE gamification_config SET friend_streak_milestone_days=2`); err != nil {
		t.Fatal(err)
	}
	a := r.friend("Race.Alpha")
	friends := []pair{r.friend("Race.Bravo"), r.friend("Race.Charlie"), r.friend("Race.Delta"), r.friend("Race.Echo")}
	for _, f := range friends {
		r.befriend(a, f)
	}
	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE friendships SET created_at='2026-11-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	r.frozenClock("2026-11-14T12:00:00Z")
	r.side(a, "UTC", "2026-11-13", []string{"2026-11-13", "2026-11-14"}, nil)
	for _, f := range friends {
		r.side(f, "UTC", "2026-11-13", []string{"2026-11-13", "2026-11-14"}, nil)
	}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		for _, p := range append([]pair{a}, friends...) {
			wg.Add(1)
			go func(p pair) {
				defer wg.Done()
				_ = r.svc.EvaluateFriendStreaks(r.ctx, p.id)
			}(p)
		}
	}
	wg.Wait()
	for _, f := range friends {
		r.wantStreak(a, f, 2)
	}
	if n := r.rowCount(`SELECT count(*) FROM friend_streak_days`); n != 8 {
		t.Fatalf("pair-day rows = %d, want 8", n)
	}
	if n := r.rowCount(`SELECT count(*) FROM friend_streak_milestones`); n != 4 {
		t.Fatalf("pair milestone keys = %d, want 4", n)
	}
	grants := r.rowCount(`SELECT count(*) FROM streak_freeze_milestones WHERE learner_id=$1::uuid AND kind='friend_streak' AND granted`, a.id)
	if grants != 1 {
		t.Fatalf("alpha friend-streak freezes = %d, want 1 deduped across %d friends", grants, len(friends))
	}
	if held := r.rowCount(`SELECT held FROM streak_freeze_inventory WHERE learner_id=$1::uuid`, a.id); held != 1 {
		t.Fatalf("alpha inventory = %d, want 1", held)
	}
	if n := len(r.eventProps(a.id, "streak_freeze_earned")); n != 1 || r.eventProps(a.id, "streak_freeze_earned")[0]["reason"] != "friend_streak" {
		t.Fatalf("alpha freeze events = %v", r.eventProps(a.id, "streak_freeze_earned"))
	}
	if r.rowCount(`SELECT count(*) FROM credit_entries WHERE kind='reward'`) != 0 {
		t.Fatal("milestone freezes must never cost or grant credits")
	}
}

func TestMilestoneAtFreezeCapRecordsNoGrant(t *testing.T) {
	r := newRig(t)
	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE gamification_config SET friend_streak_milestone_days=2,freeze_cap=0`); err != nil {
		t.Fatal(err)
	}
	a, b := r.friend("Cap.Alpha"), r.friend("Cap.Bravo")
	r.oldFriends(a, b)
	r.frozenClock("2026-11-14T12:00:00Z")
	r.side(a, "UTC", "2026-11-13", []string{"2026-11-13", "2026-11-14"}, nil)
	r.side(b, "UTC", "2026-11-13", []string{"2026-11-13", "2026-11-14"}, nil)
	r.evaluate(a)
	r.evaluate(a)
	r.wantStreak(a, b, 2)
	if r.rowCount(`SELECT count(*) FROM streak_freeze_milestones WHERE kind='friend_streak' AND NOT granted`) != 2 ||
		r.rowCount(`SELECT count(*) FROM streak_freeze_milestones WHERE granted`) != 0 || len(r.sink.named("streak_freeze_earned")) != 0 {
		t.Fatal("at the cap the milestone must be recorded once without a grant or event")
	}
}

// The friendship ending is the one place the streak ends: remove, block, deletion.
func TestFriendStreakEndsWithRemoveBlockAndAccountDeletion(t *testing.T) {
	r := newRig(t)
	a, b, c, d := r.friend("End.Alpha"), r.friend("End.Bravo"), r.friend("End.Charlie"), r.friend("End.Delta")
	r.frozenClock("2026-11-14T12:00:00Z")
	for _, f := range []pair{b, c, d} {
		r.befriend(a, f)
	}
	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE friendships SET created_at='2026-11-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []pair{a, b, c, d} {
		r.side(p, "UTC", "2026-11-13", []string{"2026-11-13", "2026-11-14"}, nil)
	}
	r.evaluate(a)
	for _, f := range []pair{b, c, d} {
		r.wantStreak(a, f, 2)
	}
	friendID := ""
	for _, f := range r.friends(a) {
		if f["username"] == "end.bravo" {
			friendID = f["id"].(string)
		}
	}
	if s, o := r.act(a, "DELETE", "/v1/social/friends/"+friendID); s != 200 {
		t.Fatal(s, o)
	}
	if _, ok := r.streak(a, b); ok {
		t.Fatal("removal left the pair state")
	}
	if s, o := r.block(a, c.name); s != 200 {
		t.Fatal(s, o)
	}
	if _, ok := r.streak(a, c); ok {
		t.Fatal("block left the pair state")
	}
	if _, err := r.db.Pool.Exec(r.ctx, `DELETE FROM learners WHERE id=$1::uuid`, d.id); err != nil {
		t.Fatal(err)
	}
	if n := r.rowCount(`SELECT count(*) FROM friend_streaks`) + r.rowCount(`SELECT count(*) FROM friend_streak_days`) + r.rowCount(`SELECT count(*) FROM friend_streak_milestones`); n != 0 {
		t.Fatalf("%d pair rows survived", n)
	}
	broken := r.eventProps(a.id, "friend_streak_broken")
	reasons := []string{}
	for _, p := range broken {
		reasons = append(reasons, fmt.Sprint(p["reason"]))
	}
	slices.Sort(reasons)
	if !slices.Equal(reasons, []string{"friendship_ended", "friendship_ended"}) {
		t.Fatalf("alpha broken reasons = %v", reasons)
	}
}

// show_streak_to_friends off hides the Friend streak and every day-level detail.
func TestFriendStreakViewHonoursShowStreakSetting(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("View.Alpha"), r.friend("View.Bravo")
	r.oldFriends(a, b)
	r.frozenClock("2026-11-14T12:00:00Z")
	r.side(a, "UTC", "2026-11-13", []string{"2026-11-13", "2026-11-14"}, nil)
	r.side(b, "UTC", "2026-11-13", []string{"2026-11-13", "2026-11-14"}, nil)
	r.evaluate(a)
	view := r.view(a)
	if view["days"] != float64(2) || view["state"] != "both_done" {
		t.Fatalf("view = %v", view)
	}
	if s, o, _ := r.call("PATCH", "/v1/social/settings", `{"show_streak_to_friends":false}`, b.token); s != 200 {
		t.Fatal(s, o)
	}
	if got := r.friends(a)[0]; got["friend_streak"] != nil || got["streak"] != nil {
		t.Fatalf("streak exposed with the setting off: %v", got)
	}
}
