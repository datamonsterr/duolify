package worker

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type retainerDouble struct {
	calls    int
	interval time.Duration
}

func (r *retainerDouble) PurgeExpiredRows(context.Context) (RowRetentionResult, error) {
	r.calls++
	return RowRetentionResult{PlaybackSyncEvents: 3}, nil
}
func (r *retainerDouble) SweepInterval() time.Duration { return r.interval }

func TestRowRetentionRegistersOneUniquePeriodicJob(t *testing.T) {
	r := &retainerDouble{interval: 6 * time.Hour}
	if jobs := registerRowRetentionWorker(river.NewWorkers(), r, nil); len(jobs) != 1 {
		t.Fatalf("want 1 periodic job, got %d", len(jobs))
	}
	if registerRowRetentionWorker(river.NewWorkers(), struct{}{}, nil) != nil {
		t.Fatal("service without a retainer must not register a job")
	}
	if (RowRetentionArgs{}).Kind() != "row_retention" {
		t.Fatal("unexpected kind")
	}
	if err := (&RowRetentionWorker{Service: r}).Work(t.Context(), nil); err != nil || r.calls != 1 {
		t.Fatalf("work: %v calls=%d", err, r.calls)
	}
}
