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
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
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
	var financialProvider credits.FinancialProvider
	if strings.EqualFold(os.Getenv("DRILL_FINANCIAL_REPLAY"), "true") {
		raw, signature, key := os.Getenv("BACKUP_FINANCIAL_FIXTURES"), os.Getenv("BACKUP_FINANCIAL_FIXTURE_SIGNATURE"), os.Getenv("BACKUP_FINANCIAL_FIXTURE_KEY")
		if raw == "" || signature == "" || key == "" {
			fmt.Println("financial replay skipped: owner-supplied signed TEST fixtures are not configured")
			return nil
		}
		provider, err := credits.LoadSignedFinancialFixtures([]byte(raw), signature, key)
		if err != nil {
			return errors.New("financial replay fixture verification failed")
		}
		financialProvider = provider
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
	var financialReplay backup.FinancialReplay
	if financialProvider != nil {
		financialReplay = credits.NewFinancialReplay(financialProvider, events, time.Now)
	}
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
		FinancialReplay: financialReplay,
	})
	// Only check names and measurements are printed: restored rows are private.
	fmt.Printf("dump_date_rpo=%s rto=%s redis_groups=%d financial_replay=%s\n", report.RPO, report.RTO.Round(time.Millisecond), report.RedisRebuilt, report.FinancialReplay)
	for _, check := range report.Checks {
		fmt.Printf("check %-40s %t\n", check.Name, check.OK)
	}
	return err
}
