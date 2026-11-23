package main

import (
	"context"
	"os"
	"strconv"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/datamonsterr/duolify/services/api/internal/steering"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// workerSweeps are the background sweeps that used to start in every API replica as well.
// The worker is their only owner, so each sweep runs once however many replicas serve HTTP.
// Credits, XP, social and course recovery are started next to their services in run().
type workerSweeps struct {
	Pool    *pgxpool.Pool
	Events  analytics.Events
	Credits *credits.Service
	Engine  *engine.Client
	Redis   *redis.Client
}

// start launches the character recovery and purge, the steering purge and the sharing
// retention, shared-asset and invite sweeps. The shared-asset sweep deletes objects with the
// application object-store key, which the worker container holds (compose app-env).
func (w workerSweeps) start(ctx context.Context) {
	allowUnapproved, _ := strconv.ParseBool(os.Getenv("ALLOW_UNAPPROVED_PRICING"))
	characterService := characters.NewService(w.Pool, nil, w.Events)
	characterService.ConfigureScreening(w.Engine)
	characterService.ConfigureCreation(w.Credits, w.Engine, os.Getenv("DUOLIFY_ENV"), allowUnapproved)
	go characterService.Maintain(ctx)

	steeringService := steering.New(steering.Options{Pool: w.Pool, Engine: w.Engine, Replanner: w.Engine, Financial: w.Credits, Events: w.Events})
	go steeringService.Maintain(ctx)

	sharingService := sharing.NewService(w.Pool, w.Events, sharing.ConfigFromEnv(os.Getenv)).WithMarketplaceCache(w.Redis).WithAssetStore(player.NewS3(os.Getenv))
	pushAdapter, _ := sharing.PushFromEnv(os.Getenv)
	sharingService.WithPush(pushAdapter)
	go sharingService.Maintain(ctx)
	go sharingService.MaintainInvites(ctx)
	go sharingService.MaintainAssets(ctx)
}
