package backup

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// Args contains only the intended UTC day, never learner data or secrets.
type Args struct {
	Date string `json:"date" river:"unique"`
}

// Kind is the stable persisted River job kind.
func (Args) Kind() string { return "database_backup" }

// InsertOpts bounds retry attempts and serializes backups in their own queue.
func (Args) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 3, Queue: "backups", UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// Worker adapts the backup service to River's durable retry mechanism.
type Worker struct {
	river.WorkerDefaults[Args]
	Service *Service
	Logger  *slog.Logger
}

// Work delegates to the service; global worker middleware traces and alerts retries.
func (w *Worker) Work(ctx context.Context, job *river.Job[Args]) error {
	return w.Service.Run(ctx, job.Args.Date)
}

// RetryAfter backs off briefly before durable retries (2 s, then 4 s).
func (*Worker) RetryAfter(job *river.Job[Args]) time.Time {
	delay := time.Duration(2) * time.Second
	if job.Attempt > 1 {
		delay = 4 * time.Second
	}
	return time.Now().Add(delay)
}

// Timeout bounds subprocesses and upload work within the job deadline.
func (*Worker) Timeout(*river.Job[Args]) time.Duration { return 30 * time.Minute }

// Nightly is a calendar schedule at 02:00 UTC, independent of host timezone/DST.
type Nightly struct{}

// Next returns the next strictly later nightly boundary.
func (Nightly) Next(current time.Time) time.Time {
	utc := current.UTC()
	next := time.Date(utc.Year(), utc.Month(), utc.Day(), 2, 0, 0, 0, time.UTC)
	if !next.After(current) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

// Periodic catches up after startup or leader changes; object idempotency avoids
// repeating the dump when River's in-memory periodic scheduler restarts.
func Periodic() *river.PeriodicJob {
	return river.NewPeriodicJob(Nightly{}, func() (river.JobArgs, *river.InsertOpts) {
		return Args{Date: time.Now().UTC().Format(time.DateOnly)}, nil
	}, &river.PeriodicJobOpts{ID: "nightly-database-backup", RunOnStart: true})
}
