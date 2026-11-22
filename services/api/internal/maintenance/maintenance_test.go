package maintenance_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/maintenance"
)

func TestLoopBacksOffWhenIdleAndResetsWhenBusy(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var passes []time.Time
	done := make(chan struct{})
	go func() {
		defer close(done)
		maintenance.Loop(ctx, 5*time.Millisecond, 40*time.Millisecond, func(context.Context) bool {
			passes = append(passes, time.Now())
			if len(passes) == 8 {
				cancel()
			}
			return len(passes) == 4 // one busy pass in the middle
		})
	}()
	<-done
	if len(passes) != 8 {
		t.Fatalf("passes=%d, want 8", len(passes))
	}
	gap := func(i int) time.Duration { return passes[i].Sub(passes[i-1]) }
	// Idle: 10ms then 20ms then 40ms; busy pass 4 resets the wait to the minimum.
	if gap(3) < 18*time.Millisecond || gap(2) < 8*time.Millisecond {
		t.Errorf("idle passes do not back off: %v %v", gap(2), gap(3))
	}
	if gap(4) > 15*time.Millisecond {
		t.Errorf("a busy pass must reset the wait to the minimum, waited %v", gap(4))
	}
}

func TestLoopStopsWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var n atomic.Int32
	finished := make(chan struct{})
	go func() {
		maintenance.Every(ctx, time.Hour, func(context.Context) { n.Add(1) })
		close(finished)
	}()
	for n.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Every did not stop after cancellation")
	}
	if n.Load() != 1 {
		t.Fatalf("passes=%d, want exactly the immediate first pass", n.Load())
	}
}
