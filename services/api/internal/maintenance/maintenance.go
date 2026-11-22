// Package maintenance runs background sweeps. Only the worker process starts them; API
// replicas serve HTTP and keep nothing but per-process in-memory refreshes.
package maintenance

import (
	"context"
	"time"
)

// Loop runs pass until ctx ends. A pass reports whether it found work: a busy pass runs
// again after min, every idle pass doubles the wait up to max, so an idle process sends
// a handful of statements a minute instead of a poll per second. The first pass runs at once.
func Loop(ctx context.Context, minWait, maxWait time.Duration, pass func(context.Context) (busy bool)) {
	wait := minWait
	for {
		if pass(ctx) {
			wait = minWait
		} else {
			wait = min(wait*2, maxWait)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// Every runs pass at a fixed interval until ctx ends; the first pass runs at once.
func Every(ctx context.Context, interval time.Duration, pass func(context.Context)) {
	Loop(ctx, interval, interval, func(ctx context.Context) bool {
		pass(ctx)
		return false
	})
}
