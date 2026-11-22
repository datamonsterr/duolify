package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// ClerkUserDeletionInterval is how often queued Clerk user deletions are retried.
const ClerkUserDeletionInterval = 10 * time.Minute

// ClerkUserEraser deletes the Clerk users of erased Duolify accounts. Enabled is false when
// Clerk is not configured; the job is then never scheduled and Clerk is never called.
type ClerkUserEraser interface {
	Enabled() bool
	ProcessClerkUserDeletions(ctx context.Context) (int, error)
}

// ClerkUserDeletionArgs is the content-free retry job.
type ClerkUserDeletionArgs struct{}

// Kind identifies the persisted job.
func (ClerkUserDeletionArgs) Kind() string { return "clerk_user_deletion" }

// InsertOpts makes the job unique per interval, so concurrent workers insert it once, and
// leaves retrying to the next scheduled run (every failed request stays queued).
func (ClerkUserDeletionArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: ClerkUserDeletionInterval}}
}

// ClerkUserDeletionWorker retries Clerk user deletions after account erasure.
type ClerkUserDeletionWorker struct {
	river.WorkerDefaults[ClerkUserDeletionArgs]
	Service ClerkUserEraser
	Logger  *slog.Logger
}

// Work never blocks a learner: account erasure only queues the request. A failure keeps the
// request queued and is reported by returning the error (no Clerk user id is in it).
func (w *ClerkUserDeletionWorker) Work(ctx context.Context, _ *river.Job[ClerkUserDeletionArgs]) error {
	n, err := w.Service.ProcessClerkUserDeletions(ctx)
	if n > 0 && w.Logger != nil {
		w.Logger.InfoContext(ctx, "clerk users deleted after account erasure", "count", n)
	}
	return err
}

func registerClerkUserDeletionWorker(workers *river.Workers, service any, logger *slog.Logger) []*river.PeriodicJob {
	e, ok := service.(ClerkUserEraser)
	if !ok || !e.Enabled() {
		return nil
	}
	river.AddWorker(workers, &ClerkUserDeletionWorker{Service: e, Logger: logger})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(ClerkUserDeletionInterval), func() (river.JobArgs, *river.InsertOpts) { return ClerkUserDeletionArgs{}, nil }, &river.PeriodicJobOpts{ID: "clerk-user-deletion", RunOnStart: true}),
	}
}
