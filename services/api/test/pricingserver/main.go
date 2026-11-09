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
	"syscall"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
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
	mux := http.NewServeMux()
	mux.Handle("/", httpapi.New(httpapi.Dependencies{Pricing: catalogue, Quotes: pricing.NewQuotes(db.Pool), Accounts: auth}, logger))
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
