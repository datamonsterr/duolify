//go:build integration

package credits_test

import (
	"context"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestPricedPurchaseRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("purchase"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	learner, err := db.Queries.CreateLearner(ctx, "Recovery learner")
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.UUID(learner.ID.Bytes).String()
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	svc := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, nil)
	quote, err := pricing.NewQuotes(db.Pool).Create(ctx, owner, pricing.QuoteInput{Action: "preview"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := catalogue.Config()
	pack := cfg.Packs[0]
	input := credits.CheckoutInput{RequestID: uuid.NewString(), SKU: pack.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits", QuoteID: quote.ID}
	order, err := svc.CreateCheckout(ctx, owner, input)
	if err != nil {
		t.Fatal(err)
	}
	if order.State != "pending" || !order.Illustrative || order.AmountMinor != pack.AmountMinor || order.Credits != pack.Credits || order.Currency != pack.Currency {
		t.Fatalf("wrong bound order: %+v", order)
	}
	again, err := svc.CreateCheckout(ctx, owner, input)
	if err != nil || again.ID != order.ID {
		t.Fatalf("non-idempotent: %+v %v", again, err)
	}
	input.SKU = cfg.Packs[1].SKU
	if _, err = svc.CreateCheckout(ctx, owner, input); err == nil {
		t.Fatal("request key reused with different terms")
	}
	input.SKU = pack.SKU
	var entries int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1", owner).Scan(&entries); err != nil || entries != 0 {
		t.Fatalf("checkout mutated ledger: %d %v", entries, err)
	}
	// Unrelated purchase, callback arguments and repeated reads never verify an illustration.
	if _, err = db.Pool.Exec(ctx, "INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,$2,'purchase','unrelated',$3)", owner, pack.Credits, cfg.Version); err != nil {
		t.Fatal(err)
	}
	status, err := svc.GetCheckout(ctx, owner, order.ID)
	if err != nil || status.State != "pending" {
		t.Fatalf("false payment verification: %+v %v", status, err)
	}
	if _, err = svc.GetCheckout(ctx, uuid.NewString(), order.ID); err == nil {
		t.Fatal("cross-learner order leaked")
	}
	for _, query := range []string{"UPDATE credit_checkout_orders SET amount_minor=1", "DELETE FROM credit_checkout_orders", "TRUNCATE credit_checkout_orders"} {
		if _, err = db.Pool.Exec(ctx, query); err == nil {
			t.Fatalf("financial order mutation permitted: %s", query)
		}
	}
	for _, target := range []string{"https://evil.test", "//evil.test", "/credits?draft=private", "/credits#private", "/notes/private"} {
		invalid := input
		invalid.RequestID = uuid.NewString()
		invalid.ReturnTo = target
		if _, err = svc.CreateCheckout(ctx, owner, invalid); err == nil {
			t.Fatalf("unsafe target %s", target)
		}
	}
	for _, platform := range []string{"ios", "android", "unknown"} {
		invalid := input
		invalid.Platform = platform
		if _, err = svc.CreateCheckout(ctx, owner, invalid); err == nil {
			t.Fatalf("unsupported channel %s", platform)
		}
	}
	invalid := input
	invalid.PricingVersion = "stale"
	if _, err = svc.CreateCheckout(ctx, owner, invalid); err == nil {
		t.Fatal("stale pricing accepted")
	}
	invalid = input
	invalid.QuoteID = uuid.NewString()
	if _, err = svc.CreateCheckout(ctx, owner, invalid); err == nil {
		t.Fatal("foreign quote accepted")
	}
	// Tombstones erase identity, retain original terms, and prevent owner reads.
	if _, err = db.Pool.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference(owner)); err != nil {
		t.Fatal(err)
	}
	if err = accounts.ApplyDeletionTombstones(ctx, db.Pool); err != nil {
		t.Fatal(err)
	}
	var anonymous bool
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id IS NULL AND owner_reference=$2 AND amount_minor=$3 FROM credit_checkout_orders WHERE id=$1", order.ID, accounts.LearnerReference(owner), pack.AmountMinor).Scan(&anonymous); err != nil || !anonymous {
		t.Fatalf("order privacy: %v %v", anonymous, err)
	}
}
