// Command api serves HTTP, runs River workers, or applies database migrations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/config"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:]); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, args []string) (result error) {
	if len(args) == 0 {
		return errors.New("usage: api {serve|worker|migrate}")
	}
	command := args[0]
	switch command {
	case "serve", "worker", "migrate":
	default:
		return fmt.Errorf("unknown subcommand %q", command)
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	if command == "migrate" {
		migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := migrations.Up(migrationCtx, cfg.DatabaseURL); err != nil {
			return fmt.Errorf("application migrations failed: %w", err)
		}
		db, err := store.New(migrationCtx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer db.Close()
		return worker.Migrate(migrationCtx, db.Pool, logger)
	}
	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if command == "worker" {
		return worker.Run(ctx, db.Pool, logger)
	}
	redis, err := cache.New(cfg.RedisURL)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, redis.Close()) }()
	backend, err := engine.New(cfg.EngineAddr, 10*time.Second)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, backend.Close()) }()
	events, err := analytics.New(cfg.PostHogKey, cfg.PostHogHost)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, events.Close()) }()
	handler := httpapi.New(httpapi.Dependencies{
		Postgres: db.Ping,
		Redis:    func(ctx context.Context) error { return redis.Ping(ctx).Err() },
		Engine:   backend,
	}, logger)
	server := httpapi.NewServer(cfg.Addr, handler)
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	logger.InfoContext(ctx, "HTTP server listening", "addr", listener.Addr().String())
	return httpapi.Serve(ctx, server, listener)
}
