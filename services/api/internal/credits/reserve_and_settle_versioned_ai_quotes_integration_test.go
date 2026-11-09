//go:build integration

package credits_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestVersionedReservationTransactions(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("quotes"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	learner, err := database.Queries.CreateLearner(ctx, "Quote learner")
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.UUID(learner.ID.Bytes).String()
	catalogue, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	events := &testAnalyticsEvents{}
	service := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, events)
	if _, err = service.EnsureDailyGrant(ctx, owner, time.Now()); err != nil {
		t.Fatal(err)
	}
	quotes := pricing.NewQuotes(database.Pool)
	quote, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "quick_note"})
	if err != nil {
		t.Fatal(err)
	}
	if quote.ExpiresAt.Sub(quote.CreatedAt) != 5*time.Minute {
		t.Fatalf("TTL: %s", quote.ExpiresAt.Sub(quote.CreatedAt))
	}
	if err = quotes.Accept(ctx, owner, quote.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	jobs := make(chan credits.Reservation, 8)
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { job, e := service.Reserve(ctx, owner, quote.ID); jobs <- job; errs <- e })
	}
	wg.Wait()
	close(jobs)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var job credits.Reservation
	for next := range jobs {
		if job.ID != "" && job.ID != next.ID {
			t.Fatal("duplicate job")
		}
		job = next
	}
	other, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "quick_note"})
	if err != nil {
		t.Fatal(err)
	}
	if err = quotes.Accept(ctx, owner, other.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Reserve(ctx, owner, other.ID); !errors.Is(err, credits.ErrConcurrency) {
		t.Fatalf("concurrency allowed: %v", err)
	}
	// Untrusted HTTP callers have no route to submit usage or prices.
	usage := []*enginev1.Usage{{Provider: quote.Rate.Provider, Model: quote.Rate.Model, InputTokens: 120, OutputTokens: 20, Attempt: 1, Component: "content", Delivered: true}}
	settled, err := service.Settle(ctx, owner, job.ID, usage)
	if err != nil {
		t.Fatal(err)
	}
	if settled.ContentSpent != 2 || settled.PricingVersion != quote.PricingVersion {
		t.Fatalf("settled: %+v", settled)
	}
	if _, err = service.Settle(ctx, owner, job.ID, usage); err != nil {
		t.Fatal(err)
	}
	wallet, err := service.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Reserved != 0 || wallet.Available != int64(catalogue.Config().DailyFree.Credits)-2 {
		t.Fatalf("wallet: %+v", wallet)
	}
	var spends, outbox int
	if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='spend'", owner).Scan(&spends); err != nil {
		t.Fatal(err)
	}
	if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_outbox WHERE event='credits_settled'").Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if spends != 1 || outbox != 1 {
		t.Fatalf("spends=%d outbox=%d", spends, outbox)
	}
	// Operational counters do not depend on analytics or Redis.
	var held, spent int64
	if err = database.Pool.QueryRow(ctx, "SELECT sum(held_nanos),sum(spent_nanos) FROM credit_cost_counters").Scan(&held, &spent); err != nil {
		t.Fatal(err)
	}
	if held != 0 || spent <= 0 {
		t.Fatalf("cost counters: %d %d", held, spent)
	}
	job2, err := service.Reserve(ctx, owner, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Settle(ctx, owner, job2.ID, nil); err != nil {
		t.Fatal(err)
	}
	t.Run("distinct quotes cannot overspend paid source", func(t *testing.T) {
		var paidOwner string
		if err = database.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('paid race') RETURNING id::text").Scan(&paidOwner); err != nil {
			t.Fatal(err)
		}
		if _, err = database.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance) VALUES($1,24,'purchase','race-purchase',$2,'{}'),($1,1,'subscription_grant','race-plan',$2,'{"plan_id":"pro","bucket":"subscription"}')`, paidOwner, catalogue.Config().Version); err != nil {
			t.Fatal(err)
		}
		a := acceptedQuote(t, quotes, paidOwner, "preview")
		b := acceptedQuote(t, quotes, paidOwner, "preview")
		outcomes := make(chan error, 2)
		var group sync.WaitGroup
		for _, q := range []pricing.Quote{a, b} {
			group.Go(func() { _, e := service.Reserve(ctx, paidOwner, q.ID); outcomes <- e })
		}
		group.Wait()
		close(outcomes)
		successes, insufficient := 0, 0
		for e := range outcomes {
			if e == nil {
				successes++
			} else if errors.Is(e, credits.ErrInsufficient) {
				insufficient++
			} else {
				t.Fatal(e)
			}
		}
		if successes != 1 || insufficient != 1 {
			t.Fatalf("race: success=%d insufficient=%d", successes, insufficient)
		}
		var allocated int64
		if err = database.Pool.QueryRow(ctx, "SELECT sum(a.amount) FROM credit_allocations a JOIN credit_jobs j ON j.id=a.job_id WHERE j.learner_id=$1", paidOwner).Scan(&allocated); err != nil || allocated != 25 {
			t.Fatalf("allocations=%d err=%v", allocated, err)
		}
	})
	t.Run("caps remain authoritative without analytics or Redis", func(t *testing.T) {
		for _, scope := range []string{"free", "paid", "platform"} {
			var capOwner string
			if err = database.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('cap fixture') RETURNING id::text").Scan(&capOwner); err != nil {
				t.Fatal(err)
			}
			q := acceptedQuote(t, quotes, capOwner, "quick_note")
			if scope == "paid" {
				if _, err = database.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,400,'purchase','cap-fixture',$2)`, capOwner, q.PricingVersion); err != nil {
					t.Fatal(err)
				}
			}
			counterScope, ref := "learner", accounts.LearnerReference(capOwner)
			capNanos := q.Bounds["free_day_nanos"]
			if scope == "paid" {
				capNanos = q.Bounds["paid_day_nanos"]
			}
			if scope == "platform" {
				counterScope = "platform"
				ref = "platform"
				capNanos = q.Bounds["platform_day_nanos"]
			}
			day := time.Now().In(time.FixedZone("fixture", 7*60*60)).Format(time.DateOnly)
			if scope == "platform" {
				day = time.Now().UTC().Format(time.DateOnly)
			}
			if _, err = database.Pool.Exec(ctx, `INSERT INTO credit_cost_counters(scope,owner_reference,day,spent_nanos) VALUES($1,$2,$3::date,$4) ON CONFLICT(scope,owner_reference,day) DO UPDATE SET spent_nanos=$4`, counterScope, ref, day, capNanos); err != nil {
				t.Fatal(err)
			}
			noAnalytics := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, nil)
			if _, e := noAnalytics.Reserve(ctx, capOwner, q.ID); !errors.Is(e, credits.ErrBudget) {
				t.Fatalf("%s cap: %v", scope, e)
			}
			if scope == "platform" {
				if _, err = database.Pool.Exec(ctx, `UPDATE credit_cost_counters SET spent_nanos=0 WHERE scope='platform'`); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
	t.Run("midnight failed release cannot revive yesterday", func(t *testing.T) {
		var midnightOwner string
		if err = database.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('midnight fixture') RETURNING id::text").Scan(&midnightOwner); err != nil {
			t.Fatal(err)
		}
		if _, err = database.Pool.Exec(ctx, `INSERT INTO credit_timezones(learner_id,time_zone,effective_at) VALUES($1,'Etc/GMT+12',now()-interval '1 hour')`, midnightOwner); err != nil {
			t.Fatal(err)
		}
		q := acceptedQuote(t, quotes, midnightOwner, "quick_note")
		j, e := service.Reserve(ctx, midnightOwner, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		// Move the fixture's local clock across midnight without sleeping or mutating financial rows.
		if _, err = database.Pool.Exec(ctx, `INSERT INTO credit_timezones(learner_id,time_zone,effective_at) VALUES($1,'Pacific/Kiritimati',now())`, midnightOwner); err != nil {
			t.Fatal(err)
		}
		if _, err = service.ExpireDailyCredits(ctx, time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err = service.Settle(ctx, midnightOwner, j.ID, nil); err != nil {
			t.Fatal(err)
		}
		wallet, e := service.GetWallet(ctx, midnightOwner)
		if e != nil || wallet.Available != 0 || wallet.Reserved != 0 {
			t.Fatalf("midnight wallet=%+v err=%v", wallet, e)
		}
		var balance int64
		if err = database.Pool.QueryRow(ctx, "SELECT sum(amount) FROM credit_entries WHERE learner_id=$1", midnightOwner).Scan(&balance); err != nil || balance != 0 {
			t.Fatalf("midnight sum=%d %v", balance, err)
		}
	})
	t.Run("failed audio independent and artifact replay free", func(t *testing.T) {
		var componentOwner string
		if err = database.Pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES('component fixture') RETURNING id::text").Scan(&componentOwner); err != nil {
			t.Fatal(err)
		}
		if _, err = database.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,400,'purchase','component-fixture',$2)`, componentOwner, catalogue.Config().Version); err != nil {
			t.Fatal(err)
		}
		q, e := quotes.Create(ctx, componentOwner, pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: 100})
		if e != nil {
			t.Fatal(e)
		}
		if e = quotes.Accept(ctx, componentOwner, q.ID); e != nil {
			t.Fatal(e)
		}
		j, e := service.Reserve(ctx, componentOwner, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		reports := []*enginev1.Usage{{Provider: q.Rate.Provider, Model: q.Rate.Model, InputTokens: 100, OutputTokens: 20, Attempt: 1, Component: "content", Delivered: true, ArtifactId: "same-content"}, {Provider: "elevenlabs", Model: q.Policy.AudioModel, NarrationCharacters: 100, Attempt: 1, Component: "audio", Delivered: false}}
		result, e := service.Settle(ctx, componentOwner, j.ID, reports)
		if e != nil || result.ContentSpent != 140 || result.AudioSpent != 0 || result.CostNanos <= 4000000 {
			t.Fatalf("component result=%+v %v", result, e)
		}
		second := acceptedQuote(t, quotes, componentOwner, "lesson_content")
		replay, e := service.Reserve(ctx, componentOwner, second.ID)
		if e != nil {
			t.Fatal(e)
		}
		result, e = service.Settle(ctx, componentOwner, replay.ID, reports[:1])
		if e != nil || result.ContentSpent != 0 {
			t.Fatalf("replay=%+v %v", result, e)
		}
	})
	t.Run("original quote remains pinned after activation", func(t *testing.T) {
		q := acceptedQuote(t, quotes, owner, "quick_note")
		j, e := service.Reserve(ctx, owner, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, err = database.Pool.Exec(ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT 'e7-03-flip',now(),true,jsonb_set(jsonb_set(jsonb_set(config,'{version}','"e7-03-flip"'),'{effective_from}',to_jsonb(now())),'{actions,quick_note,credits}','3') FROM pricing_versions WHERE version=$1`, q.PricingVersion); err != nil {
			t.Fatal(err)
		}
		if _, err = database.Pool.Exec(ctx, `INSERT INTO pricing_quote_policies(version,policy) SELECT 'e7-03-flip',policy FROM pricing_quote_policies WHERE version=$1`, q.PricingVersion); err != nil {
			t.Fatal(err)
		}
		next := acceptedQuote(t, quotes, owner, "quick_note")
		if next.ContentCeiling != 3 || next.PricingVersion == q.PricingVersion {
			t.Fatal("new config not activated")
		}
		result, e := service.Settle(ctx, owner, j.ID, usage)
		if e != nil || result.ContentSpent != 2 || result.PricingVersion != q.PricingVersion {
			t.Fatalf("pinning=%+v %v", result, e)
		}
	})

	t.Run("bad usage and abandoned jobs release funds", func(t *testing.T) {
		q := acceptedQuote(t, quotes, owner, "quick_note")
		j, e := service.Reserve(ctx, owner, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = service.Claim(ctx, owner, j.ID); e != nil {
			t.Fatal(e)
		}
		result, e := service.Settle(ctx, owner, j.ID, []*enginev1.Usage{{Provider: "unknown", Model: "unknown", Attempt: 1, Component: "content", Delivered: true}})
		if !errors.Is(e, credits.ErrUsage) || result.State != "released" || result.CostNanos <= 0 {
			t.Fatalf("invalid report=%+v %v", result, e)
		}
		q = acceptedQuote(t, quotes, owner, "quick_note")
		j, e = service.Reserve(ctx, owner, q.ID)
		if e != nil {
			t.Fatal(e)
		}
		if e = service.Claim(ctx, owner, j.ID); e != nil {
			t.Fatal(e)
		}
		if _, e = database.Pool.Exec(ctx, "UPDATE credit_jobs SET deadline_at=now()-interval '1 second' WHERE id=$1", j.ID); e != nil {
			t.Fatal(e)
		}
		if e = service.ReapExpired(ctx); e != nil {
			t.Fatal(e)
		}
		result, e = service.Settle(ctx, owner, j.ID, nil)
		if e != nil || result.State != "released" || result.CostNanos <= 0 {
			t.Fatalf("abandoned=%+v %v", result, e)
		}
		wallet, e := service.GetWallet(ctx, owner)
		if e != nil || wallet.Reserved != 0 {
			t.Fatalf("stuck reservation: %+v %v", wallet, e)
		}
	})

	t.Run("outbox wrapper delivers once with UUID and opt-out", func(t *testing.T) {
		if _, err = database.Pool.Exec(ctx, `INSERT INTO learner_profiles(learner_id,analytics_opt_out,time_zone) VALUES($1,false,'Asia/Ho_Chi_Minh') ON CONFLICT(learner_id) DO UPDATE SET analytics_opt_out=false,time_zone='Asia/Ho_Chi_Minh'`, owner); err != nil {
			t.Fatal(err)
		}
		unaffordable := acceptedQuote(t, quotes, owner, "preview")
		if _, e := service.Reserve(ctx, owner, unaffordable.ID); !errors.Is(e, credits.ErrInsufficient) {
			t.Fatalf("expected insufficiency: %v", e)
		}
		type captured struct {
			Event      string         `json:"event"`
			DistinctID string         `json:"distinct_id"`
			Properties map[string]any `json:"properties"`
		}
		var capturedEvents []captured
		var captureMu sync.Mutex
		sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var batch struct {
				Batch []captured `json:"batch"`
			}
			if e := json.NewDecoder(r.Body).Decode(&batch); e != nil {
				t.Error(e)
				w.WriteHeader(400)
				return
			}
			captureMu.Lock()
			capturedEvents = append(capturedEvents, batch.Batch...)
			captureMu.Unlock()
			_, _ = w.Write([]byte(`{"status":"Ok"}`))
		}))
		defer sink.Close()
		wrapper, e := analytics.New("synthetic-credits-key", sink.URL)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = wrapper.Close() }()
		emitter := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, wrapper)
		if e = emitter.DispatchOutbox(ctx); e != nil {
			t.Fatal(e)
		}
		if e = emitter.DispatchOutbox(ctx); e != nil {
			t.Fatal(e)
		}
		var want int
		if e = database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_outbox WHERE learner_id=$1 AND dispatched_at IS NOT NULL", owner).Scan(&want); e != nil {
			t.Fatal(e)
		}
		if _, e = database.Pool.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1", owner); e != nil {
			t.Fatal(e)
		}
		optedOutQuote := acceptedQuote(t, quotes, owner, "quick_note")
		optedOutJob, e := emitter.Reserve(ctx, owner, optedOutQuote.ID)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = emitter.Settle(ctx, owner, optedOutJob.ID, nil); e != nil {
			t.Fatal(e)
		}
		if e = emitter.DispatchOutbox(ctx); e != nil {
			t.Fatal(e)
		}
		if e = wrapper.Close(); e != nil {
			t.Fatal(e)
		}
		captureMu.Lock()
		defer captureMu.Unlock()
		if len(capturedEvents) != want {
			t.Fatalf("delivered=%d want=%d", len(capturedEvents), want)
		}
		names := map[string]bool{}
		for _, event := range capturedEvents {
			names[event.Event] = true
			if event.DistinctID != owner || event.Properties["source"] != "api" || event.Properties["platform"] != "web" || event.Properties["app_version"] == nil {
				t.Fatalf("invalid event: %+v", event)
			}
			raw, _ := json.Marshal(event.Properties)
			for _, private := range []string{"Quote learner", "prompt", "completion", "note_text", "topic"} {
				if strings.Contains(string(raw), private) {
					t.Fatalf("private metadata: %s", raw)
				}
			}
		}
		for _, name := range []string{"credits_reserved", "credits_settled", "credits_released", "credits_insufficient", "credits_exhausted", "$ai_generation"} {
			if !names[name] {
				t.Fatalf("missing event %s", name)
			}
		}
	})

}

func acceptedQuote(t *testing.T, quotes *pricing.Quotes, owner, action string) pricing.Quote {
	t.Helper()
	q, err := quotes.Create(t.Context(), owner, pricing.QuoteInput{Action: action})
	if err != nil {
		t.Fatal(err)
	}
	if err = quotes.Accept(t.Context(), owner, q.ID); err != nil {
		t.Fatal(err)
	}
	return q
}
