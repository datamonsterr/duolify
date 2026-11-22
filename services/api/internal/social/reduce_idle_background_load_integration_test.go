//go:build integration

package social

import "testing"

// The sweep is a safety net for a missed settle hook: it leaves recently checked pairs alone
// and settles a stale pair once, and two workers sweeping together settle it once.
func TestFriendStreakSweepSettlesOnlyStalePairs(t *testing.T) {
	r := newRig(t)
	a, b := r.friend("Sweep.Auckland"), r.friend("Sweep.Angeles")
	r.oldFriends(a, b)
	r.frozenClock("2026-11-15T12:00:00Z") // Auckland 11-16 01:00, Los Angeles 11-15 04:00
	r.side(a, zoneA, "2026-11-15", []string{"2026-11-13", "2026-11-14"}, nil)
	r.side(b, zoneB, "2026-11-13", []string{"2026-11-12", "2026-11-13", "2026-11-14"}, nil)
	r.evaluate(a)
	r.wantStreak(a, b, 2)

	// B's day 11-14 is now decided and A missed it unprotected, but no hook ran.
	r.side(b, zoneB, "2026-11-14", nil, nil)
	if n, err := r.svc.sweepFriendStreaks(r.ctx); err != nil || n != 0 {
		t.Fatalf("a pair checked a moment ago was swept: n=%d err=%v", n, err)
	}
	r.wantStreak(a, b, 2)

	if _, err := r.db.Pool.Exec(r.ctx, `UPDATE friend_streaks SET updated_at=now()-interval '16 minutes'`); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	counts := make(chan int, 2)
	for range 2 {
		go func() {
			n, err := r.svc.sweepFriendStreaks(r.ctx)
			counts <- n
			errs <- err
		}()
	}
	swept := 0
	for range 2 {
		swept += <-counts
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if swept < 1 {
		t.Fatalf("a stale pair was not swept: %d", swept)
	}
	r.wantStreak(a, b, 0)
	if n := len(r.eventProps(a.id, "friend_streak_broken")); n != 1 {
		t.Fatalf("broken events = %d, want exactly 1 across two workers", n)
	}
	if n, err := r.svc.sweepFriendStreaks(r.ctx); err != nil || n != 0 {
		t.Fatalf("a settled pair is swept again: n=%d err=%v", n, err)
	}
}
