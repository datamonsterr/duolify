//go:build integration

package maintenance_test

import (
	"context"
	"log/slog"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/gamification"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/datamonsterr/duolify/services/api/internal/social"
	"github.com/datamonsterr/duolify/services/api/internal/steering"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Measured before the change: 1064 statements a minute for one worker plus one API replica
// (both ran every loop; the 200 ms pricing activator was the largest single source after
// the 1 s credit polls). Every extra API replica added about 520 more.
const maxIdleStatementsPerMinute = 150

// startWorker mirrors the background loops cmd/api starts in the worker process.
func startWorker(ctx context.Context, pool *pgxpool.Pool, wallet *credits.Service, logger *slog.Logger) {
	go wallet.Maintain(ctx, logger)
	go gamification.New(pool, nil).Maintain(ctx, logger)
	go social.New(pool, nil).Maintain(ctx, logger)
	go courses.New(courses.Options{Pool: pool, Financial: wallet}).Maintain(ctx, logger)
	chars := characters.NewService(pool, nil, nil)
	chars.ConfigureCreation(wallet, nil, "test")
	go chars.Maintain(ctx)
	shares := sharing.NewService(pool, nil, sharing.ConfigFromEnv(func(string) string { return "" }))
	go shares.Maintain(ctx)
	go shares.MaintainInvites(ctx)
	go steering.New(steering.Options{Pool: pool, Financial: wallet}).Maintain(ctx)
}

// startAPI mirrors the background work one API replica starts: only the pricing
// activator, which refreshes that process's in-memory copy. No Maintain loop.
func startAPI(ctx context.Context, pool *pgxpool.Pool, db *store.Store, catalogue *pricing.Catalogue, _ *credits.Service, _ *slog.Logger) {
	runtime := pricing.NewRuntimeCatalogue(catalogue, db.Queries, pool, nil)
	go runtime.RunActivator(ctx, pricing.NewAdmin(pool, nil, ""))
}
