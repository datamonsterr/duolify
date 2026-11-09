//go:build integration

package credits_test

import (
	"context"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestReadAuditableCreditWallet_Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	pgContainer, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("duolify_credits_test"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, pgContainer)

	pgURL, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}

	if err := migrations.Up(ctx, pgURL); err != nil {
		t.Fatalf("migrations.Up failed: %v", err)
	}

	db, err := store.New(ctx, pgURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Start real Redis container
	redisContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForLog("Ready to accept connections"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, redisContainer)

	redisEndpoint, err := redisContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}

	redisClient, err := cache.New("redis://" + redisEndpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = redisClient.Close() }()

	svc := credits.NewService(db.Pool, redisClient)

	// Create test learners
	learnerA, err := db.Queries.CreateLearner(ctx, "Learner A")
	if err != nil {
		t.Fatal(err)
	}
	learnerB, err := db.Queries.CreateLearner(ctx, "Learner B")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Postgres append-only constraints and prohibited new reward writes", func(t *testing.T) {
		// New reward write must be rejected
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference)
			VALUES ($1, 50, 'reward', 'streak_milestone')
		`, learnerA.ID)
		if err == nil {
			t.Fatal("expected insert with kind 'reward' to fail, but it succeeded")
		}

		// Valid kinds: daily_grant, subscription_grant, expire, purchase, spend, reserve, release, refund
		validKinds := []struct {
			kind   string
			amount int64
		}{
			{"daily_grant", 46},
			{"purchase", 400},
			{"subscription_grant", 60},
			{"reserve", -55},
			{"release", 3},
			{"spend", -8},
			{"expire", -6},
			{"refund", 20},
		}

		for _, k := range validKinds {
			_, err := db.Pool.Exec(ctx, `
				INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, learnerA.ID, k.amount, k.kind, "ref-"+k.kind, "usd-launch-2026-11-08", `{"channel":"test"}`)
			if err != nil {
				t.Fatalf("failed to insert valid kind %q: %v", k.kind, err)
			}
		}

		// Immutability checks: UPDATE, DELETE, TRUNCATE must fail
		for _, q := range []string{
			"UPDATE credit_entries SET amount = 999",
			"DELETE FROM credit_entries",
			"TRUNCATE credit_entries",
		} {
			if _, err := db.Pool.Exec(ctx, q); err == nil {
				t.Fatalf("expected ledger mutation %q to fail, but it succeeded", q)
			}
		}

		// Anonymization UPDATE must succeed (learner_id set to NULL and owner_reference set to 64 hex chars)
		rowID := int64(0)
		err = db.Pool.QueryRow(ctx, "SELECT id FROM credit_entries WHERE learner_id=$1 LIMIT 1", learnerA.ID).Scan(&rowID)
		if err != nil {
			t.Fatal(err)
		}
		anonRef := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
		_, err = db.Pool.Exec(ctx, "UPDATE credit_entries SET learner_id=NULL, owner_reference=$1 WHERE id=$2", anonRef, rowID)
		if err != nil {
			t.Fatalf("expected anonymization UPDATE to succeed, got %v", err)
		}
	})

	t.Run("Legacy reward rows are readable in history but unspendable in balances", func(t *testing.T) {
		// Clean test learner
		learnerC, err := db.Queries.CreateLearner(ctx, "Learner C")
		if err != nil {
			t.Fatal(err)
		}

		// Temporarily disable trigger to simulate a historical legacy row created before the reward prohibition
		_, err = db.Pool.Exec(ctx, "ALTER TABLE credit_entries DISABLE TRIGGER credit_entries_no_reward")
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference)
			VALUES ($1, 100, 'reward', 'legacy-streak-milestone')
		`, learnerC.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.Pool.Exec(ctx, "ALTER TABLE credit_entries ENABLE TRIGGER credit_entries_no_reward")
		if err != nil {
			t.Fatal(err)
		}

		// Add a purchase of 50
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference)
			VALUES ($1, 50, 'purchase', 'pack-50')
		`, learnerC.ID)
		if err != nil {
			t.Fatal(err)
		}

		wallet, err := svc.GetWallet(ctx, learnerC.ID.String())
		if err != nil {
			t.Fatalf("GetWallet failed: %v", err)
		}

		// Available balance must be 50 (purchased), NOT 150 (legacy reward must NOT be spendable)
		if wallet.Available != 50 {
			t.Fatalf("expected available balance 50 (excluding unspendable reward), got %d", wallet.Available)
		}
		if wallet.Purchased != 50 {
			t.Fatalf("expected purchased balance 50, got %d", wallet.Purchased)
		}

		// Activity list MUST include the legacy reward entry
		activity, err := svc.ListActivity(ctx, learnerC.ID.String(), 10, "")
		if err != nil {
			t.Fatalf("ListActivity failed: %v", err)
		}
		foundReward := false
		for _, e := range activity.Entries {
			if e.Kind == "reward" && e.Amount == 100 {
				foundReward = true
				break
			}
		}
		if !foundReward {
			t.Fatal("expected legacy reward entry to be visible in activity history")
		}
	})

	t.Run("Balance queries, Redis caching, Redis loss and learner isolation", func(t *testing.T) {
		// Learner B ledger entries:
		// daily_grant: 46
		// spend: -8 (daily free)
		// purchase: 400
		// subscription_grant: 60
		// reserve: -55 (reference: res-100)
		// release: 3 (reference: res-100, outline settled at 52)
		entries := []struct {
			amount int64
			kind   string
			ref    string
			prov   string
		}{
			{46, "daily_grant", "dg-2026-11-09", `{"bucket":"daily_free"}`},
			{-8, "spend", "spend-tutor", `{"bucket":"daily_free"}`},
			{400, "purchase", "stripe-ch-1", `{"bucket":"purchase"}`},
			{60, "subscription_grant", "sub-grant-1", `{"bucket":"subscription"}`},
			{-55, "reserve", "res-100", `{"bucket":"purchase"}`},
			{3, "release", "res-100", `{"bucket":"purchase","detail":"Outline settled at 52"}`},
		}

		for _, e := range entries {
			_, err := db.Pool.Exec(ctx, `
				INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
				VALUES ($1, $2, $3, $4, 'usd-launch-2026-11-08', $5::jsonb)
			`, learnerB.ID, e.amount, e.kind, e.ref, e.prov)
			if err != nil {
				t.Fatalf("failed to insert entry: %v", err)
			}
		}

		// Expected totals:
		// Daily free: 46 - 8 = 38
		// Purchased: 400 - 55 + 3 = 348 (52 spent net)
		// Subscription: 60
		// Reserved: 0 (since release offset the remaining 3 of 55, settled)
		// Available: 38 + 348 + 60 = 446
		wallet1, err := svc.GetWallet(ctx, learnerB.ID.String())
		if err != nil {
			t.Fatalf("GetWallet failed: %v", err)
		}

		if wallet1.Available != 446 {
			t.Fatalf("expected available 446, got %d", wallet1.Available)
		}
		if wallet1.DailyFree.Available != 38 || wallet1.DailyFree.Granted != 46 {
			t.Fatalf("expected daily free 38/46, got %d/%d", wallet1.DailyFree.Available, wallet1.DailyFree.Granted)
		}
		if wallet1.Purchased != 348 {
			t.Fatalf("expected purchased 348, got %d", wallet1.Purchased)
		}
		if wallet1.Subscription != 60 {
			t.Fatalf("expected subscription 60, got %d", wallet1.Subscription)
		}
		if wallet1.Reserved != 0 {
			t.Fatalf("expected reserved 0, got %d", wallet1.Reserved)
		}

		// Flush Redis completely
		if err := redisClient.FlushAll(ctx).Err(); err != nil {
			t.Fatalf("failed to flush redis: %v", err)
		}

		// Re-query: must derive identical totals directly from Postgres ledger
		wallet2, err := svc.GetWallet(ctx, learnerB.ID.String())
		if err != nil {
			t.Fatalf("GetWallet after Redis loss failed: %v", err)
		}
		if wallet2.Available != wallet1.Available ||
			wallet2.DailyFree.Available != wallet1.DailyFree.Available ||
			wallet2.Purchased != wallet1.Purchased ||
			wallet2.Subscription != wallet1.Subscription ||
			wallet2.Reserved != wallet1.Reserved {
			t.Fatalf("balance mismatch after Redis loss: %+v vs %+v", wallet1, wallet2)
		}

		// Isolation check: Learner with empty ledger has 0 balance and no leak from Learner B
		emptyLearnerID := uuid.New().String()
		_, err = db.Pool.Exec(ctx, "INSERT INTO learners (id, display_name) VALUES ($1::uuid, 'Empty Learner')", emptyLearnerID)
		if err != nil {
			t.Fatal(err)
		}
		emptyWallet, err := svc.GetWallet(ctx, emptyLearnerID)
		if err != nil {
			t.Fatalf("empty learner GetWallet failed: %v", err)
		}
		if emptyWallet.Available != 0 || emptyWallet.Purchased != 0 || emptyWallet.Subscription != 0 {
			t.Fatalf("expected zero balances for empty learner, got %+v", emptyWallet)
		}
	})

	t.Run("Cursor-paginated activity", func(t *testing.T) {
		learnerP, err := db.Queries.CreateLearner(ctx, "Learner P")
		if err != nil {
			t.Fatal(err)
		}

		// Insert 25 entries
		for i := 1; i <= 25; i++ {
			_, err := db.Pool.Exec(ctx, `
				INSERT INTO credit_entries (learner_id, amount, kind, reference)
				VALUES ($1, $2, 'purchase', $3)
			`, learnerP.ID, int64(i*10), "order-"+uuid.New().String())
			if err != nil {
				t.Fatal(err)
			}
		}

		// Page 1: limit 10
		page1, err := svc.ListActivity(ctx, learnerP.ID.String(), 10, "")
		if err != nil {
			t.Fatalf("page 1 failed: %v", err)
		}
		if len(page1.Entries) != 10 {
			t.Fatalf("expected 10 entries on page 1, got %d", len(page1.Entries))
		}
		if page1.NextCursor == "" {
			t.Fatal("expected non-empty NextCursor for page 1")
		}

		// Page 2: limit 10 with cursor from page 1
		page2, err := svc.ListActivity(ctx, learnerP.ID.String(), 10, page1.NextCursor)
		if err != nil {
			t.Fatalf("page 2 failed: %v", err)
		}
		if len(page2.Entries) != 10 {
			t.Fatalf("expected 10 entries on page 2, got %d", len(page2.Entries))
		}
		if page2.NextCursor == "" {
			t.Fatal("expected non-empty NextCursor for page 2")
		}

		// Verify no overlap between page 1 and page 2
		seen := make(map[int64]bool)
		for _, e := range page1.Entries {
			seen[e.ID] = true
		}
		for _, e := range page2.Entries {
			if seen[e.ID] {
				t.Fatalf("duplicate entry %d across pages", e.ID)
			}
			seen[e.ID] = true
		}

		// Page 3: limit 10 with cursor from page 2
		page3, err := svc.ListActivity(ctx, learnerP.ID.String(), 10, page2.NextCursor)
		if err != nil {
			t.Fatalf("page 3 failed: %v", err)
		}
		if len(page3.Entries) != 5 {
			t.Fatalf("expected 5 remaining entries on page 3, got %d", len(page3.Entries))
		}
		if page3.NextCursor != "" {
			t.Fatalf("expected empty NextCursor on last page, got %q", page3.NextCursor)
		}
	})
}
