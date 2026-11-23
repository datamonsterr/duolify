package credits

import (
	"math"
	"testing"
)

func TestCheckInCostExemption(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		cost, total, checkIn int64
		paid                 bool
		want                 int64
	}{
		{"check-in", 500000000, 100, 100, false, 0},
		{"daily-free", 90000000, 46, 0, false, 90000000},
		{"mixed", 500000000, 100, 70, false, 150000000},
		{"round up", 5, 3, 1, false, 4},
		{"large allocation", 500000000, math.MaxInt64, math.MaxInt64 / 2, false, 250000001},
		{"paid cap", 500000000, 100, 100, true, 500000000},
		{"unfunded", 500, 0, 0, false, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := learnerCost(tc.cost, tc.total, tc.checkIn, tc.paid); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
