package credits

import (
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"testing"
	"time"
)

func TestCheckInGrantCapAndFullBonus(t *testing.T) {
	cfg := pricing.CheckInConfig{DailyCredits: 150, FirstTimeBonus: 250, BankDays: 7}
	for _, tt := range []struct {
		bank  int64
		first bool
		want  int64
	}{{0, true, 400}, {1049, false, 1}, {1050, false, 0}, {1049, true, 251}, {1300, false, 0}} {
		if got := checkInAmount(cfg, tt.bank, tt.first); got != tt.want {
			t.Errorf("bank=%d first=%v got %d want %d", tt.bank, tt.first, got, tt.want)
		}
	}
}
func TestCheckInCalendarMidnightDST(t *testing.T) {
	for _, tt := range []struct{ at, day, next string }{{"2026-03-08T05:00:00Z", "2026-03-08", "2026-03-09T04:00:00Z"}, {"2026-11-01T04:00:00Z", "2026-11-01", "2026-11-02T05:00:00Z"}} {
		at, _ := time.Parse(time.RFC3339, tt.at)
		day, next, err := checkInDay(at, "America/New_York")
		if err != nil || day != tt.day || next.UTC().Format(time.RFC3339) != tt.next {
			t.Fatalf("%s %s %v", day, next, err)
		}
	}
}

func TestCheckInMidnightDSTGap(t *testing.T) {
	at, _ := time.Parse(time.RFC3339, "2018-11-03T12:00:00Z")
	day, next, err := checkInDay(at, "America/Sao_Paulo")
	if err != nil || day != "2018-11-03" || next.UTC().Format(time.RFC3339) != "2018-11-04T03:00:00Z" {
		t.Fatalf("day=%s next=%s err=%v", day, next, err)
	}
}
