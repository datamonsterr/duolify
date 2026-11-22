//go:build integration

package credits_test

import (
	"encoding/json"
	"errors"
	"fmt"
	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"net/http/httptest"
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
	learner := func(t *testing.T, zone string) string {
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
	check := func(t *testing.T, id string, at time.Time) credits.CheckInResponse {
		t.Helper()
		v, e := svc.CheckInAt(ctx, id, at)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	day := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	t.Run("free cap follows funding allocations", func(t *testing.T) {
		catalogue, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			t.Fatal(err)
		}
		financial := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, nil)
		for _, tc := range []struct {
			name                            string
			hold, credits                   int64
			checkIn, freeEligible, rejected bool
			wantHeld                        int64
		}{
			{"check-in replan", 500000000, 100, true, false, false, 0},
			{"free-only over cap", 90000000, 46, false, true, true, 0},
			{"mixed", 100000000, 100, true, true, false, 46000000},
		} {
			t.Run(tc.name, func(t *testing.T) {
				id := learner(t, "UTC")
				if tc.checkIn {
					check(t, id, time.Now())
				}
				q, err := pricing.NewQuotes(db.Pool).Create(ctx, id, pricing.QuoteInput{Action: "quick_note"})
				if err != nil {
					t.Fatal(err)
				}
				q.Action, q.Input.Action = "outline_replan", "outline_replan"
				q.ContentCeiling, q.ContentMinimum = tc.credits, tc.credits
				q.DailyFreeEligible = tc.freeEligible
				q.Bounds["input_tokens"], q.Bounds["output_tokens"], q.Bounds["max_attempts"] = 1, 1, 2
				q.Rate.Input, q.Rate.Output, q.Rate.CacheWrite, q.Rate.CacheRead = 0, tc.hold/2, 0, 0
				q.Policy.FallbackModels = nil
				raw, err := json.Marshal(q)
				if err != nil {
					t.Fatal(err)
				}
				var quoteID string
				if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1,$2,$3,$4,now(),now()+interval '5 minutes') RETURNING id::text`, id, accounts.LearnerReference(id), q.PricingVersion, raw).Scan(&quoteID); err != nil {
					t.Fatal(err)
				}
				if _, err = db.Pool.Exec(ctx, `INSERT INTO pricing_quote_consents(quote_id) VALUES($1)`, quoteID); err != nil {
					t.Fatal(err)
				}
				job, err := financial.Reserve(ctx, id, quoteID)
				if tc.rejected {
					if !errors.Is(err, credits.ErrBudget) {
						t.Fatalf("want spend cap, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var learnerHeld, totalHeld int64
				if err = db.Pool.QueryRow(ctx, `SELECT learner_held_nanos,held_nanos FROM credit_jobs WHERE id=$1`, job.ID).Scan(&learnerHeld, &totalHeld); err != nil {
					t.Fatal(err)
				}
				if learnerHeld != tc.wantHeld || totalHeld != tc.hold {
					t.Fatalf("learner=%d platform=%d", learnerHeld, totalHeld)
				}
				if tc.name == "mixed" {
					if err = financial.Claim(ctx, id, job.ID); err != nil {
						t.Fatal(err)
					}
					_, err = financial.Settle(ctx, id, job.ID, []*enginev1.Usage{{Component: "content", Provider: q.Rate.Provider, Model: q.Rate.Model, Attempt: 1, OutputTokens: 1, Delivered: true}})
					if err != nil {
						t.Fatal(err)
					}
					var spent, held int64
					if err = db.Pool.QueryRow(ctx, `SELECT spent_nanos,held_nanos FROM credit_cost_counters WHERE scope='learner' AND owner_reference=$1 AND day=CURRENT_DATE`, accounts.LearnerReference(id)).Scan(&spent, &held); err != nil {
						t.Fatal(err)
					}
					if spent != 23000000 || held != 0 {
						t.Fatalf("settled learner spent=%d held=%d", spent, held)
					}
					return
				}
				if _, err = financial.Release(ctx, id, job.ID); err != nil {
					t.Fatal(err)
				}
			})
		}
	})
	t.Run("20 concurrent replicas grant once", func(t *testing.T) {
		id := learner(t, "UTC")
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
		v := check(t, id, day.AddDate(0, 0, 1))
		if v.Granted != 150 || v.FirstTime || v.BankedCheckIn != 550 {
			t.Fatalf("next: %+v", v)
		}
	})
	t.Run("HTTP check-in balance matches the wallet total including Daily free", func(t *testing.T) {
		id := learner(t, "UTC")
		cat, err := pricing.Load(ctx, db.Queries)
		if err != nil {
			t.Fatal(err)
		}
		svc := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
		r := httptest.NewRequestWithContext(accounts.WithLearnerID(ctx, id), "POST", "/v1/me/check-in", nil)
		w := httptest.NewRecorder()
		svc.ServeHTTP(w, r)
		var got credits.CheckInResponse
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != 200 {
			t.Fatalf("status=%d body=%s err=%v", w.Code, w.Body.String(), err)
		}
		w = httptest.NewRecorder()
		svc.ServeHTTP(w, httptest.NewRequestWithContext(accounts.WithLearnerID(ctx, id), "GET", "/v1/me/credits", nil))
		var wallet credits.WalletResponse
		if err := json.Unmarshal(w.Body.Bytes(), &wallet); err != nil {
			t.Fatal(err)
		}
		if got.Granted != 400 || got.Balance != wallet.Available || got.Balance <= 400 {
			t.Fatalf("check-in balance=%d wallet available=%d granted=%d", got.Balance, wallet.Available, got.Granted)
		}
	})
	t.Run("date line and DST midnights", func(t *testing.T) {
		for _, zone := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago", "America/New_York"} {
			id := learner(t, zone)
			at := time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC)
			v := check(t, id, at)
			if got := check(t, id, v.NextCheckInAt.Add(-time.Nanosecond)); got.CheckedIn {
				t.Fatalf("early %s: %+v", zone, got)
			}
			if got := check(t, id, v.NextCheckInAt); got.Granted != 150 {
				t.Fatalf("midnight %s: %+v", zone, got)
			}
		}
	})
	t.Run("zone change cannot advance frozen window", func(t *testing.T) {
		id := learner(t, "Pacific/Pago_Pago")
		v := check(t, id, day)
		if _, e := db.Pool.Exec(ctx, "UPDATE learner_profiles SET time_zone='Pacific/Kiritimati' WHERE learner_id=$1", id); e != nil {
			t.Fatal(e)
		}
		if got := check(t, id, day.Add(time.Hour)); got.CheckedIn || !got.NextCheckInAt.Equal(v.NextCheckInAt) {
			t.Fatalf("zone farm: %+v", got)
		}
	})
	t.Run("cap includes reservations and bonus once", func(t *testing.T) {
		id := learner(t, "UTC")
		check(t, id, day)
		for i := 1; i <= 4; i++ {
			check(t, id, day.AddDate(0, 0, i))
		}
		// 1000 +49 through an attributable source correction is 1049.
		if _, e := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1,49,'refund','cap-fixture','{"bucket":"check_in"}')`, id); e != nil {
			t.Fatal(e)
		}
		if v := check(t, id, day.AddDate(0, 0, 5)); v.Granted != 1 || v.BankedCheckIn != 1050 {
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
		if v := check(t, id, day.AddDate(0, 0, 6)); v.Granted != 0 || !v.CheckedIn || v.FirstTime || v.BankedCheckIn != 1050 {
			t.Fatalf("cap: %+v", v)
		}
		if _, e = service.Release(ctx, id, job.ID); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("preview uses check-in and release restores source", func(t *testing.T) {
		id := learner(t, "UTC")
		check(t, id, time.Now())
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
		id := learner(t, "UTC")
		check(t, id, time.Now())
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
		id := learner(t, "UTC")
		check(t, id, time.Now())
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
	t.Run("day-one learner affords preview, outline and first lesson from check-in alone", func(t *testing.T) {
		id := learner(t, "UTC")
		check(t, id, time.Now())
		cat, e := pricing.Load(ctx, db.Queries)
		if e != nil {
			t.Fatal(e)
		}
		service := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
		quotes := pricing.NewQuotes(db.Pool)
		// Outline quotes need a stored brief and draft; its price is asserted from the active config instead.
		actions := cat.Config().Actions
		if total := actions["preview"].Credits + actions["outline"].Credits + actions["lesson_content"].Credits; total != 220 || total > 400 {
			t.Fatalf("preview+outline+lesson price %d, want 220 within the 400 day-one Credits", total)
		}
		for _, action := range []string{"preview", "lesson_content"} {
			q, e := quotes.Create(ctx, id, pricing.QuoteInput{Action: action})
			if e != nil {
				t.Fatal(action, e)
			}
			if e = quotes.Accept(ctx, id, q.ID); e != nil {
				t.Fatal(action, e)
			}
			job, e := service.Reserve(ctx, id, q.ID)
			if e != nil {
				t.Fatalf("%s must be funded by the 400 day-one Credits: %v", action, e)
			}
			// Free learners run one generation at a time; the hold is released between actions.
			w, e := service.GetWallet(ctx, id)
			if e != nil || w.Purchased != 0 || w.CheckIn != int64(400-actions[action].Credits) {
				t.Fatalf("%s held from check-in only: %+v %v", action, w, e)
			}
			if _, e = service.Release(ctx, id, job.ID); e != nil {
				t.Fatal(e)
			}
		}
	})
	t.Run("full first-time bonus counts toward subsequent cap", func(t *testing.T) {
		id := learner(t, "UTC")
		if _, e := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1,1049,'check_in_grant','bonus-cap-fixture','{"bucket":"check_in","date":"2026-11-23"}')`, id); e != nil {
			t.Fatal(e)
		}
		if v := check(t, id, day); v.Granted != 251 || !v.FirstTime || v.BankedCheckIn != 1300 {
			t.Fatalf("full bonus: %+v", v)
		}
		if v := check(t, id, day.AddDate(0, 0, 1)); v.Granted != 0 || v.FirstTime || v.BankedCheckIn != 1300 {
			t.Fatalf("subsequent cap: %+v", v)
		}
	})
	t.Run("outbox grants are idempotent and delivery respects opt-out", func(t *testing.T) {
		events := &testAnalyticsEvents{}
		cat, e := pricing.Load(ctx, db.Queries)
		if e != nil {
			t.Fatal(e)
		}
		service := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
		allowed, denied := learner(t, "UTC"), learner(t, "UTC")
		if _, e = db.Pool.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1", denied); e != nil {
			t.Fatal(e)
		}
		for _, id := range []string{allowed, allowed, denied} {
			if _, e = service.CheckInAt(ctx, id, day); e != nil {
				t.Fatal(e)
			}
		}
		if e = service.DispatchOutbox(ctx); e != nil {
			t.Fatal(e)
		}
		seen := 0
		for _, event := range events.GetEvents("check_in_credits_granted") {
			if event.LearnerID == denied {
				t.Fatal("opted-out event delivered")
			}
			if event.LearnerID == allowed {
				seen++
				if event.Properties["amount"] != float64(400) || event.Properties["first_time"] != true {
					t.Fatalf("event: %+v", event)
				}
			}
		}
		if seen != 1 {
			t.Fatalf("event replay: %d", seen)
		}
	})
}
