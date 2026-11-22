package worker

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

const (
	// ReliabilityAlertInterval is how often alert rules are evaluated. Alert
	// dedupe windows are an hour or a day, so five minutes bounds detection delay.
	ReliabilityAlertInterval = 5 * time.Minute
	// ReliabilityAlertPeriodicID is the stable River periodic job ID.
	ReliabilityAlertPeriodicID = "reliability-alerts"
)

// ReliabilityAlerter evaluates reliability and AI cost alert rules.
type ReliabilityAlerter interface {
	RunScheduledAlerts(context.Context) error
}

// ReliabilityAlertArgs is a content-free scheduled alert evaluation.
type ReliabilityAlertArgs struct{}

// Kind identifies the persisted alert evaluation job.
func (ReliabilityAlertArgs) Kind() string { return "reliability_alerts" }

// ReliabilityAlertWorker runs one alert evaluation pass.
type ReliabilityAlertWorker struct {
	river.WorkerDefaults[ReliabilityAlertArgs]
	Service ReliabilityAlerter
}

// Work evaluates alerts; Postgres dedupe keeps concurrent workers from double-firing.
func (w *ReliabilityAlertWorker) Work(ctx context.Context, _ *river.Job[ReliabilityAlertArgs]) error {
	return w.Service.RunScheduledAlerts(ctx)
}

func registerReliabilityAlertWorkers(workers *river.Workers, alerts ReliabilityAlerter) []*river.PeriodicJob {
	if alerts == nil {
		return nil
	}
	river.AddWorker(workers, &ReliabilityAlertWorker{Service: alerts})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(ReliabilityAlertInterval), func() (river.JobArgs, *river.InsertOpts) {
			// ByPeriod collapses duplicate inserts from overlapping leaders into one job per interval.
			return ReliabilityAlertArgs{}, &river.InsertOpts{MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: ReliabilityAlertInterval}}
		}, &river.PeriodicJobOpts{ID: ReliabilityAlertPeriodicID, RunOnStart: true}),
	}
}
