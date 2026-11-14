package worker

import (
	"context"
	"time"

	"github.com/riverqueue/river"
)

// FinancialMaintainer keeps provider reconciliation separate from data expiry.
type FinancialMaintainer interface {
	ReconcileFinancialOperations(context.Context) error
	PurgeFinancialDiagnostics(context.Context) error
}

// FinancialReconciliationArgs is a content-free daily canonical reconciliation.
type FinancialReconciliationArgs struct{}

// Kind identifies the persisted reconciliation job.
func (FinancialReconciliationArgs) Kind() string { return "financial_reconciliation" }

// FinancialRetentionArgs expires diagnostic payloads and derived analytics only.
type FinancialRetentionArgs struct{}

// Kind identifies the independent retention job.
func (FinancialRetentionArgs) Kind() string { return "financial_diagnostic_retention" }

// FinancialReconciliationWorker repairs verified missing operations by business key.
type FinancialReconciliationWorker struct {
	river.WorkerDefaults[FinancialReconciliationArgs]
	Service FinancialMaintainer
}

// Work executes one canonical scan with River's bounded job retry policy.
func (w *FinancialReconciliationWorker) Work(ctx context.Context, _ *river.Job[FinancialReconciliationArgs]) error {
	return w.Service.ReconcileFinancialOperations(ctx)
}

// FinancialRetentionWorker never owns ledger, catalogue or snapshot deletion.
type FinancialRetentionWorker struct {
	river.WorkerDefaults[FinancialRetentionArgs]
	Service FinancialMaintainer
}

// Work runs the independent short-lived data purge.
func (w *FinancialRetentionWorker) Work(ctx context.Context, _ *river.Job[FinancialRetentionArgs]) error {
	return w.Service.PurgeFinancialDiagnostics(ctx)
}

func registerFinancialWorkers(workers *river.Workers, service any) []*river.PeriodicJob {
	s, ok := service.(FinancialMaintainer)
	if !ok {
		return nil
	}
	river.AddWorker(workers, &FinancialReconciliationWorker{Service: s})
	river.AddWorker(workers, &FinancialRetentionWorker{Service: s})
	opts := func() *river.InsertOpts { return &river.InsertOpts{MaxAttempts: 5} }
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour), func() (river.JobArgs, *river.InsertOpts) { return FinancialReconciliationArgs{}, opts() }, &river.PeriodicJobOpts{ID: "financial-reconciliation-daily", RunOnStart: true}),
		river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour), func() (river.JobArgs, *river.InsertOpts) { return FinancialRetentionArgs{}, opts() }, &river.PeriodicJobOpts{ID: "financial-diagnostic-retention-daily", RunOnStart: true}),
	}
}
