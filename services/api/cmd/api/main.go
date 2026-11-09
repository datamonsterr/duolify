// Command api serves HTTP, runs River workers, or applies database migrations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/config"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
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
		return errors.New("usage: api {serve|worker|migrate|admin|healthcheck}")
	}
	command := args[0]
	if command == "admin" {
		return runAdminCLI(ctx, args[1:])
	}
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
	monitor := telemetry.New(logger)
	metricsAddr := os.Getenv("OPERATIONS_ADDR")
	if metricsAddr == "" {
		metricsAddr = "127.0.0.1:9091"
		if command == "worker" {
			metricsAddr = "127.0.0.1:9092"
		}
	}
	stopMetrics, err := monitor.Listen(ctx, metricsAddr)
	if err != nil {
		return err
	}
	defer stopMetrics()
	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := accounts.ApplyDeletionTombstones(ctx, db.Pool); err != nil {
		return fmt.Errorf("apply account deletion tombstones: %w", err)
	}
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
		service, err := backup.New(backup.Options{Source: backup.Postgres{Pool: db.Pool, DatabaseURL: cfg.DatabaseURL}, Objects: objects, Recipient: backupConfig.Recipient, Events: events, OptOut: backupConfig.OptOut, Monitor: monitor})
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
	secret := os.Getenv("JWT_SECRET")
	if len(secret) < 32 {
		return errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	var mailer accounts.Mailer = accounts.LogMailer{Logger: logger}
	if os.Getenv("DUOLIFY_ENV") == "production" {
		if strings.HasPrefix(secret, "change-me") {
			return errors.New("production JWT_SECRET must replace the development placeholder")
		}
		if os.Getenv("RESEND_API_KEY") == "" || os.Getenv("AUTH_MAIL_FROM") == "" {
			return errors.New("production auth requires RESEND_API_KEY and AUTH_MAIL_FROM")
		}
		mailer = accounts.ResendMailer{APIKey: os.Getenv("RESEND_API_KEY"), From: os.Getenv("AUTH_MAIL_FROM")}
	}
	profileOptions, err := accounts.ProfileOptionsFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	rateLimits, err := accounts.RateLimitsFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	profileOptions.Events = events
	auth, err := accounts.New(db.Pool, secret, mailer)
	if err != nil {
		return err
	}
	auth.ConfigureProfile(profileOptions)
	if err := pricing.BootstrapAdmins(ctx, db.Pool, pricing.OperatorPricingAdminIDs()); err != nil {
		return fmt.Errorf("pricing admin bootstrap failed: %w", err)
	}
	pricingAdmin := pricing.NewAdmin(db.Pool, events, os.Getenv("PRICING_OWNER_ID"))
	auth.ConfigureRateLimits(rateLimits)
	auth.ConfigureSetupChat(accounts.SetupChatOptions{
		Engine:  backend,
		Pricing: catalogue,
		Events:  events,
	})
	creditService := credits.NewService(db.Pool, redis)
	handler := httpapi.New(httpapi.Dependencies{
		Monitor:       monitor,
		ObjectStorage: telemetry.StorageProbe(os.Getenv),
		Accounts:      auth,
		Credits:       creditService,
		Pricing:       pricing.NewRuntimeCatalogue(catalogue, db.Queries, db.Pool, events),
		PricingAdmin:  pricingAdmin,
		Quotes:        pricing.NewQuotes(db.Pool),
		Postgres:      db.Ping,
		Redis:         func(ctx context.Context) error { return redis.Ping(ctx).Err() },
		Engine:        backend,
	}, logger)
	server := httpapi.NewServer(cfg.Addr, handler)
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	logger.InfoContext(ctx, "HTTP server listening", "addr", listener.Addr().String())
	return httpapi.Serve(ctx, server, listener)
}

func runAdminCLI(ctx context.Context, args []string) error {
	if len(args) < 2 || args[0] != "pricing" {
		return errors.New("usage: api admin pricing {draft|edit|validate|diff|submit|approve|reject|schedule|cancel|rollback|list|show} --actor UUID --reason TEXT [--version ID] [--new-version ID] [--effective-from RFC3339] [--config FILE]")
	}
	operation := args[1]
	flags := flag.NewFlagSet("admin pricing", flag.ContinueOnError)
	actor := flags.String("actor", "", "operator account UUID")
	reason := flags.String("reason", "", "required audit reason")
	version := flags.String("version", "", "pricing version")
	configFile := flags.String("config", "", "pricing config JSON file for draft/edit")
	newVersion := flags.String("new-version", "", "new version ID for a rollback draft")
	effectiveFrom := flags.String("effective-from", "", "rollback effective instant in RFC3339")
	if err := flags.Parse(args[2:]); err != nil {
		return err
	}
	if *actor == "" || strings.TrimSpace(*reason) == "" {
		return errors.New("--actor and --reason are required")
	}
	path := "/admin/v1/pricing/drafts"
	method := http.MethodPost
	var body []byte
	switch operation {
	case "draft":
		if *configFile == "" {
			return errors.New("--config is required for draft")
		}
		config, err := os.ReadFile(*configFile)
		if err != nil {
			return err
		}
		body, err = json.Marshal(map[string]any{"config": json.RawMessage(config), "reason": *reason})
		if err != nil {
			return err
		}
	case "list":
		method = http.MethodGet
		path += "?reason=" + url.QueryEscape(*reason)
	case "edit":
		if *version == "" || *configFile == "" {
			return errors.New("--version and --config are required for edit")
		}
		config, err := os.ReadFile(*configFile)
		if err != nil {
			return err
		}
		body, err = json.Marshal(map[string]any{"config": json.RawMessage(config), "reason": *reason})
		if err != nil {
			return err
		}
		method = http.MethodPatch
		path += "/" + url.PathEscape(*version)
	case "show":
		if *version == "" {
			return errors.New("--version is required for show")
		}
		method = http.MethodGet
		path += "/" + url.PathEscape(*version) + "?reason=" + url.QueryEscape(*reason)
	case "validate", "diff", "submit", "approve", "reject", "schedule", "cancel":
		if *version == "" {
			return errors.New("--version is required for this operation")
		}
		path += "/" + url.PathEscape(*version) + "/" + operation
		body, _ = json.Marshal(map[string]string{"reason": *reason})
	case "rollback":
		if *version == "" || *newVersion == "" || *effectiveFrom == "" {
			return errors.New("--version, --new-version and --effective-from are required for rollback")
		}
		instant, err := time.Parse(time.RFC3339Nano, *effectiveFrom)
		if err != nil {
			return errors.New("--effective-from must be RFC3339")
		}
		path += "/" + url.PathEscape(*version) + "/rollback"
		body, _ = json.Marshal(map[string]any{"version": *newVersion, "effective_from": instant, "reason": *reason})
	default:
		return fmt.Errorf("unknown pricing operation %q", operation)
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := store.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if err = pricing.BootstrapAdmins(ctx, db.Pool, pricing.OperatorPricingAdminIDs()); err != nil {
		return err
	}
	key := os.Getenv("POSTHOG_API_KEY")
	if strings.EqualFold(os.Getenv("ANALYTICS_OPT_OUT"), "true") {
		key = ""
	}
	events, err := analytics.New(key, os.Getenv("POSTHOG_HOST"))
	if err != nil {
		return err
	}
	defer func() { _ = events.Close() }()
	admin := pricing.NewAdmin(db.Pool, events, os.Getenv("PRICING_OWNER_ID"))
	status, response := admin.ExecuteCLI(ctx, *actor, method, path, body)
	_, _ = os.Stdout.Write(response)
	if status < 200 || status >= 300 {
		return fmt.Errorf("pricing %s failed with HTTP %d", operation, status)
	}
	return nil
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
