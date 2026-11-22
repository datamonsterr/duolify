package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// PersonDeletionInterval is how often queued PostHog person deletions are retried.
const PersonDeletionInterval = 10 * time.Minute

// PersonEraser deletes the PostHog persons of erased learners. PersonDeletionEnabled is false when
// the PostHog personal API key is not configured; the job is then never scheduled.
type PersonEraser interface {
	PersonDeletionEnabled() bool
	ProcessPersonDeletions(ctx context.Context) (int, error)
}

// PersonDeletionArgs is the content-free retry job.
type PersonDeletionArgs struct{}

// Kind identifies the persisted job.
func (PersonDeletionArgs) Kind() string { return "posthog_person_deletion" }

// InsertOpts makes the job unique per interval; failed requests stay queued for the next run.
func (PersonDeletionArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 1, UniqueOpts: river.UniqueOpts{ByPeriod: PersonDeletionInterval}}
}

// PersonDeletionWorker retries PostHog person deletions after account erasure.
type PersonDeletionWorker struct {
	river.WorkerDefaults[PersonDeletionArgs]
	Service PersonEraser
	Logger  *slog.Logger
}

// Work never blocks a learner: account erasure only queues the request. Failures are logged
// with counts only (no learner id).
func (w *PersonDeletionWorker) Work(ctx context.Context, _ *river.Job[PersonDeletionArgs]) error {
	n, err := w.Service.ProcessPersonDeletions(ctx)
	if w.Logger != nil {
		if n > 0 {
			w.Logger.InfoContext(ctx, "posthog persons deleted after account erasure", "count", n)
		}
		if err != nil {
			w.Logger.ErrorContext(ctx, "posthog person deletion failed", "deleted", n, "failed_batches", 1)
		}
	}
	if err != nil {
		return errors.New("posthog person deletion batch failed")
	}
	return nil
}

func registerPersonDeletionWorker(workers *river.Workers, service any, logger *slog.Logger) []*river.PeriodicJob {
	e, ok := service.(PersonEraser)
	if !ok || !e.PersonDeletionEnabled() {
		return nil
	}
	river.AddWorker(workers, &PersonDeletionWorker{Service: e, Logger: logger})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(PersonDeletionInterval), func() (river.JobArgs, *river.InsertOpts) { return PersonDeletionArgs{}, nil }, &river.PeriodicJobOpts{ID: "posthog-person-deletion", RunOnStart: true}),
	}
}
