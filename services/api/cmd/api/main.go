// Command api serves HTTP, runs River workers, or applies database migrations.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	// Embedded zone database: browsers report legacy aliases (Asia/Saigon) that the images'
	// system tzdata may lack, and credits.Reserve fails closed on an unknown zone.
	_ "time/tzdata"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/chat"
	"github.com/datamonsterr/duolify/services/api/internal/config"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/gamification"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/retention"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
	"github.com/datamonsterr/duolify/services/api/internal/social"
	"github.com/datamonsterr/duolify/services/api/internal/steering"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/datamonsterr/duolify/services/api/internal/voice"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/riverqueue/river"
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

	db, err := store.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	backupConfig, err := backup.Load(os.Getenv)
	if err != nil {
		return err
	}
	backupConfig.S3.Logger = logger
	objects, err := backup.NewS3(backupConfig.S3)
	if err != nil {
		return err
	}
	deletionLedger := backup.DeletionLedger{Objects: objects, ClerkKey: os.Getenv("JWT_SECRET")}
	if err := accounts.ReplayDeletionLedger(ctx, db.Pool, deletionLedger); err != nil {
		return fmt.Errorf("apply account deletion tombstones: %w", err)
	}
	if err := backup.ReplayCharacterSuppressions(ctx, db.Pool, objects); err != nil {
		return fmt.Errorf("apply Character suppression tombstones: %w", err)
	}
	stopMetrics, err := monitor.Listen(ctx, metricsAddr)
	if err != nil {
		return err
	}
	defer stopMetrics()
	if command == "worker" {
		key := cfg.PostHogKey
		if backupConfig.OptOut {
			key = ""
		}
		events, err := analytics.New(key, cfg.PostHogHost, analytics.DatabaseConsent(db.Pool))
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, events.Close()) }()
		assetProtection := backup.CharacterAssetProtection{Pool: db.Pool, Application: &backup.S3{Client: objects.Client, Bucket: backupConfig.S3.ApplicationBucket, Logger: logger}, Protected: objects, Recipient: backupConfig.Recipient, Unversioned: !backupConfig.Versioned}
		service, err := backup.New(backup.Options{Source: backup.Postgres{Pool: db.Pool, DatabaseURL: cfg.DatabaseURL}, Objects: objects, Recipient: backupConfig.Recipient, Events: events, OptOut: backupConfig.OptOut, Monitor: monitor, FinancialCopy: true, ProtectAssets: assetProtection.Protect})
		if err != nil {
			return err
		}
		catalogue, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			return err
		}
		creditService := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, events)
		creditService.UseOperationalMonitor(monitor)
		if err := creditService.ConfigurePaymentEnvironment(os.Getenv); err != nil {
			return err
		}
		go creditService.Maintain(ctx, logger)
		xpService := gamification.New(db.Pool, events)
		xpService.UseEntitlements(creditService)
		workerSocial := social.New(db.Pool, events)
		xpService.OnSettled(friendStreakSettler(workerSocial, logger))
		go xpService.Maintain(ctx, logger)
		go workerSocial.Maintain(ctx, logger)
		backend, err := engine.New(cfg.EngineAddr, 10*time.Second)
		if err != nil {
			return err
		}
		defer func() { _ = backend.Close() }()
		// Error-level signal for quoted actions the engine would refuse (route mismatch).
		go pricing.CheckActiveRoutes(ctx, db.Pool, logger, backend.Providers)
		outlineService := courses.New(courses.Options{Pool: db.Pool, OutlineEngine: backend, Financial: creditService, Events: events})
		go outlineService.Maintain(ctx, logger)
		redis, err := cache.New(cfg.RedisURL)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, redis.Close()) }()
		workerSweeps{Pool: db.Pool, Events: events, Credits: creditService, Engine: backend, Redis: redis}.start(ctx)
		outlineQueue, err := outlineService.CreationQueue(logger)
		if err != nil {
			return err
		}
		if err = outlineQueue.Start(context.WithoutCancel(ctx)); err != nil {
			return err
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = outlineQueue.Stop(stopCtx)
		}()
		notesService := notes.NewServiceWithEnrichment(db.Pool, events, creditService, backend.RPC())
		creditService.SetCreditArrivalHook(func(ctx context.Context, learnerID string) {
			_, _ = notesService.ResumePendingEnrichments(ctx, learnerID)
		})
		notesQueue, err := notesService.NotesQueue(logger)
		if err != nil {
			return err
		}
		if err = notesQueue.Start(context.WithoutCancel(ctx)); err != nil {
			return err
		}
		defer func() {
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			_ = notesQueue.Stop(stopCtx)
		}()
		if cfg.LessonTestTracerEnabled {
			lessonService := lessontest.New(lessontest.Options{Pool: db.Pool, Engine: backend, Financial: creditService, Events: events, Casts: characters.NewService(db.Pool, nil, events), Objects: player.NewS3(os.Getenv), AssetOrigin: os.Getenv("LESSON_ASSET_ORIGIN")})
			lessonQueue, err := lessonService.LessonQueue(logger)
			if err != nil {
				return err
			}
			if err = lessonQueue.Start(context.WithoutCancel(ctx)); err != nil {
				return err
			}
			defer func() {
				stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				_ = lessonQueue.Stop(stopCtx)
			}()
			legacyPlayer := player.New(player.Options{Pool: db.Pool, Objects: player.NewS3(os.Getenv), AssetOrigin: os.Getenv("LESSON_ASSET_ORIGIN")})
			backfillQueue, err := legacyPlayer.GeneratedBackfillQueue(logger)
			if err != nil {
				return err
			}
			if err = backfillQueue.Start(context.WithoutCancel(ctx)); err != nil {
				return err
			}
			defer func() {
				stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				_ = backfillQueue.Stop(stopCtx)
			}()
			if _, err = backfillQueue.Insert(ctx, player.GeneratedBackfillArgs{}, &river.InsertOpts{Queue: "generated-publication", MaxAttempts: 10, UniqueOpts: river.UniqueOpts{ByArgs: true}}); err != nil {
				return err
			}
		}
		// E14-01: narration synthesis stays off until explicitly enabled; only the engine mock exists.
		if os.Getenv("NARRATION_ENABLED") == "true" {
			narrationService := narration.New(narration.Options{Pool: db.Pool, Objects: player.NewS3(os.Getenv), Speech: backend.RPC(), Financial: creditService, Entitlements: creditService, Events: events, Logger: logger, Retention: narrationRetention(), Segments: characters.NewService(db.Pool, nil, events), Budget: narrationBudget(os.Getenv)})
			narrationQueue, err := narrationService.Queue()
			if err != nil {
				return err
			}
			if err = narrationQueue.Start(context.WithoutCancel(ctx)); err != nil {
				return err
			}
			// Operator queue and retention: regenerate missing objects and delete expired assets.
			go narrationService.RunMaintenance(ctx, time.Hour)
			defer func() {
				stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
				defer cancel()
				_ = narrationQueue.Stop(stopCtx)
			}()
		}
		courseService := courses.New(courses.Options{Pool: db.Pool})
		clerkDeleter, err := accounts.ClerkUserDeleterFromEnv(os.Getenv)
		if err != nil {
			return err
		}
		clerkEraser := accounts.NewClerkUserEraser(db.Pool, clerkDeleter)
		return worker.RunWithAlerts(ctx, db.Pool, logger, workerServices{creditService, courseService, clerkEraser, accounts.NewPostHogPersonEraser(db.Pool, accounts.PostHogPersonDeleterFromEnv(os.Getenv)), retention.New(db.Pool, retention.ConfigFromEnv(os.Getenv, cfg.LessonTestSessionTTL), logger)}, eval.NewService(db.Pool, catalogue, monitor, events), service)
	}
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		return fmt.Errorf("pricing boot validation: %w", err)
	}
	allowUnapprovedPricing, _ := strconv.ParseBool(os.Getenv("ALLOW_UNAPPROVED_PRICING"))
	if err := characters.ValidateStartupPricing(os.Getenv("DUOLIFY_ENV"), allowUnapprovedPricing, catalogue.HasAction("character_create")); err != nil {
		return fmt.Errorf("characters pricing validation: %w", err)
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
	// Error-level signal for quoted actions the engine would refuse (route mismatch).
	go pricing.CheckActiveRoutes(ctx, db.Pool, logger, backend.Providers)
	analyticsKey := cfg.PostHogKey
	if cfg.AnalyticsOptOut {
		analyticsKey = ""
	}
	events, err := analytics.New(analyticsKey, cfg.PostHogHost, analytics.DatabaseConsent(db.Pool))
	if err != nil {
		return err
	}
	defer func() { result = errors.Join(result, events.Close()) }()
	secret, mailer, err := accounts.AuthFromEnv(os.Getenv, logger)
	if err != nil {
		return err
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
	clerkVerifier, clerkPublishableKey, err := accounts.ClerkFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	auth.ConfigureClerk(clerkVerifier, clerkPublishableKey)
	auth.ConfigureWebOrigins(accounts.WebOriginsFromEnv(os.Getenv))
	auth.ConfigureProfile(profileOptions)
	auth.ConfigureDeletionLedger(deletionLedger)
	if err := pricing.BootstrapAdmins(ctx, db.Pool, pricing.OperatorPricingAdminIDs()); err != nil {
		return fmt.Errorf("pricing admin bootstrap failed: %w", err)
	}
	pricingAdmin := pricing.NewAdmin(db.Pool, events, os.Getenv("PRICING_OWNER_ID"))
	pricingAdmin.SetEnvironment(os.Getenv("DUOLIFY_ENV"))
	if err := pricingAdmin.ConfigureVerification(os.Getenv); err != nil {
		return fmt.Errorf("pricing verifier configuration: %w", err)
	}
	runtimePricing := pricing.NewRuntimeCatalogue(catalogue, db.Queries, db.Pool, events)
	runtimePricing.SetEnvironment(os.Getenv("DUOLIFY_ENV"))
	runtimePricing.SetOwnerID(os.Getenv("PRICING_OWNER_ID"))
	evalService := eval.NewService(db.Pool, catalogue, monitor, events)
	runtimePricing.SetAlertSink(evalService.RaiseOperationalAlert)
	if err := runtimePricing.ActivateDue(ctx, pricingAdmin); err != nil {
		return fmt.Errorf("pricing activation: %w", err)
	}
	activatorCtx, stopActivator := context.WithCancel(ctx)
	defer stopActivator()
	go runtimePricing.RunActivator(activatorCtx, pricingAdmin)

	auth.ConfigureRateLimits(rateLimits)
	auth.ConfigureSetupChat(accounts.SetupChatOptions{
		Engine:  backend,
		Pricing: runtimePricing,
		Events:  events,
	})
	creditService := credits.NewServiceWithCatalogue(db.Pool, redis, catalogue, events)
	creditService.UseOperationalMonitor(monitor)
	if err := creditService.ConfigurePaymentEnvironment(os.Getenv); err != nil {
		return err
	}
	// No Maintain loop starts in this process: the worker owns every background sweep
	// (credits, XP, streaks, friend streaks, characters, sharing, steering, course recovery),
	// so replicas do not repeat them. The only background work left here is the pricing
	// activator (in-memory copy) and the shared-asset sweep below.
	xpService := gamification.New(db.Pool, events)
	xpService.UseCache(redis)
	xpService.UseEntitlements(creditService)
	socialService := social.New(db.Pool, events)
	xpService.OnSettled(friendStreakSettler(socialService, logger))
	characterService := characters.NewService(db.Pool, nil, events)
	characterService.ConfigureScreening(backend)
	characterService.ConfigureCreation(creditService, backend, os.Getenv("DUOLIFY_ENV"), allowUnapprovedPricing)
	notesService := notes.NewServiceWithEnrichment(db.Pool, events, creditService, backend.RPC())
	creditService.SetCreditArrivalHook(func(ctx context.Context, learnerID string) {
		_, _ = notesService.ResumePendingEnrichments(ctx, learnerID)
	})

	var lessonHandler http.Handler
	var lessonPreparer *lessontest.Service
	if cfg.LessonTestTracerEnabled {
		lessonPreparer = lessontest.New(lessontest.Options{
			Objects:             player.NewS3(os.Getenv),
			AssetOrigin:         os.Getenv("LESSON_ASSET_ORIGIN"),
			Pool:                db.Pool,
			Engine:              backend,
			Financial:           creditService,
			Events:              events,
			Casts:               characters.NewService(db.Pool, nil, events),
			MinGap:              cfg.LessonTestMinGap,
			MaxVariantsPerTopic: cfg.LessonTestMaxVariantsPerTopic,
			MaxVariantsPerTest:  cfg.LessonTestMaxVariantsPerTest,
			SessionTTL:          cfg.LessonTestSessionTTL,
		})
		lessonHandler = lessonPreparer
	}

	var narrationRecovery player.NarrationRecovery
	var narrationVoices http.Handler
	if os.Getenv("NARRATION_ENABLED") == "true" {
		// Insert-only queue client: the worker process runs the jobs.
		recovery := narration.New(narration.Options{Pool: db.Pool, Objects: player.NewS3(os.Getenv), Speech: backend.RPC(), Financial: creditService, Entitlements: creditService, Events: events, Logger: logger, Retention: narrationRetention(), Segments: characters.NewService(db.Pool, nil, events), Budget: narrationBudget(os.Getenv)})
		if _, err = recovery.Queue(); err != nil {
			return err
		}
		narrationRecovery = recovery
		narrationVoices = recovery.HTTP()
	}
	quotesService := pricing.NewQuotes(db.Pool).WithEvents(events)
	quotesService.SetEnvironment(os.Getenv("DUOLIFY_ENV"))
	quotesService.SetOwnerID(os.Getenv("PRICING_OWNER_ID"))

	courseService := courses.New(courses.Options{Pool: db.Pool, Objects: player.NewS3(os.Getenv), Engine: backend, ProposalEngine: backend, OutlineEngine: backend, Financial: creditService, Pricing: runtimePricing, Events: events, Environment: os.Getenv("DUOLIFY_ENV"), LiveApproved: os.Getenv("COURSE_BRIEF_CHAT_LIVE_APPROVED") == "true"})
	// Publishing needs an 18+ account (ADR 0012); no age data exists yet, so it fails closed.
	lessonObjects := player.NewS3(os.Getenv)
	sharingService := sharing.NewService(db.Pool, events, sharing.ConfigFromEnv(os.Getenv)).WithObjects(lessonObjects, os.Getenv("LESSON_ASSET_ORIGIN")).
		WithAssetStore(lessonObjects).WithBlockLedger(backup.ModerationLedger{Objects: objects})
	sharingService.ConfigureScreening(backend, sharing.PricingRates{Pool: db.Pool})
	// Marketplace pages are cached in Redis only as derived data; the flag MARKETPLACE_ENABLED stays off by default (E15-07).
	sharingService = sharingService.WithMarketplaceCache(redis)
	if lessonPreparer != nil {
		// Missing Lessons in a pinned copy reuse the private generation pipeline and the learner's credits (E15-04).
		sharingService = sharingService.WithGeneration(sharing.GenerationDeps{Quotes: quotesService, Wallet: creditService, Lessons: lessonPreparer})
	}
	steeringService := steering.New(steering.Options{Pool: db.Pool, Engine: backend, Replanner: backend, Quotes: quotesService, Financial: creditService, Events: events, Price: quotesService})
	// The shared-asset retention sweep runs in the worker (worker_sweeps.go).
	// Authored example Courses: publish their immutable objects (identical bytes, safe on every replica and rerun).
	go func() {
		if err := sharingService.InstallExampleObjects(ctx); err != nil {
			logger.Warn("example course objects not installed; they are written again at enrollment", "error", err)
		}
	}()
	// Private generated lessons use the same application identity; the worker has no app credentials.
	go courseService.MaintainPurgedObjects(ctx)
	// Same reason for narration audio of erased learners: it is purged here, with the object-store
	// keys, and regardless of NARRATION_ENABLED (the worker only regenerates).
	go narration.New(narration.Options{Pool: db.Pool, Objects: lessonObjects, Logger: logger, Retention: narrationRetention()}).MaintainOrphanedAudio(ctx, time.Hour)
	// Course invites (E15-06): push goes only through the adapter, which is disabled unless chosen
	// by environment (no APNs/FCM credentials exist yet, ADR 0012); the in-app inbox always works.
	pushAdapter, pushName := sharing.PushFromEnv(os.Getenv)
	sharingService.WithPush(pushAdapter)
	if pushName == "disabled" && (os.Getenv("PUSH_APNS_KEY_ID") != "" || os.Getenv("PUSH_FCM_SERVICE_ACCOUNT_JSON") != "") {
		logger.Warn("push credentials are set but no live push transport exists yet; invites are in-app only")
	}
	logger.Info("course invite push adapter", "adapter", pushName)
	socialService.OnBlock(sharingService.CancelInvitesOnBlock)
	playerService := player.New(player.Options{Pool: db.Pool, Objects: player.NewS3(os.Getenv), AssetOrigin: os.Getenv("LESSON_ASSET_ORIGIN"), Credits: creditService, Narration: narrationRecovery, CastSafety: characterService.SafetyProjection})
	handler := httpapi.New(httpapi.Dependencies{
		Steering:                steeringService,
		Sharing:                 sharingService,
		Characters:              characters.NewHandler(characterService),
		Gamification:            xpService,
		Social:                  socialService,
		Monitor:                 monitor,
		NarrationVoices:         narrationVoices,
		Player:                  playerService,
		ObjectStorage:           telemetry.StorageProbe(os.Getenv),
		Accounts:                auth,
		Credits:                 creditService,
		CourseBrief:             courseService,
		Tutor:                   chat.New(chat.Options{Pool: db.Pool, Engine: backend, Financial: creditService, Profiles: courseService, Personas: characterService, Slides: playerService, Events: events, Notes: notesService, Logger: logger}),
		Pricing:                 runtimePricing,
		PricingAdmin:            pricingAdmin,
		Quotes:                  quotesService,
		Notes:                   notesService,
		Voice:                   voice.New(voice.Options{Engine: backend, Pricing: runtimePricing, Lessons: voice.PoolLessons{Pool: db.Pool}, Counters: voice.RedisCounters{Client: redis}, Events: events, Logger: logger}),
		Eval:                    evalService,
		LessonTestTracerEnabled: cfg.LessonTestTracerEnabled,
		LessonTest:              lessonHandler,
		Postgres:                db.Ping,
		Redis:                   func(ctx context.Context) error { return redis.Ping(ctx).Err() },
		Engine:                  backend,
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
	if len(args) == 0 {
		return errors.New("admin subcommand requires pricing or infra")
	}
	if args[0] == "infra" {
		return runAdminInfraCLI(ctx, args[1:])
	}
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
	events, err := analytics.New(key, os.Getenv("POSTHOG_HOST"), analytics.DatabaseConsent(db.Pool))
	if err != nil {
		return err
	}
	defer func() { _ = events.Close() }()
	admin := pricing.NewAdmin(db.Pool, events, os.Getenv("PRICING_OWNER_ID"))
	admin.SetEnvironment(os.Getenv("DUOLIFY_ENV"))
	if err := admin.ConfigureVerification(os.Getenv); err != nil {
		return fmt.Errorf("pricing verifier configuration: %w", err)
	}
	status, response := admin.ExecuteCLI(ctx, *actor, method, path, body)
	_, _ = os.Stdout.Write(response)
	if status < 200 || status >= 300 {
		return fmt.Errorf("pricing %s failed with HTTP %d", operation, status)
	}
	return nil
}

func runAdminInfraCLI(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: api admin infra {plan|validate} [options]")
	}
	subcmd := args[0]
	switch subcmd {
	case "plan":
		flags := flag.NewFlagSet("admin infra plan", flag.ContinueOnError)
		targetEnv := flags.String("env", "all", "target environment: dev, staging, prod, or all")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		candidates := []string{
			".railway/plan.mjs",
			"../.railway/plan.mjs",
			"../../.railway/plan.mjs",
		}
		var scriptPath string
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				scriptPath = c
				break
			}
		}
		if scriptPath == "" {
			return errors.New("could not find .railway/plan.mjs")
		}
		cmdArgs := []string{scriptPath}
		if *targetEnv == "all" {
			cmdArgs = append(cmdArgs, "--all")
		} else {
			cmdArgs = append(cmdArgs, "--environment", *targetEnv)
		}
		cmd := exec.CommandContext(ctx, "node", cmdArgs...) // #nosec G204
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()

	case "validate":
		flags := flag.NewFlagSet("admin infra validate", flag.ContinueOnError)
		filePath := flags.String("file", "", "path to plan JSON file (reads stdin if omitted or -)")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		var data []byte
		var err error
		if *filePath != "" && *filePath != "-" {
			cleanPath := filepath.Clean(*filePath)
			data, err = os.ReadFile(cleanPath) // #nosec G304
			if err != nil {
				return fmt.Errorf("read plan file: %w", err)
			}
		} else {
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("read plan from stdin: %w", err)
			}
		}
		plans, err := backup.ParseEnvironmentPlans(data)
		if err != nil {
			return fmt.Errorf("parse environment plans: %w", err)
		}
		if len(plans) == 0 {
			return errors.New("no environment plans found in input")
		}

		var failedEnvs []string
		for _, plan := range plans {
			res := backup.ValidateEnvironmentPlan(plan)
			backup.LogEnvironmentPlanValidation(nil, plan, res)
			if !res.Valid {
				failedEnvs = append(failedEnvs, plan.Environment)
				fmt.Fprintf(os.Stderr, "Environment %s plan invalid: %s\n", plan.Environment, strings.Join(res.Violations, "; "))
			} else {
				fmt.Printf("Environment %s plan valid: summary=%s live_apply_allowed=%t hitl_gate_required=%s\n",
					plan.Environment, res.PlanSummary, res.LiveApplyAllowed, res.HITLGateRequired)
			}
		}
		if len(failedEnvs) > 0 {
			return fmt.Errorf("environment validation failed for: %s", strings.Join(failedEnvs, ", "))
		}
		return nil

	default:
		return fmt.Errorf("unknown infra subcommand %q; expected plan or validate", subcmd)
	}
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

// narrationBudget reads the live ElevenLabs character budget. It applies only when live synthesis is
// on: the deterministic mock spends no provider characters. Invalid values fall back to the Starter
// plan defaults (40,000 characters, stop at 95%, cycle resetting on the 1st).
func narrationBudget(getenv func(string) string) narration.Budget {
	if getenv("NARRATION_LIVE_ENABLED") != "true" {
		return narration.Budget{}
	}
	read := func(name string, fallback, lo, hi int) int {
		n, err := strconv.Atoi(getenv(name))
		if err != nil || n < lo || n > hi {
			return fallback
		}
		return n
	}
	return narration.Budget{
		Limit:       int64(read("NARRATION_MONTHLY_CHAR_LIMIT", narration.DefaultMonthlyCharLimit, 1, 1<<30)),
		StopPercent: read("NARRATION_BUDGET_STOP_PERCENT", narration.DefaultBudgetStopPercent, 1, 100),
		CycleDay:    read("NARRATION_CYCLE_START_DAY", 1, 1, 28),
	}
}

// narrationRetention reads how many days an unreferenced narration asset is kept (default 30).
func narrationRetention() time.Duration {
	days, err := strconv.Atoi(os.Getenv("NARRATION_RETENTION_DAYS"))
	if err != nil || days < 1 {
		return narration.DefaultRetention
	}
	return time.Duration(days) * 24 * time.Hour
}

// friendStreakSettler settles a learner's Friend streaks after their streak projection
// commits. It never fails the learner's own flow; the sweep retries what it misses.
func friendStreakSettler(svc *social.Service, logger *slog.Logger) func(context.Context, string) {
	return func(ctx context.Context, owner string) {
		if err := svc.EvaluateFriendStreaks(context.WithoutCancel(ctx), owner); err != nil {
			logger.ErrorContext(ctx, "friend streak settle failed", "error", err)
		}
	}
}

// workerServices lets the worker discover every maintenance capability by interface:
// credit expiry, financial maintenance, the 30-day course purge and deleting the Clerk users
// of erased accounts (scheduled only when Clerk is configured).
type workerServices struct {
	creditsService
	courseService
	clerkEraserService
	personEraserService
	rowRetentionService
}

type (
	creditsService      = *credits.Service
	courseService       = *courses.Service
	clerkEraserService  = *accounts.ClerkUserEraser
	personEraserService = *accounts.PostHogPersonEraser
	rowRetentionService = *retention.Service
)

var (
	_ worker.Expirer             = workerServices{}
	_ worker.AnnualIssuer        = workerServices{}
	_ worker.FinancialMaintainer = workerServices{}
	_ worker.CoursePurger        = workerServices{}
	_ worker.ClerkUserEraser     = workerServices{}
	_ worker.PersonEraser        = workerServices{}
	_ worker.RowRetainer         = workerServices{}
)
