package courses

import (
	"testing"
	"time"
)

func TestCreationPollDelayBacksOffAndResets(t *testing.T) {
	d := creationPollMin
	var total time.Duration
	polls := 0
	for total < time.Minute {
		total += d
		polls++
		next := nextCreationPollDelay(d)
		if next < d {
			t.Fatalf("delay shrank from %v to %v without a state change", d, next)
		}
		d = next
	}
	if d != creationPollMax {
		t.Fatalf("delay settled at %v, want the %v cap", d, creationPollMax)
	}
	// A fixed 200 ms poll would take 300 reads in a minute.
	if polls > 40 {
		t.Fatalf("%d polls in one idle minute", polls)
	}
	if creationPollMin > 250*time.Millisecond || creationPollMax > 5*time.Second {
		t.Fatalf("poll bounds %v..%v: fast at first, capped at a few seconds", creationPollMin, creationPollMax)
	}
}
