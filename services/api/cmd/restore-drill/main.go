// Command restore-drill is the scheduled real-data restore drill. It restores
// the newest encrypted dump into a throwaway loopback Postgres, anonymizes it
// before any other access, then checks, measures RPO/RTO and reports. Without
// owner credentials it prints a notice and exits 0: absence of secrets is a
// skipped job, never a failure. It never writes to backup storage.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/gamification"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "restore drill failed:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := backup.LoadDrill(os.Getenv)
	if errors.Is(err, backup.ErrDrillDisabled) {
		fmt.Println("restore drill skipped: owner backup credentials are not configured")
		return nil
	}
	if err != nil {
		return err
	}
	store, err := backup.NewS3(cfg.S3)
	if err != nil {
		return err
	}
	redis, err := cache.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer func() { _ = redis.Close() }()
	versions, err := migrations.Versions()
	if err != nil {
		return err
	}
	key := os.Getenv("POSTHOG_API_KEY")
	if cfg.OptOut {
		key = ""
	}
	events, err := analytics.New(key, os.Getenv("POSTHOG_HOST"), func(context.Context, string) (bool, error) { return true, nil })
	if err != nil {
		return err
	}
	defer func() { _ = events.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), backup.RTOBudget)
	defer cancel()
	report, err := backup.RunDrill(ctx, backup.DrillOptions{
		Store: store, Identity: cfg.Identity, Source: "scheduled", Events: events, OptOut: cfg.OptOut, Now: time.Now,
		Restore:            backup.DockerRestorer{Container: cfg.Container, AdminURL: cfg.DatabaseURL, User: cfg.PGUser}.Restore,
		ExpectedMigrations: versions,
		ReplayDeletions: func(ctx context.Context, pool *pgxpool.Pool) error {
			return accounts.ReplayDeletionLedger(ctx, pool, backup.ReadOnlyLedger{Objects: store})
		},
		RebuildRedis: func(ctx context.Context, pool *pgxpool.Pool) (int, error) {
			service := gamification.New(pool, nil)
			service.UseCache(redis)
			return service.RebuildLeagueCache(ctx)
		},
		// FinancialReplay: E7-11 supplies the quarterly business checks here.
	})
	// Only check names and measurements are printed: restored rows are private.
	fmt.Printf("dump_date_rpo=%s rto=%s redis_groups=%d financial_replay=%s\n", report.RPO, report.RTO.Round(time.Millisecond), report.RedisRebuilt, report.FinancialReplay)
	for _, check := range report.Checks {
		fmt.Printf("check %-40s %t\n", check.Name, check.OK)
	}
	return err
}
