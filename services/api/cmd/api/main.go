// Command api serves HTTP, runs River workers, or applies database migrations.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/config"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
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
		return errors.New("usage: api {serve|worker|migrate|healthcheck}")
	}
	command := args[0]
	if command == "healthcheck" {
		return healthcheck(ctx, args[1:])
	}
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
		backupConfig, err := backup.Load(os.Getenv)
		if err != nil {
			return err
		}
		objects, err := backup.NewS3(backupConfig.S3)
		if err != nil {
			return err
		}
		key := cfg.PostHogKey
		if backupConfig.OptOut {
			key = ""
		}
		events, err := analytics.New(key, cfg.PostHogHost)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, events.Close()) }()
		service, err := backup.New(backup.Options{Source: backup.Postgres{Pool: db.Pool, DatabaseURL: cfg.DatabaseURL}, Objects: objects, Recipient: backupConfig.Recipient, Events: events, OptOut: backupConfig.OptOut})
		if err != nil {
			return err
		}
		return worker.Run(ctx, db.Pool, logger, service)
	}
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		return fmt.Errorf("pricing boot validation: %w", err)
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
	analyticsKey := cfg.PostHogKey
	if cfg.AnalyticsOptOut {
		analyticsKey = ""
	}
	events, err := analytics.New(analyticsKey, cfg.PostHogHost)
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, events.Close()) }()
	handler := httpapi.New(httpapi.Dependencies{
		Pricing:  catalogue,
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

// healthcheck works in distroless images without a shell or curl. Workers and
// one-shot migrations use --database because they do not expose an HTTP server.
func healthcheck(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	database := flags.Bool("database", false, "check the queue's Postgres dependency")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected healthcheck arguments")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if *database {
		databaseURL := os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			return errors.New("DATABASE_URL is required")
		}
		db, err := store.New(ctx, databaseURL)
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Ping(ctx)
	}
	addr := os.Getenv("API_ADDR")
	if addr == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		addr = ":" + port
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("healthcheck port must be between 1 and 65535")
	}
	// #nosec G704 -- fixed loopback host and numeric port; no user-controlled URL.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/healthz", nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	// #nosec G704 -- request is fixed to loopback above and redirects are disabled.
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned HTTP %d", response.StatusCode)
	}
	return nil
}
