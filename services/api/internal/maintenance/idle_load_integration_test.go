//go:build integration

package maintenance_test

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// counter counts every statement a pool sends, including BEGIN and COMMIT.
type counter struct{ n atomic.Int64 }

func (c *counter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}
func (*counter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// idleSeconds is how long the processes idle; the PR numbers use 60.
func idleSeconds() time.Duration {
	if v, err := strconv.Atoi(os.Getenv("IDLE_SECONDS")); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return 20 * time.Second
}

// TestIdleStatementsPerMinute runs the background loops of one worker and one API process
// against a migrated, empty database and counts the statements they send.
func TestIdleStatementsPerMinute(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("idle"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	url, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	count := &counter{}
	cfg.ConnConfig.Tracer = count
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	run, stop := context.WithCancel(ctx)
	defer stop()
	logger := slog.New(slog.DiscardHandler)
	wallet := credits.NewServiceWithCatalogue(pool, nil, catalogue, nil)
	startWorker(run, pool, wallet, logger)
	startAPI(run, pool, db, catalogue, wallet, logger)

	time.Sleep(2 * time.Second) // startup passes are not idle load
	before := count.n.Load()
	window := idleSeconds()
	time.Sleep(window)
	statements := count.n.Load() - before
	stop()
	perMinute := float64(statements) * float64(time.Minute) / float64(window)
	t.Logf("IDLE_STATEMENTS window=%s statements=%d per_minute=%.0f", window, statements, perMinute)
	if perMinute > maxIdleStatementsPerMinute {
		t.Fatalf("idle background load %.0f statements/minute exceeds %d", perMinute, maxIdleStatementsPerMinute)
	}
}
