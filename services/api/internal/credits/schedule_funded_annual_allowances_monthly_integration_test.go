//go:build integration

package credits_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestScheduleFundedAnnualAllowancesMonthly(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("annual"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	cfg := cat.Config()
	plan := cfg.Plans[0]
	for _, refund := range []bool{false, true} {
		t.Run(fmt.Sprint(refund), func(t *testing.T) {
			learner, e := db.Queries.CreateLearner(ctx, "Annual subscriber")
			if e != nil {
				t.Fatal(e)
			}
			owner := uuid.UUID(learner.ID.Bytes).String()
			if _, e = db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1,false)", owner); e != nil {
				t.Fatal(e)
			}
			provider := &monthlyProvider{packProvider: packProvider{sessions: map[string]credits.PaymentSession{}}}
			events := new(packEvents)
			service := func() *credits.Service {
				s := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
				if e := s.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: "test-only-webhook-secret"}); e != nil {
					t.Fatal(e)
				}
				return s
			}
			svc := service()
			order, e := svc.CreateCheckout(ctx, owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: plan.Yearly.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"})
			if e != nil {
				t.Fatal(e)
			}
			if order.Illustrative {
				t.Fatal("annual checkout is illustrative")
			}
			start := time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC)
			end := time.Date(2025, 1, 31, 12, 0, 0, 0, time.UTC)
			provider.subscription = credits.ProviderSubscription{ID: "sub_" + order.ID, OrderID: order.ID, OwnerReference: accounts.LearnerReference(owner), CustomerID: "cus_" + accounts.LearnerReference(owner), PriceID: cfg.ProviderMappings("development")[plan.Yearly.SKU], Status: "active", Interval: "year", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
			provider.invoice = credits.SubscriptionInvoice{ID: "in_" + order.ID, SubscriptionID: provider.subscription.ID, OrderID: order.ID, OwnerReference: provider.subscription.OwnerReference, CustomerID: provider.subscription.CustomerID, SKU: plan.Yearly.SKU, PriceID: provider.subscription.PriceID, Currency: plan.Yearly.Currency, AmountMinor: plan.Yearly.AmountMinor, AmountPaid: plan.Yearly.AmountMinor, Status: "paid", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1, StripePaid: true}
			notify := func(id, kind, object string) {
				raw := []byte(fmt.Sprintf(`{"id":%q,"type":%q,"livemode":false,"data":{"object":{"id":%q}}}`, id+order.ID, kind, object))
				r := httptest.NewRequestWithContext(ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
				r.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
				w := httptest.NewRecorder()
				svc.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatal(w.Body.String())
				}
				if e := svc.ProcessPayments(ctx); e != nil {
					t.Fatal(e)
				}
			}
			count := func(want int) {
				var n int
				if e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='subscription_grant'", owner).Scan(&n); e != nil || n != want {
					t.Fatalf("grants=%d want=%d err=%v", n, want, e)
				}
			}
			notify("evt_fund_", "invoice.paid", provider.invoice.ID)
			count(1)
			notify("evt_alias_", "invoice.payment_succeeded", provider.invoice.ID)
			count(1)
			var due time.Time
			if e = db.Pool.QueryRow(ctx, "SELECT due_at FROM credit_subscription_schedule WHERE period_key=(SELECT period_key FROM credit_subscription_terms WHERE subscription_id=$1) AND month_index=1", provider.subscription.ID).Scan(&due); e != nil || !due.Equal(time.Date(2024, 2, 29, 12, 0, 0, 0, time.UTC)) {
				t.Fatalf("leap clamp %s %v", due, e)
			}
			provider.subscription.CancelAtPeriodEnd = true
			notify("evt_cancel_", "customer.subscription.updated", provider.subscription.ID)
			svc = service() // restart loses no schedule
			at := time.Date(2024, 4, 30, 12, 0, 0, 0, time.UTC)
			// The outbox is part of the grant transaction; a failure must leave no grant.
			if _, e = db.Pool.Exec(ctx, `CREATE FUNCTION fixture_reject_annual() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event='subscription_grant_issued' THEN RAISE EXCEPTION 'outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER fixture_reject_annual BEFORE INSERT ON credit_outbox FOR EACH ROW EXECUTE FUNCTION fixture_reject_annual()`); e != nil {
				t.Fatal(e)
			}
			if _, e = svc.IssueAnnualAllowances(ctx, at); e == nil {
				t.Fatal("outbox failure did not roll back")
			}
			count(1)
			if _, e = db.Pool.Exec(ctx, "DROP TRIGGER fixture_reject_annual ON credit_outbox; DROP FUNCTION fixture_reject_annual()"); e != nil {
				t.Fatal(e)
			}
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() {
					if _, e := svc.IssueAnnualAllowances(ctx, at); e != nil {
						t.Error(e)
					}
				})
			}
			wg.Wait()
			count(4)
			if refund {
				provider.invoice.Refunds = []credits.ProviderRefund{{ID: "re_annual_full", AmountMinor: plan.Yearly.AmountMinor, Currency: plan.Yearly.Currency, Status: "succeeded"}}
				notify("evt_refund_", "invoice.updated", provider.invoice.ID)
				notify("evt_refund_alias_", "invoice.updated", provider.invoice.ID)
			}
			if _, e = svc.IssueAnnualAllowances(ctx, end.AddDate(1, 0, 0)); e != nil {
				t.Fatal(e)
			}
			want := 12
			if refund {
				want = 4
			}
			count(want)
			if _, e = svc.IssueAnnualAllowances(ctx, end.AddDate(2, 0, 0)); e != nil {
				t.Fatal(e)
			}
			count(want)
			wallet, e := svc.GetWalletAt(ctx, owner, start.Add(time.Hour))
			if e != nil || wallet.Plan == nil || wallet.Plan.Active == refund || wallet.Subscription != int64(want*plan.MonthlyCredits)*int64(map[bool]int{false: 1, true: 0}[refund]) {
				t.Fatalf("wallet=%+v %v", wallet, e)
			}
			var n int
			if e = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_outbox WHERE learner_id=$1 AND event='subscription_grant_issued'", owner).Scan(&n); e != nil || n != want {
				t.Fatalf("deduped events %d %v", n, e)
			}
			if e = svc.DispatchOutbox(ctx); e != nil {
				t.Fatal(e)
			}
			if len(events.names) == 0 {
				t.Fatal("no analytics delivered")
			}
			if e = svc.DispatchOutbox(ctx); e != nil {
				t.Fatal(e)
			}
			// Financial schedules/revocations cannot be mutated.
			for _, table := range []string{"credit_subscription_schedule", "credit_subscription_revocations", "credit_subscription_cancellations"} {
				for _, verb := range []string{"DELETE FROM ", "TRUNCATE "} {
					if _, e = db.Pool.Exec(ctx, verb+table); e == nil {
						t.Fatal("mutable history", table)
					}
				}
			}
		})
	}
	riverLearner, e := db.Queries.CreateLearner(ctx, "River annual subscriber")
	if e != nil {
		t.Fatal(e)
	}
	riverOwner := uuid.UUID(riverLearner.ID.Bytes).String()
	provider := &monthlyProvider{packProvider: packProvider{sessions: map[string]credits.PaymentSession{}}}
	riverService := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
	if e = riverService.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: "test-only-webhook-secret"}); e != nil {
		t.Fatal(e)
	}
	order, e := riverService.CreateCheckout(ctx, riverOwner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: plan.Yearly.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"})
	if e != nil {
		t.Fatal(e)
	}
	start := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	end := start.AddDate(1, 0, 0)
	provider.subscription = credits.ProviderSubscription{ID: "sub_" + order.ID, OrderID: order.ID, OwnerReference: accounts.LearnerReference(riverOwner), CustomerID: "cus_" + accounts.LearnerReference(riverOwner), PriceID: cfg.ProviderMappings("development")[plan.Yearly.SKU], Status: "active", Interval: "year", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
	provider.invoice = credits.SubscriptionInvoice{ID: "in_" + order.ID, SubscriptionID: provider.subscription.ID, OrderID: order.ID, OwnerReference: provider.subscription.OwnerReference, CustomerID: provider.subscription.CustomerID, SKU: plan.Yearly.SKU, PriceID: provider.subscription.PriceID, Currency: plan.Yearly.Currency, AmountMinor: plan.Yearly.AmountMinor, AmountPaid: plan.Yearly.AmountMinor, Status: "paid", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1, StripePaid: true}
	raw := []byte(fmt.Sprintf(`{"id":%q,"type":"invoice.paid","livemode":false,"data":{"object":{"id":%q}}}`, "evt_river_"+order.ID, provider.invoice.ID))
	request := httptest.NewRequestWithContext(ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
	request.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
	response := httptest.NewRecorder()
	riverService.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	if e = riverService.ProcessPayments(ctx); e != nil {
		t.Fatal(e)
	}
	// The real River worker catches up persisted schedules on start.
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
	if err = worker.Migrate(ctx, db.Pool, logger); err != nil {
		t.Fatal(err)
	}
	queue, err := worker.NewWithCredits(db.Pool, logger, telemetry.New(logger), svc)
	if err != nil {
		t.Fatal(err)
	}
	if err = queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := queue.Stop(stop); e != nil {
			t.Error(e)
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM river_job WHERE kind='annual_subscription_allowances' AND state='completed'").Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			var grants int
			if e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='subscription_grant'", riverOwner).Scan(&grants); e != nil || grants != 12 {
				t.Fatalf("River did not catch up actual funded months %d %v", grants, e)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("annual River job not run")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
