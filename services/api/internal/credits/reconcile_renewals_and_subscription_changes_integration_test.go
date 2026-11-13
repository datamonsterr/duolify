//go:build integration

package credits_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
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

type lifecycleProvider struct {
	monthlyProvider
	transitions map[string]credits.SubscriptionTransition
}

func (p *lifecycleProvider) ScheduleTransition(_ context.Context, v credits.SubscriptionTransition) error {
	if v.ProrationBehavior != "none" || v.PhaseProrationBehavior != "none" || !v.EffectiveAt.Equal(p.subscription.PeriodEnd) {
		return fmt.Errorf("unsafe schedule")
	}
	if old, ok := p.transitions[v.Key]; ok && old != v {
		return fmt.Errorf("conflicting intent")
	}
	p.transitions[v.Key] = v
	if v.Kind == "cancel" {
		p.subscription.CancelAtPeriodEnd = true
	}
	if v.Kind == "resume" {
		p.subscription.CancelAtPeriodEnd = false
	}
	return nil
}

func TestReconcileRenewalsAndSubscriptionChanges(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("lifecycle"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	plan := cfg.Plans[1]
	learner, err := db.Queries.CreateLearner(ctx, "Lifecycle")
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.UUID(learner.ID.Bytes).String()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1,false)", owner); err != nil {
		t.Fatal(err)
	}
	provider := &lifecycleProvider{monthlyProvider: monthlyProvider{packProvider: packProvider{sessions: map[string]credits.PaymentSession{}}}, transitions: map[string]credits.SubscriptionTransition{}}
	events := new(packEvents)
	svc := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	if err = svc.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: "test-only-webhook-secret"}); err != nil {
		t.Fatal(err)
	}
	order, err := svc.CreateCheckout(ctx, owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: plan.Monthly.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	end := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, -1, 0)
	provider.subscription = credits.ProviderSubscription{ID: "sub_" + order.ID, OrderID: order.ID, OwnerReference: accounts.LearnerReference(owner), CustomerID: "cus_" + order.ID, PriceID: cfg.ProviderMappings("development")[order.SKU], Status: "active", Interval: "month", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
	provider.invoice = credits.SubscriptionInvoice{ID: "in_first_" + order.ID, SubscriptionID: provider.subscription.ID, OrderID: order.ID, OwnerReference: provider.subscription.OwnerReference, CustomerID: provider.subscription.CustomerID, SKU: order.SKU, PriceID: provider.subscription.PriceID, Currency: order.Currency, AmountMinor: order.AmountMinor, AmountPaid: order.AmountMinor, Status: "paid", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1, StripePaid: true}
	notify := func(id, kind, object string) {
		t.Helper()
		raw := []byte(fmt.Sprintf(`{"id":%q,"type":%q,"livemode":false,"data":{"object":{"id":%q,"description":"private learner text"}}}`, id, kind, object))
		req := httptest.NewRequestWithContext(ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
		req.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
		w := httptest.NewRecorder()
		svc.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		if e := svc.ProcessPayments(ctx); e != nil {
			t.Fatal(e)
		}
	}
	assertPlan := func(at time.Time, status string, active bool, balance int64) {
		t.Helper()
		w, e := svc.GetWalletAt(ctx, owner, at)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(w.Plan)
		var fields map[string]any
		_ = json.Unmarshal(raw, &fields)
		if w.Plan == nil || fields["status"] != status || w.Plan.Active != active || w.Subscription != balance {
			t.Fatalf("plan %s balance %d; want %s active=%t balance=%d", raw, w.Subscription, status, active, balance)
		}
	}
	notify("evt_first", "invoice.paid", provider.invoice.ID)
	provider.subscription.CancelAtPeriodEnd = true
	notify("evt_cancel", "customer.subscription.updated", provider.subscription.ID)
	assertPlan(end.Add(-time.Second), "cancel-scheduled", true, int64(plan.MonthlyCredits))
	provider.subscription.CancelAtPeriodEnd = false
	notify("evt_resume", "customer.subscription.updated", provider.subscription.ID)
	assertPlan(end.Add(-time.Second), "active", true, int64(plan.MonthlyCredits))
	provider.subscription.Status = "past_due"
	provider.invoice.ID = "in_failed_" + order.ID
	provider.invoice.Status = "open"
	provider.invoice.StripePaid = false
	provider.invoice.AmountPaid = 0
	provider.invoice.BillingReason = "subscription_cycle"
	provider.invoice.PeriodStart = end
	provider.invoice.PeriodEnd = end.AddDate(0, 1, 0)
	provider.subscription.PeriodStart = provider.invoice.PeriodStart
	provider.subscription.PeriodEnd = provider.invoice.PeriodEnd
	notify("evt_failure", "invoice.payment_failed", provider.invoice.ID)
	notify("evt_failure_alias", "invoice.payment_failed", provider.invoice.ID)
	assertPlan(end.Add(-time.Second), "past-due", true, int64(plan.MonthlyCredits))
	assertPlan(end.Add(time.Second), "past-due", false, int64(plan.MonthlyCredits))
	provider.invoice.Status = "paid"
	provider.invoice.StripePaid = true
	provider.invoice.AmountPaid = order.AmountMinor
	provider.subscription.Status = "active"
	provider.subscription.LatestInvoiceID = provider.invoice.ID
	if err = svc.ReconcileSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	if err = svc.ReconcileSubscriptions(ctx); err != nil {
		t.Fatal(err)
	}
	notify("evt_renewal", "invoice.paid", provider.invoice.ID)
	notify("evt_renewal_alias", "invoice.payment_succeeded", provider.invoice.ID)
	notify("evt_stale_cancel", "customer.subscription.deleted", provider.subscription.ID)
	assertPlan(now, "active", true, 2*int64(plan.MonthlyCredits))
	for _, name := range []string{"subscription_renewed", "subscription_cancelled", "subscription_payment_failed"} {
		var n int
		if e := db.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_outbox WHERE learner_id=$1 AND event=$2", owner, name).Scan(&n); e != nil || n != 1 {
			t.Fatalf("%s outbox=%d: %v", name, n, e)
		}
	}

	// Current funded terms authorize priority, never the non-expiring balance.
	quotes := pricing.NewQuotes(db.Pool)
	ordinary, e := db.Queries.CreateLearner(ctx, "Ordinary")
	if e != nil {
		t.Fatal(e)
	}
	ordinaryID := uuid.UUID(ordinary.ID.Bytes).String()
	if _, e = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,10000,'purchase','fixture-ordinary',$2)`, ordinaryID, cfg.Version); e != nil {
		t.Fatal(e)
	}
	normalJob, e := svc.Reserve(ctx, ordinaryID, acceptedQuote(t, quotes, ordinaryID, "preview").ID)
	if e != nil {
		t.Fatal(e)
	}
	priorityJob, e := svc.Reserve(ctx, owner, acceptedQuote(t, quotes, owner, "preview").ID)
	if e != nil {
		t.Fatal(e)
	}
	waiting, cancel := context.WithTimeout(ctx, 60*time.Millisecond)
	if e = svc.Claim(waiting, ordinaryID, normalJob.ID); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("ordinary job bypassed priority: %v", e)
	}
	cancel()
	if e = svc.Claim(ctx, owner, priorityJob.ID); e != nil {
		t.Fatal(e)
	}
	if e = svc.Claim(ctx, ordinaryID, normalJob.ID); e != nil {
		t.Fatal(e)
	}
	for _, j := range []struct{ owner, id string }{{owner, priorityJob.ID}, {ordinaryID, normalJob.ID}} {
		if _, e = svc.Settle(ctx, j.owner, j.id, nil); e != nil {
			t.Fatal(e)
		}
	}
	// Premium priority still cannot cross the operational provider/USD cap.
	if _, e = db.Pool.Exec(ctx, `UPDATE credit_cost_counters SET spent_nanos=1000000000000 WHERE scope='learner' AND owner_reference=$1`, accounts.LearnerReference(owner)); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Reserve(ctx, owner, acceptedQuote(t, quotes, owner, "preview").ID); !errors.Is(e, credits.ErrBudget) {
		t.Fatalf("premium bypassed USD cap: %v", e)
	}
	if _, e = db.Pool.Exec(ctx, `UPDATE credit_cost_counters SET spent_nanos=0 WHERE scope='learner' AND owner_reference=$1`, accounts.LearnerReference(owner)); e != nil {
		t.Fatal(e)
	}
	change := credits.ChangeSubscriptionInput{RequestID: uuid.NewString(), Kind: "plan", SKU: cfg.Plans[0].Monthly.SKU, PricingVersion: cfg.Version}
	scheduled, e := svc.ChangeSubscription(ctx, owner, change)
	if e != nil || !scheduled.EffectiveAt.Equal(provider.invoice.PeriodEnd) {
		t.Fatalf("schedule %+v %v", scheduled, e)
	}
	if _, e = svc.ChangeSubscription(ctx, owner, change); e != nil || len(provider.transitions) != 1 {
		t.Fatalf("retry duplicated provider change: %v", e)
	}
	if _, e = svc.ChangeSubscription(ctx, ordinaryID, change); e == nil {
		t.Fatal("another learner claimed transition")
	}
	altered := change
	altered.SKU = cfg.Plans[2].Monthly.SKU
	if _, e = svc.ChangeSubscription(ctx, owner, altered); e == nil {
		t.Fatal("changed idempotent intent")
	}
	intervalChange := change
	intervalChange.RequestID = uuid.NewString()
	intervalChange.SKU = cfg.Plans[0].Yearly.SKU
	if _, e = svc.ChangeSubscription(ctx, owner, intervalChange); e == nil {
		t.Fatal("unverified interval change")
	}
	w, e := svc.GetWallet(ctx, owner)
	if e != nil || w.Plan.PendingPlan == nil || w.Plan.PlanID != plan.ID || w.Subscription != 2*int64(plan.MonthlyCredits) {
		t.Fatalf("schedule changed funding %+v %v", w, e)
	}
	for _, table := range []string{"credit_subscription_lifecycle", "credit_subscription_transitions"} {
		for _, verb := range []string{"UPDATE " + table + " SET subscription_id=subscription_id", "DELETE FROM " + table, "TRUNCATE " + table} {
			if _, e = db.Pool.Exec(ctx, verb); e == nil {
				t.Fatal("mutable lifecycle", verb)
			}
		}
	}
	// Lifecycle events cannot rewrite the ledger, including after expiry.
	assertPlan(provider.invoice.PeriodEnd, "expired", false, 2*int64(plan.MonthlyCredits))
	if err = svc.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}

	t.Run("restored pending downgrade funds only the new configured allowance", func(t *testing.T) {
		learner, e := db.Queries.CreateLearner(ctx, "Restored transition")
		if e != nil {
			t.Fatal(e)
		}
		id := uuid.UUID(learner.ID.Bytes).String()
		old, e := svc.CreateCheckout(ctx, id, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: plan.Monthly.SKU, PricingVersion: cfg.Version, Platform: "web", ReturnTo: "/credits"})
		if e != nil {
			t.Fatal(e)
		}
		sub := provider.subscription
		sub.ID = "sub_" + old.ID
		sub.OrderID = old.ID
		sub.CustomerID = "cus_" + old.ID
		sub.OwnerReference = accounts.LearnerReference(id)
		sub.PeriodStart = start
		sub.PeriodEnd = end
		sub.BillingAnchor = start
		invoice := provider.invoice
		invoice.ID = "in_old_" + old.ID
		invoice.OrderID = old.ID
		invoice.SubscriptionID = sub.ID
		invoice.CustomerID = sub.CustomerID
		invoice.OwnerReference = sub.OwnerReference
		invoice.PeriodStart = start
		invoice.PeriodEnd = end
		invoice.BillingReason = "subscription_create"
		provider.subscription = sub
		provider.invoice = invoice
		notify("evt_restored_old", "invoice.paid", invoice.ID)
		newPlan := cfg.Plans[0]
		newOffer := newPlan.Monthly
		pinned, _ := json.Marshal(map[string]any{"order": map[string]any{"sku": newOffer.SKU, "amount_minor": newOffer.AmountMinor, "currency": newOffer.Currency, "credits": newPlan.MonthlyCredits, "billing_period": "monthly"}, "plan": newPlan})
		if _, e = db.Pool.Exec(ctx, `INSERT INTO credit_subscription_transitions(request_id,subscription_id,kind,effective_at,pricing_version,terms,price_id) VALUES($1,$2,'plan',$3,$4,$5,$6)`, uuid.NewString(), sub.ID, end, cfg.Version, pinned, cfg.ProviderMappings("development")[newOffer.SKU]); e != nil {
			t.Fatal(e)
		}
		provider.subscription.PeriodStart = end
		provider.subscription.PeriodEnd = end.AddDate(0, 1, 0)
		provider.subscription.PriceID = cfg.ProviderMappings("development")[newOffer.SKU]
		provider.invoice.ID = "in_downgrade_" + old.ID
		provider.invoice.PeriodStart = end
		provider.invoice.PeriodEnd = provider.subscription.PeriodEnd
		provider.invoice.BillingReason = "subscription_cycle"
		provider.invoice.SKU = newOffer.SKU
		provider.invoice.PriceID = provider.subscription.PriceID
		provider.invoice.AmountMinor = newOffer.AmountMinor
		provider.invoice.AmountPaid = newOffer.AmountMinor
		notify("evt_downgrade_paid", "invoice.paid", provider.invoice.ID)
		notify("evt_downgrade_paid_alias", "invoice.payment_succeeded", provider.invoice.ID)
		w, e := svc.GetWallet(ctx, id)
		if e != nil || w.Plan == nil || w.Plan.PlanID != newPlan.ID || w.Plan.PendingPlan != nil || w.Plan.PriorityGeneration || w.Subscription != int64(plan.MonthlyCredits+newPlan.MonthlyCredits) {
			t.Fatalf("downgrade funded wrong terms %+v %v", w, e)
		}
		if _, e = db.Pool.Exec(ctx, `INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1)`, accounts.LearnerReference(id)); e != nil {
			t.Fatal(e)
		}
		if e = accounts.ApplyDeletionTombstones(ctx, db.Pool); e != nil {
			t.Fatal(e)
		}
		var retained int
		if e = db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_subscription_transitions x JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id WHERE b.subscription_id=$1 AND o.learner_id IS NULL`, sub.ID).Scan(&retained); e != nil || retained != 1 {
			t.Fatalf("identity severance lost history %d %v", retained, e)
		}
	})

}
