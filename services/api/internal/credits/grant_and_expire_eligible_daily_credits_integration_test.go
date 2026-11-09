//go:build integration

package credits_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/cache"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

type recordedEvent struct {
	LearnerID  string
	Event      string
	Properties map[string]any
}

type testAnalyticsEvents struct {
	mu     sync.Mutex
	events []recordedEvent
}

func (e *testAnalyticsEvents) Capture(learnerID, event string, properties map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, recordedEvent{
		LearnerID:  learnerID,
		Event:      event,
		Properties: properties,
	})
	return nil
}

func (e *testAnalyticsEvents) Close() error { return nil }

func (e *testAnalyticsEvents) GetEvents(event string) []recordedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	var res []recordedEvent
	for _, ev := range e.events {
		if ev.Event == event {
			res = append(res, ev)
		}
	}
	return res
}

func TestGrantAndExpireEligibleDailyCredits_Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	pgContainer, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("duolify_daily_credits_test"),
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

	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatalf("load pricing catalogue: %v", err)
	}

	events := &testAnalyticsEvents{}
	svc := credits.NewServiceWithCatalogue(db.Pool, redisClient, catalogue, events)

	learnerA, err := db.Queries.CreateLearner(ctx, "Daily Learner A")
	if err != nil {
		t.Fatal(err)
	}
	learnerAID := uuid.UUID(learnerA.ID.Bytes).String()
	// Event delivery requires an explicit persisted consent record.
	if _, err = db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,time_zone,analytics_opt_out) VALUES($1,'Asia/Ho_Chi_Minh',false)", learnerAID); err != nil {
		t.Fatal(err)
	}

	t.Run("Postgres grant uniqueness under concurrency", func(t *testing.T) {
		const concurrency = 10
		var wg sync.WaitGroup
		errs := make(chan error, concurrency)
		now := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.EnsureDailyGrant(ctx, learnerAID, now)
				if err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			t.Fatalf("EnsureDailyGrant failed under concurrency: %v", err)
		}

		// Verify exactly 1 daily_grant entry exists for this learner and date in Postgres
		var count int
		err := db.Pool.QueryRow(ctx, `
			SELECT count(*) FROM credit_entries
			WHERE learner_id = $1 AND kind = 'daily_grant' AND provenance->>'date' = '2026-11-10'
		`, learnerA.ID).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected exactly 1 daily_grant row in Postgres, got %d", count)
		}

		// Verify event daily_credits_granted was captured
		grantedEvents := events.GetEvents("daily_credits_granted")
		if len(grantedEvents) < 1 {
			t.Fatalf("expected at least 1 daily_credits_granted event, got %d", len(grantedEvents))
		}
	})

	t.Run("Bucket provenance and spend order", func(t *testing.T) {
		learnerB, err := db.Queries.CreateLearner(ctx, "Bucket Learner B")
		if err != nil {
			t.Fatal(err)
		}
		learnerBID := uuid.UUID(learnerB.ID.Bytes).String()

		now := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)
		// 1. Grant daily free (46)
		_, err = svc.EnsureDailyGrant(ctx, learnerBID, now)
		if err != nil {
			t.Fatal(err)
		}

		// 2. Add subscription grant (600) and purchased pack (400)
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
			VALUES
				($1, 600, 'subscription_grant', 'sub-grant-1', $2, '{"bucket":"subscription"}'::jsonb),
				($1, 400, 'purchase', 'pack-400', $2, '{"bucket":"purchase"}'::jsonb)
		`, learnerB.ID, catalogue.Config().Version)
		if err != nil {
			t.Fatal(err)
		}

		wallet, err := svc.GetWalletAt(ctx, learnerBID, now)
		if err != nil {
			t.Fatal(err)
		}
		expectedDaily := int64(catalogue.Config().DailyFree.Credits)
		if wallet.DailyFree.Available != expectedDaily || wallet.Subscription != 600 || wallet.Purchased != 400 {
			t.Fatalf("expected %d daily, 600 sub, 400 purchased; got %+v", expectedDaily, wallet)
		}
		if wallet.Available != expectedDaily+1000 {
			t.Fatalf("expected total available %d, got %d", expectedDaily+1000, wallet.Available)
		}

		// Verify eligibility: note and chat are eligible for daily free, preview/outline are not
		if !svc.IsActionEligibleForDailyFree("quick_note") {
			t.Fatal("expected quick_note to be eligible for daily free")
		}
		if !svc.IsActionEligibleForDailyFree("tutor_short") {
			t.Fatal("expected tutor_short to be eligible for daily free")
		}
		if !svc.IsActionEligibleForDailyFree("tutor_long") {
			t.Fatal("expected tutor_long to be eligible for daily free")
		}
		if svc.IsActionEligibleForDailyFree("preview") {
			t.Fatal("expected preview NOT to be eligible for daily free")
		}
		if svc.IsActionEligibleForDailyFree("outline") {
			t.Fatal("expected outline NOT to be eligible for daily free")
		}
	})

	t.Run("Expiry and elapsed-time worker", func(t *testing.T) {
		learnerC, err := db.Queries.CreateLearner(ctx, "Expiry Learner C")
		if err != nil {
			t.Fatal(err)
		}
		learnerCID := uuid.UUID(learnerC.ID.Bytes).String()

		// Day 1: grant daily credits, spend 8 on tutor chat, reserve 15 for note
		day1 := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
		_, err = svc.EnsureDailyGrant(ctx, learnerCID, day1)
		if err != nil {
			t.Fatal(err)
		}

		pricingVer := catalogue.Config().Version
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
			VALUES
				($1, -8, 'spend', 'chat-1', $2, '{"bucket":"daily_free","date":"2026-11-09"}'::jsonb),
				($1, -15, 'reserve', 'res-1', $2, '{"bucket":"daily_free","date":"2026-11-09"}'::jsonb)
		`, learnerC.ID, pricingVer)
		if err != nil {
			t.Fatal(err)
		}

		// Day 2 arrives (midnight crossed): run expiry worker as of Day 2
		day2 := time.Date(2026, 11, 10, 1, 0, 0, 0, time.UTC)
		expiredCount, err := svc.ExpireDailyCredits(ctx, day2)
		if err != nil {
			t.Fatal(err)
		}
		if expiredCount < 1 {
			t.Fatalf("expected at least 1 expired entry, got %d", expiredCount)
		}

		// Unreserved remainder for Day 1 was DailyFree.Credits - 8 - 15
		expectedRemainder := -(int64(catalogue.Config().DailyFree.Credits) - 8 - 15)
		var expireAmount int64
		err = db.Pool.QueryRow(ctx, `
			SELECT amount FROM credit_entries
			WHERE learner_id = $1 AND kind = 'expire' AND provenance->>'date' = '2026-11-09'
		`, learnerC.ID).Scan(&expireAmount)
		if err != nil {
			t.Fatalf("expected expire row in Postgres: %v", err)
		}
		if expireAmount != expectedRemainder {
			t.Fatalf("expected expire amount %d (unreserved remainder), got %d", expectedRemainder, expireAmount)
		}

		// Ensure Day 2 grant provides fresh credits independent of Day 1
		_, err = svc.EnsureDailyGrant(ctx, learnerCID, day2)
		if err != nil {
			t.Fatal(err)
		}

		wallet, err := svc.GetWalletAt(ctx, learnerCID, day2)
		if err != nil {
			t.Fatal(err)
		}
		expectedFreshDaily := int64(catalogue.Config().DailyFree.Credits)
		if wallet.DailyFree.Available != expectedFreshDaily {
			t.Fatalf("expected fresh %d daily free on Day 2, got %d", expectedFreshDaily, wallet.DailyFree.Available)
		}

		// When the 15 reservation releases 5 after Day 1 expired, it must NOT roll over into Day 2
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
			VALUES
				($1, 5, 'release', 'res-1', $2, '{"bucket":"daily_free","date":"2026-11-09"}'::jsonb)
		`, learnerC.ID, pricingVer)
		if err != nil {
			t.Fatal(err)
		}

		walletAfterRelease, err := svc.GetWalletAt(ctx, learnerCID, day2)
		if err != nil {
			t.Fatal(err)
		}
		if walletAfterRelease.DailyFree.Available > expectedFreshDaily {
			t.Fatalf("released expired allocation leaked into Day 2: got %d", walletAfterRelease.DailyFree.Available)
		}
	})

	t.Run("Timezone 24h delay and elapsed-time anti-farming guard", func(t *testing.T) {
		learnerD, err := db.Queries.CreateLearner(ctx, "Timezone Learner D")
		if err != nil {
			t.Fatal(err)
		}
		learnerDID := uuid.UUID(learnerD.ID.Bytes).String()

		t0 := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC)
		// Initial grant in default timezone (Asia/Ho_Chi_Minh, UTC+7)
		_, err = svc.EnsureDailyGrant(ctx, learnerDID, t0)
		if err != nil {
			t.Fatal(err)
		}

		// Request timezone change to America/New_York (UTC-5)
		err = svc.UpdateTimezone(ctx, learnerDID, "America/New_York", t0)
		if err != nil {
			t.Fatal(err)
		}

		// 1 hour later: change should NOT be effective yet (24-hour delay)
		t1 := t0.Add(1 * time.Hour)
		tzEffective1, err := svc.GetEffectiveTimezone(ctx, learnerDID, t1)
		if err != nil {
			t.Fatal(err)
		}
		if tzEffective1 != "Asia/Ho_Chi_Minh" {
			t.Fatalf("expected effective timezone to remain Asia/Ho_Chi_Minh before 24h, got %s", tzEffective1)
		}

		// 25 hours later: change is now effective
		t25 := t0.Add(25 * time.Hour)
		tzEffective25, err := svc.GetEffectiveTimezone(ctx, learnerDID, t25)
		if err != nil {
			t.Fatal(err)
		}
		if tzEffective25 != "America/New_York" {
			t.Fatalf("expected effective timezone to be America/New_York after 24h, got %s", tzEffective25)
		}
	})
}
