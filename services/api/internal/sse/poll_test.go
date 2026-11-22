package sse

import (
	"testing"
	"time"
)

func TestPollDelayBacksOffAndResets(t *testing.T) {
	d := PollMin
	var total time.Duration
	polls := 0
	for total < time.Minute {
		total += d
		polls++
		next := NextPollDelay(d)
		if next < d {
			t.Fatalf("delay shrank from %v to %v without a state change", d, next)
		}
		d = next
	}
	if d != PollMax {
		t.Fatalf("delay settled at %v, want the %v cap", d, PollMax)
	}
	// A fixed 200 ms poll would take 300 reads in a minute.
	if polls > 40 {
		t.Fatalf("%d polls in one idle minute", polls)
	}
	if PollMin > 250*time.Millisecond || PollMax > 5*time.Second {
		t.Fatalf("poll bounds %v..%v: fast at first, capped at a few seconds", PollMin, PollMax)
	}
}
