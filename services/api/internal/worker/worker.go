// Package worker configures the Postgres-backed River queue.
package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
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

// New registers bounded queues and optionally the nightly backup service. The
// side-effect-free bootstrap job remains available for queue migration checks.
func New(pool *pgxpool.Pool, logger *slog.Logger, backups ...*backup.Service) (*river.Client[pgx.Tx], error) {
	monitor := telemetry.New(logger)
	if len(backups) > 0 && backups[0] != nil && backups[0].Monitor() != nil {
		monitor = backups[0].Monitor()
	}
	return NewObserved(pool, logger, monitor, backups...)
}

// NewObserved also supports monitored queues without a backup service.
func NewObserved(pool *pgxpool.Pool, logger *slog.Logger, monitor *telemetry.Monitor, backups ...*backup.Service) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &dummyWorker{logger: logger})
	periodic := []*river.PeriodicJob{}
	if len(backups) > 0 && backups[0] != nil {
		river.AddWorker(workers, &backup.Worker{Service: backups[0], Logger: logger})
		periodic = append(periodic, backup.Periodic())
	}
	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Middleware:   []rivertype.Middleware{NewTelemetry(monitor)},
		Logger:       logger,
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 4}, "backups": {MaxWorkers: 1}},
		Workers:      workers,
		PeriodicJobs: periodic,
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
func Run(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, backups ...*backup.Service) error {
	monitor := telemetry.New(logger)
	if len(backups) > 0 && backups[0] != nil && backups[0].Monitor() != nil {
		monitor = backups[0].Monitor()
	}
	watchCtx, watchCancel := context.WithCancel(ctx)
	defer watchCancel()
	go WatchQueue(watchCtx, monitor, pool.Ping, time.Second*15)
	client, err := NewObserved(pool, logger, monitor, backups...)
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

// WatchQueue observes idle queue outages too, without depending on analytics delivery.
func WatchQueue(ctx context.Context, monitor *telemetry.Monitor, ping func(context.Context) error, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		probeCtx, end := monitor.Start(ctx, "dependency")
		probe, cancel := context.WithTimeout(probeCtx, 2*time.Second)
		err := ping(probe)
		cancel()
		if ctx.Err() != nil {
			end(false)
			return
		}
		end(err == nil)
		monitor.Incident(probeCtx, "queue", "database", err == nil)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
