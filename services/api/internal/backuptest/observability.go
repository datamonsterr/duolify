package backuptest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
)

// Enqueue inserts through the production middleware with the HTTP action context.
func (f *Fixture) Enqueue(ctx context.Context, date string) (int64, error) {
	result, err := f.queue.Insert(ctx, backup.Args{Date: date}, nil)
	if err != nil {
		return 0, err
	}
	return result.Job.ID, nil
}

// WaitDiscarded observes durable terminal state rather than an in-memory log.
func (f *Fixture) WaitDiscarded(t *testing.T, id int64) { t.Helper(); f.waitState(t, id, "discarded") }

// WaitCompleted preserves the job for independent backup/restore assertions.
func (f *Fixture) WaitCompleted(t *testing.T, id int64) { t.Helper(); f.waitState(t, id, "completed") }
func (f *Fixture) waitState(t *testing.T, id int64, state string) {
	t.Helper()
	deadline := time.NewTimer(40 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(25 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			job, err := f.queue.JobGet(t.Context(), id)
			if err != nil {
				t.Fatal(err)
			}
			if string(job.State) == state {
				return
			}
			if job.State == "discarded" {
				t.Fatal("job unexpectedly discarded")
			}
		case <-deadline.C:
			t.Fatal("job did not reach " + state)
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
}

// AssertCorrelationRestored verifies metadata without introducing product-state tables.
func (f *Fixture) AssertCorrelationRestored(t *testing.T, restored *store.Store, id int64, want telemetry.Correlation) {
	t.Helper()
	var raw []byte
	if err := restored.Pool.QueryRow(t.Context(), "SELECT metadata FROM river_job WHERE id=$1", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Correlation telemetry.Correlation `json:"correlation"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Correlation != want {
		t.Fatalf("correlation changed on restore: %+v want %+v", meta.Correlation, want)
	}
}
