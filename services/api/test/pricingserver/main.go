// pricingserver is an isolated Playwright fixture: empty Postgres, real migrations,
// real pricing handler, no provider calls, no production database or credentials.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/credits/creditstest"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (result error) {
	_ = os.Setenv("FEATURE_CUSTOM_CHARACTERS", "true")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	boot, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(boot, "postgres:18", postgres.WithDatabase("pricing_e2e"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		result = errors.Join(result, container.Terminate(cleanup))
	}()
	url, err := container.ConnectionString(boot, "sslmode=disable")
	if err != nil {
		return err
	}
	if err := migrations.Up(boot, url); err != nil {
		return err
	}
	db, err := store.New(boot, url)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := accounts.ApplyDeletionTombstones(boot, db.Pool); err != nil {
		return err
	}
	catalogue, err := pricing.Load(boot, db.Queries)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sink := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, "test-only-signing-key-at-least-32-bytes", sink)
	if err != nil {
		return err
	}
	rateLimits, err := accounts.RateLimitsFromEnv(os.Getenv)
	if err != nil {
		return err
	}
	if os.Getenv("AUTH_CODE_REQUESTS_PER_IP_PER_HOUR") == "" {
		rateLimits.RequestIP = 100000
	}
	if os.Getenv("AUTH_CODE_REQUESTS_PER_EMAIL_PER_HOUR") == "" {
		rateLimits.RequestEmail = 100000
	}
	if os.Getenv("AUTH_CODE_VERIFICATIONS_PER_IP_PER_HOUR") == "" {
		rateLimits.VerifyIP = 100000
	}
	auth.ConfigureRateLimits(rateLimits)
	setupPricing := catalogue
	devCatalogue, err := pricing.LoadDevelopment(boot, db.Pool)
	if err == nil {
		setupPricing = devCatalogue
	}
	auth.ConfigureSetupChat(accounts.SetupChatOptions{
		Engine:  mockE2ESetupChatEngine{},
		Pricing: setupPricing,
	})
	creditService := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, nil)
	stripe := creditstest.NewStripe()
	paidCredits := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, nil)
	if err := paidCredits.ConfigurePayments(credits.PaymentOptions{Mode: "test", WebhookSecret: creditstest.WebhookSecret, Provider: stripe}); err != nil {
		return err
	}
	creditHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Test-Stripe-Fixture") == "true" {
			paidCredits.ServeHTTP(w, r)
		} else {
			creditService.ServeHTTP(w, r)
		}
	})
	objects := &fixtureObjects{values: playerfixture.Documents()}
	lessonPlayer := player.New(player.Options{Pool: db.Pool, Objects: objects, AssetOrigin: playerfixture.AssetOrigin, Credits: paidCredits})
	mux := http.NewServeMux()
	characterService := characters.NewService(db.Pool, nil, nil)
	characterService.ConfigureScreening(characterScreenFixture{})
	characterService.ConfigureCreation(creditService, characterMapFixture{}, "development")
	go characterService.Maintain(boot)
	charHandler := characters.NewHandler(characterService)
	mux.Handle("POST /test/characters/fund", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := db.Pool.Exec(r.Context(), `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) SELECT $1::uuid,(config->'actions'->'character_create'->>'credits')::bigint*3,'purchase','e17-funding-'||$1::text,'usd-dev-character-create-unapproved' FROM pricing_versions WHERE version='usd-dev-character-create-unapproved' ON CONFLICT DO NOTHING`, accounts.LearnerID(r.Context()))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(204)
	})))

	mux.Handle("POST /test/characters", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		spec := characters.DefaultGuideFixture()
		spec.Info.Name = "Seed guide"
		raw, _ := json.Marshal(spec)
		tx, err := db.Pool.Begin(r.Context())
		if err != nil {
			http.Error(w, "seed unavailable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		defer func() { _ = tx.Rollback(r.Context()) }()
		var id, quote, creation, course string
		err = tx.QueryRow(r.Context(), `INSERT INTO characters(learner_id) VALUES($1) RETURNING id::text`, owner).Scan(&id)
		if err == nil {
			_, err = tx.Exec(r.Context(), `INSERT INTO character_versions(character_id,version,spec) VALUES($1,1,$2)`, id, raw)
		}
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT $1::uuid,encode(sha256(convert_to($1::text,'UTF8')),'hex'),version,'{}',now(),now()+interval '1 hour' FROM pricing_quote_policies ORDER BY version LIMIT 1 RETURNING id::text`, owner).Scan(&quote)
		}
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1,gen_random_uuid(),1,$2,'{}','enrolled') RETURNING id::text`, owner, quote).Scan(&creation)
		}
		if err == nil {
			err = tx.QueryRow(r.Context(), `INSERT INTO courses(learner_id,creation_id,title) VALUES($1,$2,'Original fixture course') RETURNING id::text`, owner, creation).Scan(&course)
		}
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			http.Error(w, "seed unavailable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"character_id": id, "course_id": course, "other_id": "8a333333-3333-4333-8333-333333333333"})
	})))
	mux.Handle("POST /test/player/fixture", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := playerfixture.Seed(r.Context(), db.Pool, objects, accounts.LearnerID(r.Context()))
		if err != nil {
			w.WriteHeader(500)
			return
		}
		var courseID string
		_ = db.Pool.QueryRow(r.Context(), `SELECT c.id::text FROM courses c JOIN course_units u ON u.course_id=c.id JOIN course_lesson_stubs l ON l.unit_id=u.id WHERE l.id=$1`, id).Scan(&courseID)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]string{"lesson_id": id, "course_id": courseID})
	})))
	mux.Handle("POST /test/subscription/fund", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		plan := catalogue.Config().Plans[0]
		order, err := paidCredits.CreateCheckout(r.Context(), owner, credits.CheckoutInput{
			RequestID:      uuid.NewString(),
			SKU:            plan.Monthly.SKU,
			PricingVersion: catalogue.Config().Version,
			Platform:       "web",
			ReturnTo:       "/credits",
		})
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		raw, signature, err := stripe.Notification("cs_test_"+order.ID, "paid", "invoice.paid")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "/v1/payments/stripe/webhook", bytes.NewReader(raw))
		req.Header.Set("Stripe-Signature", signature)
		req.Header.Set("X-Test-Stripe-Fixture", "true")
		rec := httptest.NewRecorder()
		paidCredits.ServeHTTP(rec, req)
		if rec.Code != 200 {
			http.Error(w, rec.Body.String(), rec.Code)
			return
		}
		if err := paidCredits.ProcessPayments(r.Context()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = paidCredits.InvalidateCache(r.Context(), owner)
		_ = creditService.InvalidateCache(r.Context(), owner)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"funded": true})
	})))
	mux.Handle("POST /test/subscription/expire", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		eventID := "evt_expire_" + uuid.NewString()
		_, err := db.Pool.Exec(r.Context(), `INSERT INTO credit_payment_inbox(event_id, environment, event_type, session_id)
			VALUES($1, 'test', 'customer.subscription.deleted', 'sub_expire')
			ON CONFLICT DO NOTHING`, eventID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, err = db.Pool.Exec(r.Context(), `INSERT INTO credit_payment_results(event_id, state)
			VALUES($1, 'expired')
			ON CONFLICT DO NOTHING`, eventID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_, err = db.Pool.Exec(r.Context(), `INSERT INTO credit_subscription_revocations(period_key, event_id, reason)
			SELECT t.period_key, $1, 'refunded'
			FROM credit_subscription_terms t
			JOIN credit_subscriptions s USING(subscription_id)
			JOIN credit_checkout_orders o ON o.id=s.order_id
			WHERE o.learner_id=$2
			ON CONFLICT DO NOTHING`, eventID, owner)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = paidCredits.InvalidateCache(r.Context(), owner)
		_ = creditService.InvalidateCache(r.Context(), owner)
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"expired": true})
	})))
	mux.Handle("/", httpapi.New(httpapi.Dependencies{Player: lessonPlayer, Pricing: pricing.NewRuntimeCatalogue(catalogue, db.Queries, db.Pool, nil), Quotes: pricing.NewQuotes(db.Pool), Credits: creditHandler, Accounts: auth, CourseBrief: courses.New(courses.Options{Pool: db.Pool, Engine: mockE2EBriefEngine{}, Pricing: setupPricing, Environment: "development"}), Characters: charHandler}, logger))
	// Exercise the real expiry worker across a historical local date. Only the
	// disposable fixture exposes this clock control; amounts stay config-owned.
	mux.Handle("POST /test/credits/yesterday", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		if _, err := creditService.EnsureDailyGrant(r.Context(), owner, time.Now().Add(-24*time.Hour)); err != nil {
			http.Error(w, "historical grant failed", http.StatusInternalServerError)
			return
		}
		if _, err := creditService.ExpireDailyCredits(r.Context(), time.Now()); err != nil {
			http.Error(w, "daily expiry failed", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})))
	mux.HandleFunc("POST /test/stripe/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			State string `json:"state"`
			Event string `json:"event"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			w.WriteHeader(400)
			return
		}
		var sessionID string
		if db.Pool.QueryRow(r.Context(), "SELECT session_id FROM credit_payment_sessions WHERE order_id=$1", r.PathValue("id")).Scan(&sessionID) != nil {
			w.WriteHeader(404)
			return
		}
		raw, signature, err := stripe.Notification(sessionID, in.State, in.Event)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "/v1/payments/stripe/webhook", bytes.NewReader(raw))
		req.Header.Set("Stripe-Signature", signature)
		// Exercise the real public router, without account credentials on webhooks.
		req.Header.Set("X-Test-Stripe-Fixture", "true")
		response := httptest.NewRecorder()
		httpapi.New(httpapi.Dependencies{Credits: creditHandler, Accounts: auth}, logger).ServeHTTP(response, req)
		if response.Code != 200 {
			w.WriteHeader(response.Code)
			return
		}
		if paidCredits.ProcessPayments(r.Context()) != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("POST /test/annual/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Month int `json:"month"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || in.Month < 0 || in.Month > 24 {
			w.WriteHeader(400)
			return
		}
		var start time.Time
		if err := db.Pool.QueryRow(r.Context(), `SELECT t.service_start FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) WHERE b.order_id=$1`, r.PathValue("id")).Scan(&start); err != nil {
			w.WriteHeader(404)
			return
		}
		if _, err := paidCredits.IssueAnnualAllowances(r.Context(), start.AddDate(0, in.Month, 0)); err != nil {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(200)
	})
	mux.Handle("POST /test/credits/{id}/settle", auth.Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner := accounts.LearnerID(r.Context())
		limits, err := creditService.Limits(r.Context(), owner, r.PathValue("id"), "quick_note")
		if err != nil {
			result, e := creditService.Settle(r.Context(), owner, r.PathValue("id"), nil)
			if e != nil {
				w.WriteHeader(409)
				return
			}
			_ = json.NewEncoder(w).Encode(result)
			return
		}
		result, err := creditService.Settle(r.Context(), owner, r.PathValue("id"), []*enginev1.Usage{{Provider: limits.Provider, Model: limits.Model, InputTokens: 100, OutputTokens: 20, Attempt: 1, Component: "content", Delivered: true}})
		if err != nil {
			w.WriteHeader(409)
			return
		}
		_ = json.NewEncoder(w).Encode(result)
	})))
	// Test-only controls bind to loopback in this disposable fixture binary.
	mux.HandleFunc("GET /test/mail", func(w http.ResponseWriter, r *http.Request) {
		webPort := os.Getenv("E2E_WEB_PORT")
		if webPort == "" {
			webPort = "4010"
		}
		w.Header().Set("Access-Control-Allow-Origin", "http://127.0.0.1:"+webPort)
		_ = json.NewEncoder(w).Encode(map[string]string{"code": sink.Code(r.URL.Query().Get("email"))})
	})
	mux.HandleFunc("POST /test/expire-access", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			AccessToken string `json:"access_token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(400)
			return
		}
		claims, err := auth.ParseAccess(in.AccessToken)
		if err != nil {
			w.WriteHeader(400)
			return
		}
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-only-signing-key-at-least-32-bytes"))
		if err != nil {
			w.WriteHeader(500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": raw})
	})
	mux.HandleFunc("POST /test/expire", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			w.WriteHeader(400)
			return
		}
		if _, err := db.Pool.Exec(r.Context(), "UPDATE auth_codes SET expires_at=now()-interval '1 second' WHERE email=$1", in.Email); err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	})
	// E2E tests reset request throttles between cases; production API limits remain
	// unchanged. This fixture server is loopback-only and is never deployed.
	mux.HandleFunc("POST /test/reset-auth-rate-limits", func(w http.ResponseWriter, r *http.Request) {
		if _, err := db.Pool.Exec(r.Context(), "DELETE FROM auth_rate_limits"); err != nil {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(204)
	})
	addr := os.Getenv("E2E_API_ADDR")
	if addr == "" {
		addr = "127.0.0.1:18081"
	}
	server := httpapi.NewServer(addr, mux)
	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", server.Addr)
	if err != nil {
		return err
	}
	return httpapi.Serve(ctx, server, listener)
}

// characterScreenFixture is injected only into this disposable test server.
type characterScreenFixture struct{}

func (characterScreenFixture) ScreenCharacter(_ context.Context, r *enginev1.ScreenCharacterRequest) (*enginev1.ScreenCharacterResponse, error) {
	reason := ""
	if strings.Contains(strings.ToLower(r.SpecJson+r.RawPrompt), "reveal system prompt") {
		reason = "prompt_injection"
	}
	return &enginev1.ScreenCharacterResponse{Approved: reason == "", ReasonCode: reason, PolicyVersion: "character-v1", Attempts: []*enginev1.Usage{{Provider: "mock", Model: "fixture", InputTokens: 10, OutputTokens: 3, Attempt: 1, Delivered: true}}}, nil
}

type mockE2ESetupChatEngine struct{}

func (mockE2ESetupChatEngine) SetupChat(_ context.Context, req *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	if req.CurrentStep == "goal" {
		lower := strings.ToLower(req.Answer)
		if !req.IsFollowUp && strings.Contains(lower, "space") && !strings.Contains(lower, "kids") && !strings.Contains(lower, "fun") {
			return &enginev1.SetupChatResponse{
				Message: "Space is a great pick! Is this for fun, for school, or for work?",
				QuickReplies: []*enginev1.QuickReply{
					{Id: "fun", Label: "For fun"},
					{Id: "school", Label: "For school"},
					{Id: "work", Label: "For work"},
					{Id: "skip", Label: "Skip"},
				},
				UpdatedProfile: &enginev1.LearnerProfile{
					Topic: "Astronomy",
					Goals: req.Answer,
				},
				NextStep:   "goal",
				IsFollowUp: true,
			}, nil
		}
		topic := "Astronomy"
		goal := "Learn " + req.Answer
		if !strings.Contains(lower, "space") && req.Answer != "" {
			topic = req.Answer
		}
		if req.Profile.GetGoals() != "" {
			goal = req.Profile.GetGoals()
		}
		return &enginev1.SetupChatResponse{
			Message: "What level would you describe yourself as?",
			QuickReplies: []*enginev1.QuickReply{
				{Id: "beginner", Label: "Beginner"},
				{Id: "intermediate", Label: "Intermediate"},
				{Id: "advanced", Label: "Advanced"},
				{Id: "expert", Label: "Expert"},
				{Id: "skip", Label: "Skip"},
			},
			UpdatedProfile: &enginev1.LearnerProfile{
				Topic: topic,
				Goals: goal,
			},
			NextStep:   "level",
			IsFollowUp: false,
		}, nil
	}
	if req.CurrentStep == "level" {
		level := strings.ToLower(req.Answer)
		return &enginev1.SetupChatResponse{
			Message: "How much time can you give it on a normal day?",
			QuickReplies: []*enginev1.QuickReply{
				{Id: "5", Label: "5 min · Casual"},
				{Id: "10", Label: "10 min · Regular"},
				{Id: "15", Label: "15 min · Serious"},
				{Id: "20", Label: "20+ min · Intense"},
				{Id: "skip", Label: "Skip"},
			},
			UpdatedProfile: &enginev1.LearnerProfile{
				Topic: req.Profile.GetTopic(),
				Goals: req.Profile.GetGoals(),
				Level: level,
			},
			NextStep:   "daily_time",
			IsFollowUp: false,
		}, nil
	}
	if req.CurrentStep == "daily_time" {
		return &enginev1.SetupChatResponse{
			Message: "Would you like daily reminders?",
			QuickReplies: []*enginev1.QuickReply{
				{Id: "20:00", Label: "8:00 pm"},
				{Id: "09:00", Label: "9:00 am"},
				{Id: "none", Label: "No reminders"},
				{Id: "skip", Label: "Skip"},
			},
			UpdatedProfile: &enginev1.LearnerProfile{
				Topic:        req.Profile.GetTopic(),
				Goals:        req.Profile.GetGoals(),
				Level:        req.Profile.GetLevel(),
				DailyMinutes: 10,
				DailyXp:      20,
			},
			NextStep:   "reminders",
			IsFollowUp: false,
		}, nil
	}
	if req.CurrentStep == "reminders" {
		return &enginev1.SetupChatResponse{
			Message: "Does this time zone look right?",
			QuickReplies: []*enginev1.QuickReply{
				{Id: "confirm", Label: "Looks right"},
				{Id: "skip", Label: "Skip"},
			},
			UpdatedProfile: &enginev1.LearnerProfile{
				Topic:           req.Profile.GetTopic(),
				Goals:           req.Profile.GetGoals(),
				Level:           req.Profile.GetLevel(),
				DailyMinutes:    req.Profile.GetDailyMinutes(),
				DailyXp:         req.Profile.GetDailyXp(),
				ReminderEnabled: false,
			},
			NextStep:   "time_zone",
			IsFollowUp: false,
		}, nil
	}
	return &enginev1.SetupChatResponse{
		Message:        "Here's what I've got. Does this look right?",
		UpdatedProfile: req.Profile,
		NextStep:       "confirm",
		IsComplete:     true,
	}, nil
}

// This fixture implements only clarification, making paid generation impossible.
type mockE2EBriefEngine struct{}

func (mockE2EBriefEngine) CourseBriefChat(_ context.Context, r *enginev1.CourseBriefChatRequest) (*enginev1.CourseBriefChatResponse, error) {
	b := r.GetBrief()
	steps := []string{"topic", "goal", "include_topics", "skip_topics", "level", "maths", "viewing_conditions", "lesson_length", "language"}
	values := []*string{&b.Topic, &b.Goal, &b.IncludeTopics, &b.SkipTopics, &b.Level, &b.Maths, &b.ViewingConditions, &b.LessonLength, &b.Language}
	next := "confirm"
	for i, step := range steps {
		if step == r.CurrentStep {
			if !r.Skipped {
				*values[i] = r.Answer
			}
			if i+1 < len(steps) {
				next = steps[i+1]
			}
			break
		}
	}
	message := "Review your brief and confirm when ready."
	if next == "goal" {
		message = "What is your goal?"
	}
	return &enginev1.CourseBriefChatResponse{Brief: b, Message: message, NextStep: next, IsComplete: next == "confirm"}, nil
}

type characterMapFixture struct{}

func (characterMapFixture) MapCharacter(_ context.Context, r *enginev1.MapCharacterRequest) (*enginev1.MapCharacterResponse, error) {
	spec := characters.DefaultGuideFixture()
	spec.Info.Name = "Original moon gardener"
	if strings.Contains(r.Prompt, "scarf") {
		spec.Appearance.Accessories = []string{"scarf"}
	}
	raw, _ := json.Marshal(spec)
	if strings.Contains(r.Prompt, "exhausted providers") {
		raw = []byte(`{}`)
	}
	return &enginev1.MapCharacterResponse{SpecJson: string(raw), Attempts: []*enginev1.Usage{{Provider: r.Limits.Provider, Model: r.Limits.Model, InputTokens: 100, OutputTokens: 100, Attempt: 1, Component: "content", Delivered: true}}}, nil
}

// Fixture documents are immutable and contain no personal content; integration
// coverage separately exercises the real RustFS adapter and protected restore.
type fixtureObjects struct{ values map[string][]byte }

func (f *fixtureObjects) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := f.values[key]
	if !ok {
		return nil, errors.New("missing fixture object")
	}
	return v, nil
}
func (f *fixtureObjects) Put(_ context.Context, key string, value []byte) error {
	if !bytes.Equal(f.values[key], value) {
		return errors.New("fixture mutation")
	}
	return nil
}
