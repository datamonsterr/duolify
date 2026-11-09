//go:build integration

package credits_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestE7ReviewFinancialRegressions(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("review"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	events := &testAnalyticsEvents{}
	svc := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	owner := func(t *testing.T, tz string, optOut bool) string {
		t.Helper()
		var id string
		if e := db.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('review fixture') RETURNING id::text").Scan(&id); e != nil {
			t.Fatal(e)
		}
		if _, e := db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,time_zone,analytics_opt_out) VALUES($1,$2,$3)", id, tz, optOut); e != nil {
			t.Fatal(e)
		}
		return id
	}
	t.Run("persisted opt out suppresses daily analytics", func(t *testing.T) {
		id := owner(t, "UTC", true)
		before := len(events.events)
		if _, e := svc.EnsureDailyGrant(ctx, id, time.Now()); e != nil {
			t.Fatal(e)
		}
		if len(events.events) != before {
			t.Fatal("opted-out daily grant emitted analytics")
		}
		var count int
		if e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='daily_grant'", id).Scan(&count); e != nil || count != 1 {
			t.Fatalf("opt out affected grant %d: %v", count, e)
		}
		id = owner(t, "UTC", false)
		if _, e := svc.EnsureDailyGrant(ctx, id, time.Now()); e != nil {
			t.Fatal(e)
		}
		if len(events.events) != before+1 {
			t.Fatal("consenting grant did not emit exactly once")
		}
	})
	t.Run("settling yesterday leaves existing new grant intact", func(t *testing.T) {
		id := owner(t, "Etc/GMT+12", true)
		q := acceptedQuote(t, pricing.NewQuotes(db.Pool), id, "quick_note")
		j, e := svc.Reserve(ctx, id, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Pool.Exec(ctx, "INSERT INTO credit_timezones(learner_id,time_zone,effective_at) VALUES($1,'Pacific/Kiritimati',now())", id); e != nil {
			t.Fatal(e)
		}
		// The fixture crosses the date line without waiting for wall clock midnight.
		// Explicitly append the next date's grant: it already exists when settlement runs.
		loc, _ := time.LoadLocation("Pacific/Kiritimati")
		day := time.Now().In(loc).Format(time.DateOnly)
		if _, e = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,kind,amount,reference,pricing_version,provenance) VALUES($1,'daily_grant',$2,'review-new-day',$3,jsonb_build_object('date',$4::text,'bucket','daily_free','timezone','Pacific/Kiritimati'))`, id, int64(cat.Config().DailyFree.Credits), q.PricingVersion, day); e != nil {
			t.Fatal(e)
		}
		if _, e = svc.ExpireDailyCredits(ctx, time.Now()); e != nil {
			t.Fatal(e)
		}
		if _, e = svc.Settle(ctx, id, j.ID, []*enginev1.Usage{{Provider: q.Rate.Provider, Model: q.Rate.Model, Attempt: 1, Component: "content", Delivered: true, InputTokens: 100, OutputTokens: 20}}); e != nil {
			t.Fatal(e)
		}
		wallet, e := svc.GetWallet(ctx, id)
		if e != nil || wallet.DailyFree.Available != int64(cat.Config().DailyFree.Credits) || wallet.Reserved != 0 {
			t.Fatalf("new grant reduced by yesterday: %+v %v", wallet, e)
		}
	})
	t.Run("cleanup recovers anonymized pending jobs from older backups", func(t *testing.T) {
		id := owner(t, "UTC", true)
		q := acceptedQuote(t, pricing.NewQuotes(db.Pool), id, "quick_note")
		j, e := svc.Reserve(ctx, id, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = svc.Claim(ctx, id, j.ID); e != nil {
			t.Fatal(e)
		}
		// Reproduce the retained financial rows written by the old deletion path.
		for _, sql := range []string{
			`UPDATE credit_entries SET learner_id=NULL,owner_reference=encode(sha256(convert_to($1,'UTF8')),'hex') WHERE learner_id::text=$1`,
			`UPDATE credit_timezones SET learner_id=NULL,owner_reference=encode(sha256(convert_to($1,'UTF8')),'hex') WHERE learner_id::text=$1`,
			`UPDATE credit_jobs SET learner_id=NULL WHERE learner_id::text=$1`,
			`UPDATE pricing_quotes SET learner_id=NULL WHERE learner_id::text=$1`,
		} {
			if _, e = db.Pool.Exec(ctx, sql, id); e != nil {
				t.Fatal(e)
			}
		}
		if e = svc.ReapExpired(ctx); e != nil {
			t.Fatal(e)
		}
		var state string
		var cost int64
		if e = db.Pool.QueryRow(ctx, `SELECT state,COALESCE((result->>'cost_nanos')::bigint,0) FROM credit_jobs WHERE id=$1`, j.ID).Scan(&state, &cost); e != nil || state != "released" || cost <= 0 {
			t.Fatalf("anonymous hold stranded: %s %d %v", state, cost, e)
		}
	})
	for _, maxConns := range []int32{1, 2} {
		t.Run(fmt.Sprintf("constrained pool %d", maxConns), func(t *testing.T) {
			cfg, e := pgxpool.ParseConfig(url)
			if e != nil {
				t.Fatal(e)
			}
			cfg.MaxConns = maxConns
			pool, e := pgxpool.NewWithConfig(ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			bounded := credits.NewServiceWithCatalogue(pool, nil, cat, nil)
			// Different owners/quotes exercise the locked path on every call.
			type request struct {
				owner string
				quote pricing.Quote
			}
			requests := make([]request, 12)
			for i := range requests {
				id := owner(t, "UTC", true)
				requests[i] = request{id, acceptedQuote(t, pricing.NewQuotes(db.Pool), id, "quick_note")}
			}
			timeout, stop := context.WithTimeout(ctx, 8*time.Second)
			defer stop()
			var wg sync.WaitGroup
			errs := make(chan error, len(requests)+1)
			for _, r := range requests {
				wg.Go(func() {
					j, e := bounded.Reserve(timeout, r.owner, r.quote.ID)
					if e == nil {
						_, e = bounded.Settle(timeout, r.owner, j.ID, nil)
					}
					errs <- e
				})
			}
			wg.Go(func() { _, e := bounded.ExpireDailyCredits(timeout, time.Now()); errs <- e })
			wg.Wait()
			close(errs)
			for e := range errs {
				if e != nil {
					t.Errorf("financial operations exhausted pool: %v", e)
				}
			}
		})
	}
	for _, tc := range []struct {
		name  string
		start time.Time
		hours time.Duration
	}{
		{"spring forward", time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC), 23 * time.Hour},
		{"fall back", time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), 25 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := owner(t, "America/New_York", true)
			// Initial timezone records must be effective at the historical test instant too.
			if _, e := db.Pool.Exec(ctx, "INSERT INTO credit_timezones(learner_id,time_zone,effective_at) VALUES($1,'America/New_York',$2)", id, tc.start.Add(-time.Hour)); e != nil {
				t.Fatal(e)
			}
			if _, e := svc.EnsureDailyGrant(ctx, id, tc.start); e != nil {
				t.Fatal(e)
			}
			wallet, e := svc.GetWalletAt(ctx, id, tc.start)
			if e != nil {
				t.Fatal(e)
			}
			end := tc.start.Add(tc.hours)
			if !wallet.DailyFree.ResetsAt.Equal(end) {
				t.Fatalf("DST reset %s, want %s", wallet.DailyFree.ResetsAt, end)
			}
			if _, e = svc.ExpireDailyCredits(ctx, end.Add(-time.Nanosecond)); e != nil {
				t.Fatal(e)
			}
			var expired int
			if e = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='expire'", id).Scan(&expired); e != nil || expired != 0 {
				t.Fatalf("expired before local midnight: %d %v", expired, e)
			}
			if _, e = svc.ExpireDailyCredits(ctx, end); e != nil {
				t.Fatal(e)
			}
			if _, e = svc.EnsureDailyGrant(ctx, id, end); e != nil {
				t.Fatal(e)
			}
			wallet, e = svc.GetWalletAt(ctx, id, end)
			if e != nil || wallet.DailyFree.Available != int64(cat.Config().DailyFree.Credits) {
				t.Fatalf("DST next grant %+v %v", wallet, e)
			}
			if _, e = svc.EnsureDailyGrant(ctx, id, end.Add(time.Hour)); e != nil {
				t.Fatal(e)
			}
			var grants int
			if e = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='daily_grant'", id).Scan(&grants); e != nil || grants != 2 {
				t.Fatalf("DST grants=%d %v", grants, e)
			}
		})
	}
	t.Run("financial migration cannot roll back retained history", func(t *testing.T) {
		raw, e := os.ReadFile("../../migrations/20261110050000_credit_review_guards.sql")
		if e != nil {
			t.Fatal(e)
		}
		_, down, ok := strings.Cut(string(raw), "-- +goose Down")
		if !ok {
			t.Fatal("missing migration down policy")
		}
		tx, e := db.Pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, e = tx.Exec(ctx, down); e == nil || !strings.Contains(e.Error(), "financial history requires retention") {
			t.Fatalf("migration discarded financial guards: %v", e)
		}
	})
}
