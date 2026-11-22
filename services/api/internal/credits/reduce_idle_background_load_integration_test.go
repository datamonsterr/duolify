//go:build integration

package credits_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestReducedIdleBackgroundLoadKeepsCreditsExact_Integration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("idle_credits"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	defer db.Close()
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	newService := func() (*credits.Service, *testAnalyticsEvents) {
		events := &testAnalyticsEvents{}
		return credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, events), events
	}
	learner := func(name, zone string) string {
		t.Helper()
		l, e := db.Queries.CreateLearner(ctx, name)
		if e != nil {
			t.Fatal(e)
		}
		id := uuid.UUID(l.ID.Bytes).String()
		if _, e = db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,time_zone,analytics_opt_out) VALUES($1,$2,false)", id, zone); e != nil {
			t.Fatal(e)
		}
		return id
	}
	expired := func(id string) int {
		t.Helper()
		var n int
		if e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='expire'", id).Scan(&n); e != nil {
			t.Fatal(e)
		}
		return n
	}

	t.Run("daily expiry follows each learner's own local midnight in two time zones", func(t *testing.T) {
		svc, _ := newService()
		auckland := learner("Auckland", "Pacific/Auckland")         // UTC+13 in November
		losAngeles := learner("Los Angeles", "America/Los_Angeles") // UTC-8
		grantedAt := time.Date(2026, 11, 10, 10, 0, 0, 0, time.UTC) // Nov 10 23:00 in Auckland, Nov 10 02:00 in Los Angeles
		for _, id := range []string{auckland, losAngeles} {
			if g, e := svc.EnsureDailyGrant(ctx, id, grantedAt); e != nil || g == nil {
				t.Fatalf("grant %s: %v", id, e)
			}
		}
		// Two hours later Auckland is on Nov 11, Los Angeles is still on Nov 10.
		n, err := svc.ExpireDailyCredits(ctx, grantedAt.Add(2*time.Hour))
		if err != nil || n != 1 {
			t.Fatalf("first pass expired %d, err %v, want 1", n, err)
		}
		if expired(auckland) != 1 || expired(losAngeles) != 0 {
			t.Fatalf("expire rows auckland=%d los angeles=%d, want 1 and 0", expired(auckland), expired(losAngeles))
		}
		// A day later Los Angeles crosses its own midnight; Auckland is not expired twice.
		n, err = svc.ExpireDailyCredits(ctx, grantedAt.Add(26*time.Hour))
		if err != nil || n != 1 {
			t.Fatalf("second pass expired %d, err %v, want 1", n, err)
		}
		if expired(auckland) != 1 || expired(losAngeles) != 1 {
			t.Fatalf("expire rows auckland=%d los angeles=%d, want 1 and 1", expired(auckland), expired(losAngeles))
		}
		if n, err = svc.ExpireDailyCredits(ctx, grantedAt.Add(27*time.Hour)); err != nil || n != 0 {
			t.Fatalf("idle pass expired %d, err %v, want 0", n, err)
		}
		want := -int64(catalogue.Config().DailyFree.Credits)
		var amount int64
		if err = db.Pool.QueryRow(ctx, "SELECT amount FROM credit_entries WHERE learner_id=$1 AND kind='expire'", auckland).Scan(&amount); err != nil || amount != want {
			t.Fatalf("expired amount %d, err %v, want %d", amount, err, want)
		}
	})

	t.Run("two workers expire the same grants once", func(t *testing.T) {
		first, _ := newService()
		second, _ := newService()
		grantedAt := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
		ids := make([]string, 6)
		for i := range ids {
			ids[i] = learner(fmt.Sprintf("Pair %d", i), "UTC")
			if _, err := first.EnsureDailyGrant(ctx, ids[i], grantedAt); err != nil {
				t.Fatal(err)
			}
		}
		asOf := grantedAt.Add(24 * time.Hour)
		var wg sync.WaitGroup
		var mu sync.Mutex
		total := 0
		errs := []error{}
		for _, svc := range []*credits.Service{first, second, first, second} {
			wg.Go(func() {
				n, e := svc.ExpireDailyCredits(ctx, asOf)
				mu.Lock()
				defer mu.Unlock()
				total += n
				if e != nil {
					errs = append(errs, e)
				}
			})
		}
		wg.Wait()
		if len(errs) > 0 || total != len(ids) {
			t.Fatalf("expired %d (want %d), errors %v", total, len(ids), errs)
		}
		for _, id := range ids {
			if expired(id) != 1 {
				t.Fatalf("learner %s has %d expire rows, want exactly 1", id, expired(id))
			}
		}
	})

	t.Run("sweep looks back a few days, a fresh process and a reservation look at everything", func(t *testing.T) {
		svc, _ := newService()
		id := learner("Old Grant", "UTC")
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance) VALUES($1,40,'daily_grant',$2,$3,'{"bucket":"daily_free","date":"2026-09-01"}'::jsonb)`, id, "daily_grant:"+id+":2026-09-01", catalogue.Config().Version); err != nil {
			t.Fatal(err)
		}
		asOf := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
		// The first pass of a process covers the whole ledger.
		if n, err := svc.ExpireDailyCredits(ctx, asOf); err != nil || n < 1 || expired(id) != 1 {
			t.Fatalf("full pass expired %d, err %v, learner rows %d", n, err, expired(id))
		}
		old := learner("Older Grant", "UTC")
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance) VALUES($1,40,'daily_grant',$2,$3,'{"bucket":"daily_free","date":"2026-09-02"}'::jsonb)`, old, "daily_grant:"+old+":2026-09-02", catalogue.Config().Version); err != nil {
			t.Fatal(err)
		}
		// Later passes of the same process only look back a few days...
		if n, err := svc.ExpireDailyCredits(ctx, asOf.Add(time.Hour)); err != nil || n != 0 || expired(old) != 0 {
			t.Fatalf("windowed pass expired %d, err %v, learner rows %d, want 0", n, err, expired(old))
		}
		// ...the learner's own next wallet read or reservation finds it...
		if n, err := svc.ExpireLearnerDailyCredits(ctx, old, asOf.Add(time.Hour)); err != nil || n != 1 || expired(old) != 1 {
			t.Fatalf("learner pass expired %d, err %v, learner rows %d, want 1", n, err, expired(old))
		}
		// ...and a restarted worker sweeps everything again.
		third := learner("Oldest Grant", "UTC")
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance) VALUES($1,40,'daily_grant',$2,$3,'{"bucket":"daily_free","date":"2026-09-03"}'::jsonb)`, third, "daily_grant:"+third+":2026-09-03", catalogue.Config().Version); err != nil {
			t.Fatal(err)
		}
		restarted, _ := newService()
		if n, err := restarted.ExpireDailyCredits(ctx, asOf.Add(2*time.Hour)); err != nil || n != 1 || expired(third) != 1 {
			t.Fatalf("restarted worker expired %d, err %v, learner rows %d, want 1", n, err, expired(third))
		}
	})

	t.Run("expiring one learner leaves every other learner untouched", func(t *testing.T) {
		svc, _ := newService()
		a, b := learner("Solo A", "UTC"), learner("Solo B", "UTC")
		grantedAt := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
		for _, id := range []string{a, b} {
			if _, err := svc.EnsureDailyGrant(ctx, id, grantedAt); err != nil {
				t.Fatal(err)
			}
		}
		if n, err := svc.ExpireLearnerDailyCredits(ctx, a, grantedAt.Add(24*time.Hour)); err != nil || n != 1 {
			t.Fatalf("expired %d, err %v, want 1", n, err)
		}
		if expired(a) != 1 || expired(b) != 0 {
			t.Fatalf("a=%d b=%d, want 1 and 0", expired(a), expired(b))
		}
	})

	t.Run("outbox events are delivered once by two workers and an empty outbox is a no-op", func(t *testing.T) {
		first, firstEvents := newService()
		second, secondEvents := newService()
		if err := first.DispatchOutbox(ctx); err != nil {
			t.Fatalf("empty outbox: %v", err)
		}
		id := learner("Outbox", "UTC")
		for i := range 25 {
			if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_outbox(event_key,learner_id,owner_reference,event,properties) VALUES($1,$2,'ref','credits_insufficient','{}')`, fmt.Sprintf("outbox-once-%d", i), id); err != nil {
				t.Fatal(err)
			}
		}
		var wg sync.WaitGroup
		for _, svc := range []*credits.Service{first, second, first, second} {
			wg.Go(func() {
				if err := svc.DispatchOutbox(ctx); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Wait()
		got := len(firstEvents.GetEvents("credits_insufficient")) + len(secondEvents.GetEvents("credits_insufficient"))
		if got != 25 {
			t.Fatalf("delivered %d events, want exactly 25", got)
		}
		var pending int
		if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_outbox WHERE dispatched_at IS NULL").Scan(&pending); err != nil || pending != 0 {
			t.Fatalf("pending %d, err %v", pending, err)
		}
	})

	t.Run("the due predicates are served by their indexes", func(t *testing.T) {
		if _, err := db.Pool.Exec(ctx, `INSERT INTO learners(display_name) SELECT 'bulk '||g FROM generate_series(1,60) g`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance)
 SELECT l.id,40,'daily_grant','bulk:'||l.id||':'||d,'v',('{"bucket":"daily_free","date":"2026-10-'||lpad(d::text,2,'0')||'"}')::jsonb
 FROM (SELECT id FROM learners WHERE display_name LIKE 'bulk %') l, generate_series(1,28) d`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_outbox(event_key,learner_id,owner_reference,event,properties,dispatched_at)
 SELECT 'bulk-'||g,NULL,'ref','x','{}',now() FROM generate_series(1,3000) g`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, "ANALYZE credit_entries; ANALYZE credit_outbox"); err != nil {
			t.Fatal(err)
		}
		cases := []struct{ name, index, query string }{
			{"outbox poll", "credit_outbox_pending", `SELECT event_key FROM credit_outbox WHERE dispatched_at IS NULL ORDER BY created_at LIMIT 100`},
			{"recent grants", "credit_entries_daily_grant_date", `SELECT DISTINCT learner_id FROM credit_entries WHERE kind='daily_grant' AND learner_id IS NOT NULL AND provenance->>'date'>='2026-10-26'`},
			{"learner day sum", "credit_entries_learner_date", `SELECT sum(amount) FROM credit_entries WHERE learner_id=(SELECT id FROM learners LIMIT 1) AND provenance->>'date'='2026-10-05'`},
			{"owner reference", "credit_entries_owner_reference", `SELECT sum(amount) FROM credit_entries WHERE owner_reference='abc'`},
			{"orphaned holds", "credit_jobs_orphans", `SELECT DISTINCT owner_reference FROM credit_jobs WHERE learner_id IS NULL AND state IN ('reserved','running')`},
		}
		for _, tc := range cases {
			tx, err := db.Pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			// Tiny tables prefer a sequential scan; forbid it to prove the index is usable for the predicate.
			if _, err = tx.Exec(ctx, "SET LOCAL enable_seqscan=off"); err != nil {
				t.Fatal(err)
			}
			rows, err := tx.Query(ctx, "EXPLAIN "+tc.query)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			var plan strings.Builder
			for rows.Next() {
				var line string
				if err = rows.Scan(&line); err != nil {
					t.Fatal(err)
				}
				plan.WriteString(line + "\n")
			}
			rows.Close()
			_ = tx.Rollback(ctx)
			if !strings.Contains(plan.String(), tc.index) {
				t.Errorf("%s does not use %s:\n%s", tc.name, tc.index, plan.String())
			}
		}
	})
}
