//go:build integration

package pricing_test

import (
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestOpenRouterNarrationCostOnlyApprovalKeepsCreditsAndOldQuote(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("narration_policy"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	url, err := c.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	var learner string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('Narration learner') RETURNING id::text`).Scan(&learner); err != nil {
		t.Fatal(err)
	}
	quotes := pricing.NewQuotes(db.Pool)
	input := pricing.QuoteInput{Action: "narration", NarrationCharacters: 100, Voice: "default"}
	before, err := quotes.Create(ctx, learner, input)
	if err != nil {
		t.Fatal(err)
	}
	if err = narrationtest.ActivateOpenRouterCostPolicy(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	after, err := quotes.Create(ctx, learner, input)
	if err != nil {
		t.Fatal(err)
	}
	old, err := quotes.Get(ctx, learner, before.ID)
	if err != nil || old.PricingVersion != before.PricingVersion || old.PricingVersion == after.PricingVersion || old.AudioCeiling != after.AudioCeiling {
		t.Fatalf("quote before %s/%d after %s/%d err %v", old.PricingVersion, old.AudioCeiling, after.PricingVersion, after.AudioCeiling, err)
	}
	usage := []*pb.Usage{{Provider: "openrouter", Model: "hexgrad/kokoro-82m", Component: "audio", Attempt: 1, NarrationCharacters: 100, Delivered: true}}
	if _, err = credits.CalculateSettlement(old, usage); err == nil {
		t.Fatal("historical quote authorized a new provider")
	}
	settled, err := credits.CalculateSettlement(after, usage)
	if err != nil || settled.AudioSpent != old.AudioCeiling || settled.CostNanos != 400000 {
		t.Fatalf("OpenRouter cost-only settlement %+v %v", settled, err)
	}
	reported := uint64(99000)
	usage[0].ProviderCostNanos = &reported
	settled, err = credits.CalculateSettlement(after, usage)
	if err != nil || settled.AudioSpent != old.AudioCeiling || settled.CostNanos != 99000 {
		t.Fatalf("reported provider cost changed delivered credit terms: %+v %v", settled, err)
	}
}
