//go:build integration

package store_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	dbsql "github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMigrationsStoreAndQueue(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("duolify"), postgres.WithUsername("duolify"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrations.Up(ctx, url); err != nil {
			t.Fatal(err)
		}
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	learner, err := db.Queries.CreateLearner(ctx, "Learner")
	if err != nil {
		t.Fatal(err)
	}
	got, err := db.Queries.GetLearner(ctx, learner.ID)
	if err != nil || got.DisplayName != "Learner" {
		t.Fatalf("learner=%+v err=%v", got, err)
	}
	for _, kind := range []string{"purchase", "reward", "spend", "reserve", "release", "refund"} {
		if _, err := db.Queries.CreateCreditEntry(ctx, dbsql.CreateCreditEntryParams{LearnerID: learner.ID, Amount: 10, Kind: kind, Reference: kind}); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []string{
		"UPDATE credit_entries SET amount = 20",
		"DELETE FROM credit_entries",
		"TRUNCATE credit_entries",
		"INSERT INTO credit_entries (learner_id,amount,kind,reference) SELECT id,1,'invalid','x' FROM learners",
		"INSERT INTO credit_entries (learner_id,amount,kind,reference) SELECT id,0,'purchase','x' FROM learners",
	} {
		if _, err := db.Pool.Exec(ctx, query); err == nil {
			t.Errorf("accepted invalid ledger mutation: %s", query)
		}
	}
	if _, err := db.Queries.CreateCreditEntry(ctx, dbsql.CreateCreditEntryParams{LearnerID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, Amount: 1, Kind: "reward", Reference: "orphan"}); err == nil {
		t.Error("orphan entry accepted")
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	if err := worker.Migrate(ctx, db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	queue, err := worker.New(db.Pool, logger)
	if err != nil {
		t.Fatal(err)
	}
	events, unsubscribe := queue.Subscribe(river.EventKindJobCompleted)
	defer unsubscribe()
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := queue.Stop(stopCtx); err != nil {
			t.Error(err)
		}
	}()
	inserted, err := queue.Insert(ctx, worker.DummyArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-events:
		if event.Job.ID != inserted.Job.ID {
			t.Fatal("unexpected completed job")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
