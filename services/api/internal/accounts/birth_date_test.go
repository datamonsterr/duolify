package accounts_test

import (
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

func TestValidateBirthDateStrictCalendarAndBounds(t *testing.T) {
	now := time.Date(2026, 11, 16, 23, 59, 59, 0, time.UTC)
	for _, raw := range []string{"1900-01-01", "2000-02-29", "2026-11-16"} {
		if value, err := accounts.ValidateBirthDate(raw, now); err != nil || value.Format(time.DateOnly) != raw {
			t.Errorf("valid %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"", "1899-12-31", "2026-11-17", "2023-02-29", "2026-04-31", "2000-1-01", "2000-01-01T00:00:00Z", " 2000-01-01", "infinity"} {
		if _, err := accounts.ValidateBirthDate(raw, now); err == nil {
			t.Errorf("accepted invalid %q", raw)
		}
	}
}

func TestMinimumAgeUsesUTCCalendarBirthdays(t *testing.T) {
	for _, tc := range []struct {
		birth, now string
		want       bool
	}{
		{"2008-11-16", "2026-11-16T00:00:00Z", true},
		{"2008-11-17", "2026-11-16T23:59:59Z", false},
		{"2008-11-16", "2026-11-16T00:30:00+07:00", false},
		{"2008-11-16", "2026-11-15T23:30:00-02:00", true},
		{"2008-02-29", "2026-02-28T23:59:59Z", false},
		{"2008-02-29", "2026-03-01T00:00:00Z", true},
		{"2027-01-01", "2026-11-16T00:00:00Z", false},
	} {
		birth, _ := time.Parse(time.DateOnly, tc.birth)
		now, _ := time.Parse(time.RFC3339, tc.now)
		if got := accounts.MeetsMinimumAge(birth, now, 18); got != tc.want {
			t.Errorf("%s at %s=%v", tc.birth, tc.now, got)
		}
	}
}

func TestBirthDateRestorePIIClassification(t *testing.T) {
	if backup.ClassifyColumn("learners", "birth_date") != backup.ClassRedacted {
		t.Fatal("account birth date must be explicitly redacted PII")
	}
}
