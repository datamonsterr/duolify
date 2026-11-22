//go:build integration

package credits_test

import (
	"fmt"
	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"sync"
	"testing"
	"time"
)

func TestDailyCheckInTransactions(t *testing.T) {
	ctx := t.Context()
	pg, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("attendance"), postgres.WithUsername("test"), postgres.WithPassword("test"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, pg)
	url, err := pg.ConnectionString(ctx, "sslmode=disable")
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
	svc := credits.NewService(db.Pool, nil)
	learner := func(zone string) string {
		t.Helper()
		l, e := db.Queries.CreateLearner(ctx, "Attendance learner")
		if e != nil {
			t.Fatal(e)
		}
		id := uuid.UUID(l.ID.Bytes).String()
		if _, e = db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,time_zone) VALUES($1,$2)", id, zone); e != nil {
			t.Fatal(e)
		}
		return id
	}
	check := func(id string, at time.Time) credits.CheckInResponse {
		t.Helper()
		v, e := svc.CheckInAt(ctx, id, at)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	day := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	t.Run("20 concurrent replicas grant once", func(t *testing.T) {
		id := learner("UTC")
		var wg sync.WaitGroup
		results := make(chan credits.CheckInResponse, 20)
		errs := make(chan error, 20)
		for range 20 {
			wg.Go(func() { v, e := credits.NewService(db.Pool, nil).CheckInAt(ctx, id, day); results <- v; errs <- e })
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		var sum int64
		var first int
		for v := range results {
			sum += v.Granted
			if v.FirstTime {
				first++
			}
		}
		if sum != 400 || first != 1 {
			t.Fatalf("granted=%d bonuses=%d", sum, first)
		}
		v := check(id, day.AddDate(0, 0, 1))
		if v.Granted != 150 || v.FirstTime || v.BankedCheckIn != 550 {
			t.Fatalf("next: %+v", v)
		}
	})
	t.Run("date line and DST midnights", func(t *testing.T) {
		for _, zone := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago", "America/New_York"} {
			id := learner(zone)
			at := time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC)
			v := check(id, at)
			if got := check(id, v.NextCheckInAt.Add(-time.Nanosecond)); got.CheckedIn {
				t.Fatalf("early %s: %+v", zone, got)
			}
			if got := check(id, v.NextCheckInAt); got.Granted != 150 {
				t.Fatalf("midnight %s: %+v", zone, got)
			}
		}
	})
	t.Run("zone change cannot advance frozen window", func(t *testing.T) {
		id := learner("Pacific/Pago_Pago")
		v := check(id, day)
		if _, e := db.Pool.Exec(ctx, "UPDATE learner_profiles SET time_zone='Pacific/Kiritimati' WHERE learner_id=$1", id); e != nil {
			t.Fatal(e)
		}
		if got := check(id, day.Add(time.Hour)); got.CheckedIn || !got.NextCheckInAt.Equal(v.NextCheckInAt) {
			t.Fatalf("zone farm: %+v", got)
		}
	})
	t.Run("cap includes reservations and bonus once", func(t *testing.T) {
		id := learner("UTC")
		check(id, day)
		for i := 1; i <= 4; i++ {
			check(id, day.AddDate(0, 0, i))
		}
		// 1000 +49 through an attributable source correction is 1049.
		if _, e := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1,49,'refund','cap-fixture','{"bucket":"check_in"}')`, id); e != nil {
			t.Fatal(e)
		}
		if v := check(id, day.AddDate(0, 0, 5)); v.Granted != 1 || v.BankedCheckIn != 1050 {
			t.Fatalf("partial: %+v", v)
		}
		cat, e := pricing.Load(ctx, db.Queries)
		if e != nil {
			t.Fatal(e)
		}
		service := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
		quotes := pricing.NewQuotes(db.Pool)
		q, e := quotes.Create(ctx, id, pricing.QuoteInput{Action: "preview"})
		if e != nil {
			t.Fatal(e)
		}
		if e = quotes.Accept(ctx, id, q.ID); e != nil {
			t.Fatal(e)
		}
		job, e := service.Reserve(ctx, id, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if v := check(id, day.AddDate(0, 0, 6)); v.Granted != 0 || !v.CheckedIn || v.FirstTime || v.BankedCheckIn != 1050 {
			t.Fatalf("cap: %+v", v)
		}
		if _, e = service.Release(ctx, id, job.ID); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("preview uses check-in and release restores source", func(t *testing.T) {
		id := learner("UTC")
		check(id, time.Now())
		cat, e := pricing.Load(ctx, db.Queries)
		if e != nil {
			t.Fatal(e)
		}
		service := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
		if _, e = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1,400,'purchase','paid-fixture','{"bucket":"purchase"}')`, id); e != nil {
			t.Fatal(e)
		}
		q, e := pricing.NewQuotes(db.Pool).Create(ctx, id, pricing.QuoteInput{Action: "preview"})
		if e != nil {
			t.Fatal(e)
		}
		if e = pricing.NewQuotes(db.Pool).Accept(ctx, id, q.ID); e != nil {
			t.Fatal(e)
		}
		job, e := service.Reserve(ctx, id, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		w, e := service.GetWallet(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if w.CheckIn != 375 || w.Purchased != 400 || w.DailyFree.Available != 46 {
			t.Fatalf("spend order: %+v", w)
		}
		if _, e = service.Release(ctx, id, job.ID); e != nil {
			t.Fatal(e)
		}
		w, e = service.GetWallet(ctx, id)
		if e != nil || w.CheckIn != 400 || w.Purchased != 400 {
			t.Fatalf("release: %+v %v", w, e)
		}
	})
	t.Run("eligible daily allowance precedes check-in and settlement preserves sources", func(t *testing.T) {
		id := learner("UTC")
		check(id, time.Now())
		cat, e := pricing.Load(ctx, db.Queries)
		if e != nil {
			t.Fatal(e)
		}
		service := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
		grant, e := service.EnsureDailyGrant(ctx, id, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		if _, e = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1,-45,'spend','daily-fixture',jsonb_build_object('bucket','daily_free','date',$2::text,'source_id',$3::text)),($1,400,'purchase','mixed-purchase','{"bucket":"purchase"}')`, id, time.Now().UTC().Format(time.DateOnly), fmt.Sprint(grant.ID)); e != nil {
			t.Fatal(e)
		}
		quotes := pricing.NewQuotes(db.Pool)
		q, e := quotes.Create(ctx, id, pricing.QuoteInput{Action: "tutor_long"})
		if e != nil {
			t.Fatal(e)
		}
		if e = quotes.Accept(ctx, id, q.ID); e != nil {
			t.Fatal(e)
		}
		job, e := service.Reserve(ctx, id, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		w, e := service.GetWallet(ctx, id)
		if e != nil || w.DailyFree.Available != 0 || w.CheckIn != 393 || w.Purchased != 400 {
			t.Fatalf("mixed reserve: %+v %v", w, e)
		}
		result, e := service.Settle(ctx, id, job.ID, []*enginev1.Usage{{Component: "content", Provider: q.Rate.Provider, Model: q.Rate.Model, Attempt: 1, Delivered: true, InputTokens: 1, OutputTokens: 1}})
		if e != nil || result.ContentSpent != 8 {
			t.Fatalf("settle: %+v %v", result, e)
		}
		w, e = service.GetWallet(ctx, id)
		if e != nil || w.CheckIn != 393 || w.Purchased != 400 || w.Reserved != 0 {
			t.Fatalf("settled sources: %+v %v", w, e)
		}
		// A correction follows the spend's source_id; no purchased balance is minted.
		if _, e = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) SELECT learner_id,-amount,'refund',reference||':correction',provenance FROM credit_entries WHERE learner_id=$1 AND kind='spend' AND provenance->>'bucket'='check_in'`, id); e != nil {
			t.Fatal(e)
		}
		w, e = service.GetWallet(ctx, id)
		if e != nil || w.CheckIn != 400 || w.Purchased != 400 {
			t.Fatalf("refund source: %+v %v", w, e)
		}
	})
	t.Run("lesson generation can spend check-in without purchased Credits", func(t *testing.T) {
		id := learner("UTC")
		check(id, time.Now())
		cat, e := pricing.Load(ctx, db.Queries)
		if e != nil {
			t.Fatal(e)
		}
		service := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
		quotes := pricing.NewQuotes(db.Pool)
		q, e := quotes.Create(ctx, id, pricing.QuoteInput{Action: "lesson_content"})
		if e != nil {
			t.Fatal(e)
		}
		if e = quotes.Accept(ctx, id, q.ID); e != nil {
			t.Fatal(e)
		}
		job, e := service.Reserve(ctx, id, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		w, e := service.GetWallet(ctx, id)
		if e != nil || w.CheckIn != 260 || w.Purchased != 0 || w.DailyFree.Available != 46 {
			t.Fatalf("lesson source: %+v %v", w, e)
		}
		if _, e = service.Release(ctx, id, job.ID); e != nil {
			t.Fatal(e)
		}
	})
}
