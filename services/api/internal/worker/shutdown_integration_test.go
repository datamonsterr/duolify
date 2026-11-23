//go:build integration

package worker_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type sleepArgs struct{}

func (sleepArgs) Kind() string { return "drain_test_sleep" }

type sleepWorker struct {
	river.WorkerDefaults[sleepArgs]
	started chan struct{}
	finish  chan struct{}
	ctxErr  chan error
}

func (w *sleepWorker) Work(ctx context.Context, _ *river.Job[sleepArgs]) error {
	w.started <- struct{}{}
	select {
	case <-w.finish:
		return nil
	case <-ctx.Done():
		w.ctxErr <- ctx.Err()
		return ctx.Err()
	}
}

func newQueue(t *testing.T) (*river.Client[pgx.Tx], *sleepWorker, *store.Store) {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if err = worker.Migrate(ctx, db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	w := &sleepWorker{started: make(chan struct{}, 4), finish: make(chan struct{}), ctxErr: make(chan error, 4)}
	workers := river.NewWorkers()
	river.AddWorker(workers, w)
	client, err := river.NewClient(riverpgxv5.New(db.Pool), &river.Config{
		Logger: logger, Workers: workers, Queues: map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
		MaxAttempts: 3, RetryPolicy: &river.DefaultClientRetryPolicy{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	return client, w, db
}

func jobState(t *testing.T, db *store.Store, id int64) string {
	t.Helper()
	var state string
	if err := db.Pool.QueryRow(t.Context(), `SELECT state::text FROM river_job WHERE id=$1`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

// A running job that finishes inside the drain window completes; nothing is cancelled.
func TestStopQueueLetsRunningJobFinish(t *testing.T) {
	client, w, db := newQueue(t)
	res, err := client.Insert(t.Context(), sleepArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-w.started
	go func() { time.Sleep(300 * time.Millisecond); close(w.finish) }()
	if err = worker.StopQueue(client, 10*time.Second); err != nil {
		t.Fatalf("soft stop: %v", err)
	}
	if got := jobState(t, db, res.Job.ID); got != "completed" {
		t.Fatalf("job state %q, want completed", got)
	}
}

// A job still running when the drain ends is cancelled and returned to the queue for retry, never left "running".
func TestStopQueueHardStopRequeuesJob(t *testing.T) {
	client, w, db := newQueue(t)
	res, err := client.Insert(t.Context(), sleepArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-w.started
	_ = worker.StopQueue(client, 300*time.Millisecond)
	select {
	case err := <-w.ctxErr:
		if err == nil {
			t.Fatal("job saw no context error")
		}
	default:
		t.Fatal("job context was not cancelled after the drain timeout")
	}
	if got := jobState(t, db, res.Job.ID); got != "retryable" && got != "available" {
		t.Fatalf("job state %q, want retryable (picked up again by the next worker)", got)
	}
}
