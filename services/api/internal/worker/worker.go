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

// ExpireDailyCreditsArgs is the job args for River to expire unreserved daily credits.
type ExpireDailyCreditsArgs struct{}

// Kind identifies the job on disk.
func (ExpireDailyCreditsArgs) Kind() string { return "expire_daily_credits" }

// Expirer abstracts daily credits expiration for worker processing.
type Expirer interface {
	ExpireDailyCredits(ctx context.Context, asOf time.Time) (int, error)
}

// CreditsWorker processes daily credit expiry jobs.
type CreditsWorker struct {
	river.WorkerDefaults[ExpireDailyCreditsArgs]
	Expirer Expirer
	Logger  *slog.Logger
}

// Work processes an expire daily credits job by invoking the expirer.
func (w *CreditsWorker) Work(ctx context.Context, _ *river.Job[ExpireDailyCreditsArgs]) error {
	count, err := w.Expirer.ExpireDailyCredits(ctx, time.Now().UTC())
	if err != nil {
		w.Logger.ErrorContext(ctx, "daily credits expiry job failed", "error", err)
		return err
	}
	if count > 0 {
		w.Logger.InfoContext(ctx, "daily credits expired", "count", count)
	}
	return nil
}

// PeriodicCredits schedules periodic daily credit expiry.
type PeriodicCredits struct{}

// Next schedules the next interval for checking unexpired past daily credits.
func (PeriodicCredits) Next(current time.Time) time.Time {
	return current.Add(15 * time.Minute)
}

// PeriodicCreditsJob returns a River periodic job for expiring daily credits.
func PeriodicCreditsJob() *river.PeriodicJob {
	return river.NewPeriodicJob(PeriodicCredits{}, func() (river.JobArgs, *river.InsertOpts) {
		return ExpireDailyCreditsArgs{}, nil
	}, &river.PeriodicJobOpts{ID: "periodic-daily-credits-expiry", RunOnStart: true})
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
	return NewWithCredits(pool, logger, monitor, nil, backups...)
}

// NewWithCredits registers queue workers for backups and daily credit expiry.
func NewWithCredits(pool *pgxpool.Pool, logger *slog.Logger, monitor *telemetry.Monitor, expirer Expirer, backups ...*backup.Service) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &dummyWorker{logger: logger})
	periodic := []*river.PeriodicJob{}
	if expirer != nil {
		river.AddWorker(workers, &CreditsWorker{Expirer: expirer, Logger: logger})
		periodic = append(periodic, PeriodicCreditsJob())
	}
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
