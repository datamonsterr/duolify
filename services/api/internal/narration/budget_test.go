package narration

import (
	"testing"
	"time"
)

func TestBudgetAllowanceStopsBeforeTheLimit(t *testing.T) {
	b := Budget{Limit: 40000, StopPercent: 95}
	if !b.Enabled() || b.Allowed() != 38000 {
		t.Fatalf("enabled=%v allowed=%d", b.Enabled(), b.Allowed())
	}
	if (Budget{}).Enabled() || (Budget{Limit: 40000}).Enabled() {
		t.Fatal("a budget without a limit or stop percent must not be enforced")
	}
}

func TestBudgetCycleStartFollowsTheSubscriptionDay(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		day         int
		now, expect string
	}{
		{1, "2026-11-22T10:00:00Z", "2026-11-01T00:00:00Z"},
		{15, "2026-11-22T10:00:00Z", "2026-11-15T00:00:00Z"},
		{15, "2026-11-14T23:59:59Z", "2026-10-15T00:00:00Z"},
		{15, "2026-11-15T00:00:00Z", "2026-11-15T00:00:00Z"},
		{20, "2027-01-05T08:00:00Z", "2026-12-20T00:00:00Z"},
		{0, "2026-11-22T10:00:00Z", "2026-11-01T00:00:00Z"},  // invalid day falls back to the 1st
		{31, "2026-11-30T10:00:00Z", "2026-11-28T00:00:00Z"}, // days above 28 clamp so every month has one
	} {
		got := Budget{Limit: 1, StopPercent: 1, CycleDay: c.day}.CycleStart(at(c.now))
		if !got.Equal(at(c.expect)) {
			t.Fatalf("day %d at %s: got %s want %s", c.day, c.now, got, c.expect)
		}
	}
}
