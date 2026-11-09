//go:build integration

package credits_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
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

type monthlyProvider struct {
	packProvider
	invoice      credits.SubscriptionInvoice
	subscription credits.ProviderSubscription
}

func (p *monthlyProvider) CreateSession(ctx context.Context, r credits.PaymentRequest) (credits.PaymentSession, error) {
	s, err := p.packProvider.CreateSession(ctx, r)
	s.Mode = r.Mode
	if s.Mode == "subscription" {
		s.SubscriptionID = "sub_" + r.OrderID
		s.CustomerID = "cus_" + r.OwnerReference
	}
	p.mu.Lock()
	p.sessions[s.ID] = s
	p.mu.Unlock()
	return s, err
}
func (p *monthlyProvider) RetrieveInvoice(context.Context, string) (credits.SubscriptionInvoice, error) {
	if p.unavailable {
		return p.invoice, fmt.Errorf("fixture outage")
	}
	return p.invoice, nil
}
func (p *monthlyProvider) RetrieveSubscription(context.Context, string) (credits.ProviderSubscription, error) {
	return p.subscription, nil
}

func TestActivateMonthlySubscriptionsFromPaidInvoices(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("monthly"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	redisContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForLog("Ready to accept connections")}, Started: true})
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, redisContainer)
	endpoint, err := redisContainer.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	redisClient, err := cache.New("redis://" + endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = redisClient.Close() }()
	plan := cfg.Plans[0]
	for _, invoiceFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(invoiceFirst), func(t *testing.T) {
			learner, e := db.Queries.CreateLearner(ctx, "Subscriber")
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
				s := credits.NewServiceWithCatalogue(db.Pool, redisClient, cat, events)
				if e = s.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: "test-only-webhook-secret"}); e != nil {
					t.Fatal(e)
				}
				return s
			}
			svc := service()
			before, e := svc.GetWallet(ctx, owner)
			if e != nil || before.Plan != nil || before.Subscription != 0 {
				t.Fatalf("initial cached wallet %+v %v", before, e)
			}
			in := credits.CheckoutInput{RequestID: uuid.NewString(), SKU: plan.Monthly.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"}
			order, e := svc.CreateCheckout(ctx, owner, in)
			if e != nil {
				t.Fatal(e)
			}
			if order.Illustrative {
				t.Fatal("monthly checkout is still illustrative")
			}
			if _, e = svc.CreateCheckout(ctx, owner, in); e != nil {
				t.Fatalf("idempotent retry: %v", e)
			}
			overlap := in
			overlap.RequestID = uuid.NewString()
			if _, e = svc.CreateCheckout(ctx, owner, overlap); e == nil {
				t.Fatal("overlapping pending subscription allowed")
			}
			start := time.Now().UTC().Truncate(time.Second).Add(-time.Minute)
			start = time.Date(start.Year(), start.Month(), 1, start.Hour(), start.Minute(), start.Second(), 0, time.UTC)
			end := start.AddDate(0, 1, 0)
			provider.subscription = credits.ProviderSubscription{ID: "sub_" + order.ID, OrderID: order.ID, OwnerReference: accounts.LearnerReference(owner), CustomerID: "cus_" + accounts.LearnerReference(owner), PriceID: cfg.ProviderMappings("development")[plan.Monthly.SKU], Status: "active", Interval: "month", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
			provider.invoice = credits.SubscriptionInvoice{ID: "in_" + order.ID, SubscriptionID: provider.subscription.ID, OrderID: order.ID, OwnerReference: provider.subscription.OwnerReference, CustomerID: provider.subscription.CustomerID, SKU: plan.Monthly.SKU, PriceID: provider.subscription.PriceID, Currency: plan.Monthly.Currency, AmountMinor: plan.Monthly.AmountMinor, AmountPaid: plan.Monthly.AmountMinor, Status: "paid", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1, StripePaid: true}
			notify := func(id, kind, object string) {
				raw := []byte(fmt.Sprintf(`{"id":%q,"type":%q,"livemode":false,"data":{"object":{"id":%q,"description":"private learner text"}}}`, id, kind, object))
				r := httptest.NewRequestWithContext(ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
				r.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
				w := httptest.NewRecorder()
				svc.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("receipt %d %s", w.Code, w.Body.String())
				}
			}
			assertGrants := func(want int) {
				var n int
				if e = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='subscription_grant'", owner).Scan(&n); e != nil || n != want {
					t.Fatalf("grants %d want %d: %v", n, want, e)
				}
			}
			checkout := func() { notify("evt_checkout_"+order.ID, "checkout.session.completed", "cs_test_"+order.ID) }
			invoice := func() { notify("evt_invoice_"+order.ID, "invoice.paid", provider.invoice.ID) }
			notify("evt_subscription_early_"+order.ID, "customer.subscription.created", provider.subscription.ID)
			if e = svc.ProcessPayments(ctx); e != nil {
				t.Fatal(e)
			}
			assertGrants(0)
			if !invoiceFirst {
				checkout()
				if e = svc.ProcessPayments(ctx); e != nil {
					t.Fatal(e)
				}
				assertGrants(0)
			}
			invoice()
			provider.unavailable = true
			if e = svc.ProcessPayments(ctx); e == nil {
				t.Fatal("provider outage not retryable")
			}
			provider.unavailable = false
			if _, e = db.Pool.Exec(ctx, `CREATE FUNCTION fixture_reject_plan_outbox() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.event='plan_purchased' THEN RAISE EXCEPTION 'outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER fixture_reject_plan_outbox BEFORE INSERT ON credit_outbox FOR EACH ROW EXECUTE FUNCTION fixture_reject_plan_outbox()`); e != nil {
				t.Fatal(e)
			}
			if e = svc.ProcessPayments(ctx); e == nil {
				t.Fatal("outbox failure did not roll back")
			}
			assertGrants(0)
			if _, e = db.Pool.Exec(ctx, "DROP TRIGGER fixture_reject_plan_outbox ON credit_outbox; DROP FUNCTION fixture_reject_plan_outbox()"); e != nil {
				t.Fatal(e)
			}
			svc = service()
			var wg sync.WaitGroup
			for range 4 {
				wg.Go(func() {
					if e := svc.ProcessPayments(ctx); e != nil {
						t.Error(e)
					}
				})
			}
			wg.Wait()
			checkout()
			invoice()
			notify("evt_alias_"+order.ID, "invoice.payment_succeeded", provider.invoice.ID)
			notify("evt_sub_"+order.ID, "customer.subscription.deleted", provider.subscription.ID)
			if e = svc.ProcessPayments(ctx); e != nil {
				t.Fatal(e)
			}
			assertGrants(1)
			wallet, e := svc.GetWallet(ctx, owner)
			if e != nil || wallet.Subscription != int64(plan.MonthlyCredits) || wallet.Plan == nil || !wallet.Plan.Active || wallet.Plan.PricingVersion != cfg.Version {
				t.Fatalf("funded wallet %+v %v", wallet, e)
			}
			expired, e := svc.GetWalletAt(ctx, owner, end.Add(time.Second))
			if e != nil || expired.Plan == nil || expired.Plan.Active || expired.Subscription != int64(plan.MonthlyCredits) {
				t.Fatalf("expired benefits %+v %v", expired, e)
			}
			if _, e = svc.CreateCheckout(ctx, owner, overlap); e == nil {
				t.Fatal("active overlapping plan allowed")
			}
			for _, event := range []string{"plan_purchased", "subscription_grant_issued", "entitlement_changed"} {
				var n int
				if e = db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_outbox WHERE learner_id=$1 AND event=$2", owner, event).Scan(&n); e != nil || n != 1 {
					t.Fatalf("outbox %s %d %v", event, n, e)
				}
			}
			wantEvents := 4
			if !invoiceFirst {
				if _, e = db.Pool.Exec(ctx, "UPDATE learner_profiles SET analytics_opt_out=true WHERE learner_id=$1", owner); e != nil {
					t.Fatal(e)
				}
				wantEvents = 0
			}
			if e = svc.DispatchOutbox(ctx); e != nil {
				t.Fatal(e)
			}
			if len(events.names) != wantEvents {
				t.Fatalf("analytics %v", events.names)
			}
			if e = svc.DispatchOutbox(ctx); e != nil || len(events.names) != wantEvents {
				t.Fatal("duplicate dispatch", e)
			}
			// Distinct invoice IDs cannot fund an overlapping or duplicate service month.
			original := provider.invoice
			provider.invoice.ID = "in_duplicate_" + order.ID
			notify("evt_business_"+order.ID, "invoice.paid", provider.invoice.ID)
			if e = svc.ProcessPayments(ctx); e != nil {
				t.Fatal(e)
			}
			assertGrants(1)
			provider.invoice = original
			provider.invoice.ID = "in_wrong_account_" + order.ID
			provider.invoice.OwnerReference = "other_account"
			notify("evt_wrong_account_"+order.ID, "invoice.paid", provider.invoice.ID)
			if e = svc.ProcessPayments(ctx); e != nil {
				t.Fatal(e)
			}
			assertGrants(1)
			provider.invoice = original
			for _, table := range []string{"credit_subscriptions", "credit_subscription_terms"} {
				for _, verb := range []string{"DELETE FROM ", "TRUNCATE "} {
					if _, e = db.Pool.Exec(ctx, verb+table); e == nil {
						t.Fatal("mutable history", table)
					}
				}
			}
			if _, e = db.Pool.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)", accounts.LearnerReference(owner)); e != nil {
				t.Fatal(e)
			}
			if e = accounts.ApplyDeletionTombstones(ctx, db.Pool); e != nil {
				t.Fatal(e)
			}
			notify("evt_deleted_"+order.ID, "invoice.paid", provider.invoice.ID)
			if e = svc.ProcessPayments(ctx); e != nil {
				t.Fatal(e)
			}
		})
	}
	for _, active := range []bool{true, false} {
		t.Run(fmt.Sprintf("funded concurrency active=%t", active), func(t *testing.T) {
			learner, e := db.Queries.CreateLearner(ctx, "Concurrency subscriber")
			if e != nil {
				t.Fatal(e)
			}
			owner := uuid.UUID(learner.ID.Bytes).String()
			plan := cfg.Plans[len(cfg.Plans)-1]
			provider := &monthlyProvider{packProvider: packProvider{sessions: map[string]credits.PaymentSession{}}}
			svc := credits.NewServiceWithCatalogue(db.Pool, redisClient, cat, nil)
			if e = svc.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: "test-only-webhook-secret"}); e != nil {
				t.Fatal(e)
			}
			order, e := svc.CreateCheckout(ctx, owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: plan.Monthly.SKU, PricingVersion: cfg.Version, Platform: "desktop", ReturnTo: "/credits"})
			if e != nil {
				t.Fatal(e)
			}
			start := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
			start = time.Date(start.Year(), start.Month(), 1, start.Hour(), start.Minute(), start.Second(), 0, time.UTC)
			if !active {
				start = start.AddDate(0, -2, 0)
			}
			end := start.AddDate(0, 1, 0)
			provider.subscription = credits.ProviderSubscription{ID: "sub_" + order.ID, OrderID: order.ID, OwnerReference: accounts.LearnerReference(owner), CustomerID: "cus_" + accounts.LearnerReference(owner), PriceID: cfg.ProviderMappings("development")[plan.Monthly.SKU], Status: "active", Interval: "month", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
			provider.invoice = credits.SubscriptionInvoice{ID: "in_" + order.ID, SubscriptionID: provider.subscription.ID, OrderID: order.ID, OwnerReference: provider.subscription.OwnerReference, CustomerID: provider.subscription.CustomerID, SKU: plan.Monthly.SKU, PriceID: provider.subscription.PriceID, Currency: plan.Monthly.Currency, AmountMinor: plan.Monthly.AmountMinor, AmountPaid: plan.Monthly.AmountMinor, Status: "paid", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1, StripePaid: true}
			raw := []byte(fmt.Sprintf(`{"id":"evt_concurrency_%s","type":"invoice.paid","livemode":false,"data":{"object":{"id":%q}}}`, order.ID, provider.invoice.ID))
			req := httptest.NewRequestWithContext(ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
			req.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
			w := httptest.NewRecorder()
			svc.ServeHTTP(w, req)
			if w.Code != 200 {
				t.Fatal(w.Body.String())
			}
			if e = svc.ProcessPayments(ctx); e != nil {
				t.Fatal(e)
			}
			quotes := pricing.NewQuotes(db.Pool)
			first := acceptedQuote(t, quotes, owner, "preview")
			want := plan.ConcurrentGeneration
			if !active {
				want = int(first.Bounds["concurrency"])
			}
			var jobs []string
			for i := 0; i <= want; i++ {
				q := first
				if i > 0 {
					q = acceptedQuote(t, quotes, owner, "preview")
				}
				job, e := svc.Reserve(ctx, owner, q.ID)
				if i < want {
					if e != nil {
						t.Fatalf("funded concurrency %d/%d: %v", i, want, e)
					}
					jobs = append(jobs, job.ID)
				} else if !errors.Is(e, credits.ErrConcurrency) {
					t.Fatalf("excess concurrency allowed: %v", e)
				}
			}
			for _, job := range jobs {
				if _, e = svc.Settle(ctx, owner, job, nil); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}
