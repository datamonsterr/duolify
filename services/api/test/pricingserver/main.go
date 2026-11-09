// pricingserver is an isolated Playwright fixture: empty Postgres, real migrations,
// real pricing handler, no provider calls, no production database or credentials.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/golang-jwt/jwt/v5"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (result error) {
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
	creditService := credits.NewService(db.Pool, nil)
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.New(httpapi.Dependencies{Pricing: pricing.NewRuntimeCatalogue(catalogue, db.Queries, db.Pool, nil), Quotes: pricing.NewQuotes(db.Pool), Credits: creditService, Accounts: auth}, logger))
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
