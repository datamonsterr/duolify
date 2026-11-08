// Package worker configures the Postgres-backed River queue.
package worker

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// DummyArgs is a side-effect-free bootstrap job with a stable queue kind.
type DummyArgs struct{}

// Kind identifies the job on disk.
func (DummyArgs) Kind() string { return "bootstrap_dummy" }

type dummyWorker struct {
	river.WorkerDefaults[DummyArgs]
	logger *slog.Logger
}

func (w *dummyWorker) Work(ctx context.Context, job *river.Job[DummyArgs]) error {
	w.logger.InfoContext(ctx, "dummy job completed", "job_id", job.ID)
	return nil
}

// New registers the dummy worker and a bounded default queue.
func New(pool *pgxpool.Pool, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &dummyWorker{logger: logger})
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:  logger,
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}},
		Workers: workers,
	})
}

// Migrate applies River's own pinned schema, separate from application Goose versions.
func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{Logger: logger})
	if err != nil {
		return err
	}
	_, err = migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	return err
}

// Run starts queue processing and waits for a signal context, then drains workers.
func Run(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	client, err := New(pool, logger)
	if err != nil {
		return err
	}
	if err := client.Start(context.WithoutCancel(ctx)); err != nil {
		return err
	}
	<-ctx.Done()
	// Stop must receive a fresh context because the signal context is canceled.
	return stop(client)
}
