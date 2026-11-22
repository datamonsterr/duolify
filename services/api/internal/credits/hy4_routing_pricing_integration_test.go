//go:build integration

package credits_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const hy4Version = "usd-hy4-routing-2026-11-22-unapproved"

// The Hy4/GLM routing version is an unapproved proposal: same learner prices as the latest approved
// snapshot, a quote policy that routes every action to tencent/hy4-flash then z-ai/glm-5.3-flash,
// and a settlement that accepts exactly those two models at their own rates.
func TestHy4RoutingVersionQuotesAndSettlesTheTwoModelRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("hy4"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	var approved bool
	var raw, policyRaw []byte
	if err = database.Pool.QueryRow(ctx, "SELECT approved, config FROM pricing_versions WHERE version=$1", hy4Version).Scan(&approved, &raw); err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Fatal("the Hy4 routing version must stay unapproved until the owner approves it")
	}
	if err = database.Pool.QueryRow(ctx, "SELECT policy FROM pricing_quote_policies WHERE version=$1", hy4Version).Scan(&policyRaw); err != nil {
		t.Fatal(err)
	}
	active, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	if active.Config().Version == hy4Version {
		t.Fatal("an unapproved version must never be the active catalogue")
	}
	cfg, err := pricing.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	var policy pricing.QuotePolicy
	if err = json.Unmarshal(policyRaw, &policy); err != nil {
		t.Fatal(err)
	}

	// Learner prices are the latest approved snapshot's, unchanged.
	fixture, err := os.ReadFile("../pricing/fixtures/usd_character_create.json")
	if err != nil {
		t.Fatal(err)
	}
	latest, err := pricing.Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Actions) != len(latest.Actions) {
		t.Fatalf("actions: %d, latest approved: %d", len(cfg.Actions), len(latest.Actions))
	}
	for name, action := range latest.Actions {
		if cfg.Actions[name] != action {
			t.Fatalf("credit price of %s changed: %+v, latest approved %+v", name, cfg.Actions[name], action)
		}
	}
	if cfg.Narration != latest.Narration || string(cfg.Limits) == "" {
		t.Fatal("narration price or limits changed")
	}

	// Every model action quotes on Hy4 with GLM behind it.
	for name := range cfg.Actions {
		if name == "narration" || name == "cached_replay" {
			continue
		}
		q, err := pricing.CalculateQuote(cfg, policy, pricing.QuoteInput{Action: name}, time.Now())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if q.Rate.Model != "tencent/hy4-flash" || q.Rate.Provider != "openrouter" {
			t.Fatalf("%s: primary %+v", name, q.Rate)
		}
		if fallbacks := q.Policy.Fallbacks(q.Input.Model); len(fallbacks) != 1 || fallbacks[0].Model != "z-ai/glm-5.3-flash" {
			t.Fatalf("%s: fallbacks %+v", name, fallbacks)
		}
	}

	q, err := pricing.CalculateQuote(cfg, policy, pricing.QuoteInput{Action: "quick_note"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	usage := func(model string, fallback bool) []*enginev1.Usage {
		return []*enginev1.Usage{{Provider: "openrouter", Model: model, Attempt: 1, Component: "content", Delivered: true, FallbackUsed: fallback, InputTokens: 1000, OutputTokens: 300}}
	}
	got, err := credits.CalculateSettlement(q, usage("tencent/hy4-flash", false))
	if err != nil || got.CostNanos != 1000*35+300*141 {
		t.Fatalf("primary: %+v %v", got, err)
	}
	got, err = credits.CalculateSettlement(q, usage("z-ai/glm-5.3-flash", true))
	if err != nil || got.CostNanos != 1000*40+300*79 {
		t.Fatalf("fallback: %+v %v", got, err)
	}
	if _, err = credits.CalculateSettlement(q, usage("anthropic/claude-haiku-5.5", true)); err == nil {
		t.Fatal("a model outside the route must be rejected")
	}
}
