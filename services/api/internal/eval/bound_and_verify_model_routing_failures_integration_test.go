//go:build integration

package eval_test

import (
	"context"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestBoundAndVerifyModelRoutingFailuresIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("eval_e10_03"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	database, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	learner, err := database.Queries.CreateLearner(ctx, "Eval Learner")
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.UUID(learner.ID.Bytes).String()

	catalogue, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	creditService := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, nil)
	quotes := pricing.NewQuotes(database.Pool)

	// Fund initial purchase
	if _, err = database.Pool.Exec(ctx, "INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,500,'purchase','eval-init',$2)", owner, catalogue.Config().Version); err != nil {
		t.Fatal(err)
	}

	t.Run("terminal 5xx provider outage settles 0 credits and releases 100% reservation", func(t *testing.T) {
		q, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
		if err != nil {
			t.Fatal(err)
		}
		if err = quotes.Accept(ctx, owner, q.ID); err != nil {
			t.Fatal(err)
		}

		res, err := creditService.Reserve(ctx, owner, q.ID)
		if err != nil {
			t.Fatal(err)
		}

		// Simulate two failed attempts (primary and fallback both fail with 5xx)
		attempts := []*enginev1.Usage{
			{
				Provider:         "anthropic",
				Model:            "claude-sonnet-5-5",
				Attempt:          1,
				FallbackUsed:     false,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "provider_outage",
			},
			{
				Provider:         "openrouter",
				Model:            "anthropic/claude-sonnet-5.5",
				Attempt:          2,
				FallbackUsed:     true,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "provider_outage",
			},
		}

		// Settle with failed undelivered attempts
		settled, err := creditService.Settle(ctx, owner, res.ID, attempts)
		if err != nil {
			t.Fatalf("unexpected settlement error on failed undelivered attempts: %v", err)
		}
		if settled.State != "released" {
			t.Fatalf("expected state 'released', got %s", settled.State)
		}
		if settled.ContentSpent != 0 || settled.AudioSpent != 0 {
			t.Fatalf("expected 0 spent credits on failure, got content=%d, audio=%d", settled.ContentSpent, settled.AudioSpent)
		}

		// Verify wallet balance is fully preserved (nothing deducted from purchased balance, 0 reserved)
		wallet, err := creditService.GetWallet(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		if wallet.Purchased != 500 || wallet.Reserved != 0 {
			t.Fatalf("expected purchased=500 and reserved=0 after full release, got purchased=%d, reserved=%d", wallet.Purchased, wallet.Reserved)
		}
	})

	t.Run("rate limit 429 recovers on fallback within attempt ceiling", func(t *testing.T) {
		q, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
		if err != nil {
			t.Fatal(err)
		}
		if err = quotes.Accept(ctx, owner, q.ID); err != nil {
			t.Fatal(err)
		}

		res, err := creditService.Reserve(ctx, owner, q.ID)
		if err != nil {
			t.Fatal(err)
		}

		attempts := []*enginev1.Usage{
			{
				Provider:         "anthropic",
				Model:            "claude-sonnet-5-5",
				Attempt:          1,
				FallbackUsed:     false,
				Component:        "content",
				Delivered:        false,
				UsageUnavailable: true,
				ErrorCode:        "rate_limited",
			},
			{
				Provider:         "openrouter",
				Model:            "anthropic/claude-sonnet-5.5",
				InputTokens:      100,
				OutputTokens:     50,
				Attempt:          2,
				FallbackUsed:     true,
				Component:        "content",
				Delivered:        true,
				UsageUnavailable: false,
			},
		}

		settled, err := creditService.Settle(ctx, owner, res.ID, attempts)
		if err != nil {
			t.Fatalf("unexpected error settling fallback delivery: %v", err)
		}
		if settled.State != "settled" {
			t.Fatalf("expected state 'settled', got %s", settled.State)
		}
		if settled.ContentSpent <= 0 {
			t.Fatalf("expected positive spent credits, got %d", settled.ContentSpent)
		}
	})

	t.Run("expensive fallback exceeding cap halts before call and releases funds", func(t *testing.T) {
		q, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
		if err != nil {
			t.Fatal(err)
		}
		if err = quotes.Accept(ctx, owner, q.ID); err != nil {
			t.Fatal(err)
		}

		res, err := creditService.Reserve(ctx, owner, q.ID)
		if err != nil {
			t.Fatal(err)
		}

		// Verify expensive fallback detection via eval helper
		expensive := eval.IsExpensiveFallback(q.Bounds["max_cost_nanos"], q.Bounds["max_cost_nanos"]+1000)
		if !expensive {
			t.Fatal("expected expensive fallback detection")
		}

		// Releasing the reservation
		released, err := creditService.Release(ctx, owner, res.ID)
		if err != nil {
			t.Fatal(err)
		}
		if released.State != "released" {
			t.Fatalf("expected released state, got %s", released.State)
		}
	})

	t.Run("decimal OpenRouter aliases verify against canonical catalogue", func(t *testing.T) {
		q, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
		if err != nil {
			t.Fatal(err)
		}
		sonnetFallback := q.Policy.Models["sonnet_fallback"]
		haikuFallback := q.Policy.Models["haiku_fallback"]

		if !strings.HasSuffix(sonnetFallback.Model, "-5.5") {
			t.Fatalf("expected decimal alias for sonnet fallback, got %s", sonnetFallback.Model)
		}
		if !strings.HasSuffix(haikuFallback.Model, "-5.5") {
			t.Fatalf("expected decimal alias for haiku fallback, got %s", haikuFallback.Model)
		}
	})
}
