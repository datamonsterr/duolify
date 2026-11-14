package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
)

type financialMaintenanceDouble struct{ reconciliations, purges int }

func (f *financialMaintenanceDouble) ReconcileFinancialOperations(context.Context) error {
	f.reconciliations++
	return errors.New("fixture provider unavailable")
}
func (f *financialMaintenanceDouble) PurgeFinancialDiagnostics(context.Context) error {
	f.purges++
	return nil
}

func TestFinancialRetentionRunsIndependentlyOfProviderReconciliation(t *testing.T) {
	f := new(financialMaintenanceDouble)
	if jobs := registerFinancialWorkers(river.NewWorkers(), f); len(jobs) != 2 {
		t.Fatal("daily financial jobs not registered separately")
	}
	if err := (&FinancialReconciliationWorker{Service: f}).Work(t.Context(), nil); err == nil {
		t.Fatal("failed provider reconciliation succeeded")
	}
	if err := (&FinancialRetentionWorker{Service: f}).Work(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if f.reconciliations != 1 || f.purges != 1 {
		t.Fatal("provider failure blocked independent retention")
	}
}
