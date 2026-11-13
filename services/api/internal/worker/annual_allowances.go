package worker

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

// AnnualIssuer resolves due grants from durable funded-term history.
type AnnualIssuer interface {
	IssueAnnualAllowances(context.Context, time.Time) (int, error)
}

// AnnualAllowanceArgs contains no learner data; Postgres owns the due schedule.
type AnnualAllowanceArgs struct{}

// Kind is the stable on-disk River kind.
func (AnnualAllowanceArgs) Kind() string { return "annual_subscription_allowances" }

// AnnualAllowanceWorker sweeps due funded months, including outages before start.
type AnnualAllowanceWorker struct {
	river.WorkerDefaults[AnnualAllowanceArgs]
	Issuer AnnualIssuer
}

// Work commits one bounded batch. A full batch snoozes to drain the backlog.
func (w *AnnualAllowanceWorker) Work(ctx context.Context, _ *river.Job[AnnualAllowanceArgs]) error {
	count, err := w.Issuer.IssueAnnualAllowances(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	if count == 100 {
		return river.JobSnooze(time.Second)
	}
	return nil
}
