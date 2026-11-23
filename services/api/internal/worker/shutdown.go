package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// DefaultDrainTimeout bounds how long running jobs may finish after SIGTERM (soft stop). It matches the
// API's HTTP drain; compose stop_grace_period must stay above it plus hardStopGrace.
const DefaultDrainTimeout = 90 * time.Second

// hardStopGrace is the time jobs get to return after their context is cancelled.
const hardStopGrace = 10 * time.Second

// DrainTimeoutFrom reads a Go duration from key; empty or invalid keeps the default.
func DrainTimeoutFrom(getenv func(string) string, key string) time.Duration {
	if d, err := time.ParseDuration(getenv(key)); err == nil && d > 0 {
		return d
	}
	return DefaultDrainTimeout
}

// StopQueue stops fetching new jobs and waits up to drain for running ones (River soft stop). Jobs still
// running then get their context cancelled (hard stop) so they return and are retried at once instead of
// staying "running" until River's rescuer finds them an hour later.
func StopQueue(client *river.Client[pgx.Tx], drain time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), drain)
	defer cancel()
	if err := client.Stop(ctx); err != nil {
		hardCtx, hardCancel := context.WithTimeout(context.Background(), hardStopGrace)
		defer hardCancel()
		return errors.Join(err, client.StopAndCancel(hardCtx))
	}
	return nil
}

// StopQueues stops every queue at once: all stop fetching immediately and share one drain window.
func StopQueues(clients []*river.Client[pgx.Tx], drain time.Duration) error {
	errs := make([]error, len(clients))
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Go(func() { errs[i] = StopQueue(c, drain) })
	}
	wg.Wait()
	return errors.Join(errs...)
}
