package social

import (
	"strings"
	"testing"
	"time"
)

func TestNudgeDayFollowsTheSendersZoneAndReportsNextMidnight(t *testing.T) {
	cases := []struct {
		now, zone, date, next string
	}{
		{"2026-11-14T11:10:00Z", "Pacific/Auckland", "2026-11-15", "2026-11-15T11:00:00Z"},
		{"2026-11-14T11:10:00Z", "America/Los_Angeles", "2026-11-14", "2026-11-15T08:00:00Z"},
		{"2026-11-14T23:59:59Z", "UTC", "2026-11-14", "2026-11-15T00:00:00Z"},
		{"2026-11-14T00:00:00Z", "UTC", "2026-11-14", "2026-11-15T00:00:00Z"},
		{"2026-11-14T12:00:00Z", "Not/AZone", "2026-11-14", "2026-11-15T00:00:00Z"}, // unknown zone falls back to UTC
		{"2026-11-14T12:00:00Z", "", "2026-11-14", "2026-11-15T00:00:00Z"},
	}
	for _, c := range cases {
		now, _ := time.Parse(time.RFC3339, c.now)
		date, next := nudgeDay(now, c.zone)
		if date != c.date || next.UTC().Format(time.RFC3339) != c.next {
			t.Errorf("nudgeDay(%s,%q) = %s %s, want %s %s", c.now, c.zone, date, next.UTC().Format(time.RFC3339), c.date, c.next)
		}
	}
}

func TestParseNudgeBodyAcceptsOnlyAnOperationKey(t *testing.T) {
	const good = "8d1cf3a2-34a2-4b5b-9d3a-3bfa1c7c6f10"
	for _, body := range []string{`{"operation_key":"` + good + `"}`, `{}`, ``} {
		key, err := parseNudgeBody(strings.NewReader(body))
		if err != nil {
			t.Errorf("%q rejected: %v", body, err)
		}
		if body != "" && body != "{}" && key != good {
			t.Errorf("key = %q", key)
		}
	}
	for _, body := range []string{`{"operation_key":"nope"}`, `{"operation_key":"` + good + `","extra":1}`, `{"operation_key":"` + good + `"} {}`, `[]`, `{"operation_key":7}`, `{"operation_key":"` + strings.Repeat("a", 5000) + `"}`} {
		if _, err := parseNudgeBody(strings.NewReader(body)); err == nil {
			t.Errorf("%.40q accepted", body)
		}
	}
}

func TestNudgeRejectionsAreNeutralAndCarryRetryAfter(t *testing.T) {
	unavailable := &Rejection{Code: CodeNudgeUnavailable}
	if unavailable.Status() != 404 || unavailable.Error() != "We couldn't send that nudge." {
		t.Fatal(unavailable.Status(), unavailable.Error())
	}
	limited := &Rejection{Code: CodeNudgeLimited, RetryAfter: 90 * time.Minute}
	if limited.Status() != 429 || limited.RetryAfterSeconds() != 5400 {
		t.Fatal(limited.Status(), limited.RetryAfterSeconds())
	}
	for _, r := range []*Rejection{unavailable, limited} {
		for _, leak := range []string{"block", "friend", "setting", "finished", "deleted"} {
			if strings.Contains(strings.ToLower(r.Error()), leak) {
				t.Errorf("%s message reveals %q", r.Code, leak)
			}
		}
	}
}

func TestNudgeEligibilityTable(t *testing.T) {
	cases := []struct {
		name                   string
		friends, blocked, done bool
		want                   bool
	}{
		{"friends", true, false, false, true},
		{"not friends", false, false, false, false},
		{"blocked", true, true, false, false},
		{"already finished", true, false, true, false},
		{"everything wrong", false, true, true, false},
	}
	for _, c := range cases {
		if got := nudgeAllowed(c.friends, c.blocked, c.done); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestNudgeSentEventCarriesOnlyUUIDAndDate(t *testing.T) {
	props := nudgeSentProps("8d1cf3a2-34a2-4b5b-9d3a-3bfa1c7c6f10", "2026-11-14")
	if len(props) != 2 || props["event_id"] != "8d1cf3a2-34a2-4b5b-9d3a-3bfa1c7c6f10" || props["local_date"] != "2026-11-14" {
		t.Fatal(props)
	}
}
