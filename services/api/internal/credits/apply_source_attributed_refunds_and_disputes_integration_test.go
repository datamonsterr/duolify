//go:build integration

package credits_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
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

// rvEvents records delivered analytics events including properties.
type rvEvents struct {
	mu   sync.Mutex
	got  []string
	prop []map[string]any
}

func (e *rvEvents) Capture(id, name string, props map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, err := uuid.Parse(id); err != nil {
		return err
	}
	if props["source"] != "api" || props["app_version"] == nil || props["platform"] == nil {
		return fmt.Errorf("missing common properties")
	}
	e.got = append(e.got, name)
	e.prop = append(e.prop, props)
	return nil
}
func (*rvEvents) Close() error { return nil }
func (e *rvEvents) count(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, g := range e.got {
		if g == name {
			n++
		}
	}
	return n
}

type rvEnv struct {
	t        *testing.T
	ctx      context.Context
	db       *store.Store
	cfg      pricing.Config
	provider *monthlyProvider
	events   *rvEvents
	svc      *credits.Service
	seq      int
}

type rvLearner struct {
	owner string
}

func rvSetup(t *testing.T) *rvEnv {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("reversals"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	t.Cleanup(db.Close)
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	e := &rvEnv{t: t, ctx: ctx, db: db, cfg: cat.Config(), provider: &monthlyProvider{packProvider: packProvider{sessions: map[string]credits.PaymentSession{}}}, events: new(rvEvents)}
	e.svc = credits.NewServiceWithCatalogue(db.Pool, nil, cat, e.events)
	if err = e.svc.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: e.provider, WebhookSecret: "test-only-webhook-secret"}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *rvEnv) learner(optOut bool) rvLearner {
	e.t.Helper()
	l, err := e.db.Queries.CreateLearner(e.ctx, "Reversal learner")
	if err != nil {
		e.t.Fatal(err)
	}
	owner := uuid.UUID(l.ID.Bytes).String()
	if _, err = e.db.Pool.Exec(e.ctx, "INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1,$2)", owner, optOut); err != nil {
		e.t.Fatal(err)
	}
	return rvLearner{owner}
}

func (e *rvEnv) notify(id, kind, object string) error {
	e.t.Helper()
	raw := []byte(fmt.Sprintf(`{"id":%q,"type":%q,"livemode":false,"data":{"object":{"id":%q}}}`, id, kind, object))
	r := httptest.NewRequestWithContext(e.ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
	r.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
	w := httptest.NewRecorder()
	e.svc.ServeHTTP(w, r)
	if w.Code != 200 {
		e.t.Fatalf("webhook %d %s", w.Code, w.Body.String())
	}
	return e.svc.ProcessPayments(e.ctx)
}

// order creates a pack checkout whose canonical session is paid but unprocessed.
func (e *rvEnv) order(l rvLearner, pack int) (credits.CheckoutOrder, string) {
	e.t.Helper()
	p := e.cfg.Packs[pack]
	o, err := e.svc.CreateCheckout(e.ctx, l.owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: p.SKU, PricingVersion: e.cfg.Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		e.t.Fatal(err)
	}
	s := credits.PaymentSession{ID: "cs_test_" + o.ID, OrderID: o.ID, OwnerReference: accounts.LearnerReference(l.owner), SKU: o.SKU, PriceID: e.cfg.ProviderMappings("development")[o.SKU], AmountMinor: o.AmountMinor, Currency: o.Currency, Mode: "payment", Status: "complete", PaymentStatus: "paid", PaymentID: "pi_" + o.ID}
	e.provider.mu.Lock()
	e.provider.sessions[s.ID] = s
	e.provider.mu.Unlock()
	return o, s.ID
}

func (e *rvEnv) canonical(session string, f func(*credits.PaymentSession)) {
	e.provider.mu.Lock()
	defer e.provider.mu.Unlock()
	s := e.provider.sessions[session]
	f(&s)
	e.provider.sessions[session] = s
}

func (e *rvEnv) refund(session, id string, amount int, currency string) {
	e.canonical(session, func(s *credits.PaymentSession) {
		s.Refunds = append(s.Refunds, credits.ProviderRefund{ID: id, AmountMinor: amount, Currency: currency, Status: "succeeded"})
	})
}

func (e *rvEnv) spend(l rvLearner, source int64, amount int64) {
	e.t.Helper()
	_, err := e.db.Pool.Exec(e.ctx, `INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance)
		VALUES($1,$2,$3,'spend',$4,$5,jsonb_build_object('source_id',$6::text,'bucket','purchase'))`, l.owner, accounts.LearnerReference(l.owner), -amount, "spend-"+uuid.NewString(), e.cfg.Version, fmt.Sprint(source))
	if err != nil {
		e.t.Fatal(err)
	}
	_ = e.svc.InvalidateCache(e.ctx, l.owner)
}

func (e *rvEnv) sourceID(order string) int64 {
	var id int64
	if err := e.db.Pool.QueryRow(e.ctx, `SELECT ledger_id FROM credit_payment_results WHERE order_id=$1 AND state='paid'`, order).Scan(&id); err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *rvEnv) n(query string, args ...any) int64 {
	e.t.Helper()
	var n int64
	if err := e.db.Pool.QueryRow(e.ctx, query, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *rvEnv) purchased(l rvLearner) int64 {
	e.t.Helper()
	_ = e.svc.InvalidateCache(e.ctx, l.owner)
	w, err := e.svc.GetWallet(e.ctx, l.owner)
	if err != nil {
		e.t.Fatal(err)
	}
	return w.Purchased
}

func TestApplySourceAttributedRefundsAndDisputesIntegration(t *testing.T) {
	e := rvSetup(t)

	t.Run("two partial refunds after a partly spent purchase", func(t *testing.T) {
		l := e.learner(false)
		other, osess := e.order(l, 1) // unrelated pack, preserved
		if err := e.notify("evt_paid_"+other.ID, "checkout.session.completed", osess); err != nil {
			t.Fatal(err)
		}
		o, sess := e.order(l, 0) // 400 credits / 500 minor
		if err := e.notify("evt_paid_"+o.ID, "checkout.session.completed", sess); err != nil {
			t.Fatal(err)
		}
		src := e.sourceID(o.ID)
		e.spend(l, src, 140)
		// Both refunds happened before either notification was processed: the
		// canonical charge carries two objects, each applied under its own id.
		e.refund(sess, "re_first", 250, o.Currency)
		e.refund(sess, "re_second", 100, o.Currency)
		if err := e.notify("evt_ref1_"+o.ID, "charge.refunded", sess); err != nil {
			t.Fatal(err)
		}
		if got := e.n("SELECT count(*) FROM credit_reversals WHERE order_id=$1", o.ID); got != 2 {
			t.Fatalf("reversals=%d", got)
		}
		// cumulative: 250→200 credits, 350→280; second reverses 80 of the 120 unspent... 260-200=60 → 60 + debt 20
		if e.n("SELECT credits_reversed FROM credit_reversals WHERE reversal_key='stripe:test:re_first'") != 200 || e.n("SELECT credits_reversed FROM credit_reversals WHERE reversal_key='stripe:test:re_second'") != 80 {
			t.Fatal("cumulative credits")
		}
		if debt, _ := e.svc.GetSourceDebt(e.ctx, l.owner); debt != 20 {
			t.Fatalf("debt=%d", debt)
		}
		if got := e.purchased(l); got != 0+1350 {
			t.Fatalf("purchased=%d (unrelated pack must be preserved)", got)
		}
		// Replays with new event ids change nothing.
		if err := e.notify("evt_ref_replay_"+o.ID, "charge.refunded", sess); err != nil {
			t.Fatal(err)
		}
		if e.n("SELECT count(*) FROM credit_reversals WHERE order_id=$1", o.ID) != 2 || e.n("SELECT count(*) FROM credit_entries WHERE kind='refund' AND learner_id=$1", l.owner) != 2 {
			t.Fatal("replay applied twice")
		}
		// Final refund reaches the cap exactly; one past it fails closed.
		e.refund(sess, "re_third", 150, o.Currency)
		e.refund(sess, "re_over", 1, o.Currency)
		before := e.n("SELECT count(*) FROM credit_reversals")
		if err := e.notify("evt_ref3_"+o.ID, "charge.refunded", sess); err == nil {
			t.Fatal("cap exceeded must fail closed")
		}
		if e.n("SELECT count(*) FROM credit_reversals") != before {
			t.Fatal("failed reconcile changed the ledger")
		}
		if e.n("SELECT count(*) FROM credit_payment_results WHERE event_id=$1", "evt_ref3_"+o.ID) != 0 {
			t.Fatal("event must stay unprocessed for retry")
		}
		// The provider corrects its state; the same inbox event retries and applies the valid remainder.
		e.canonical(sess, func(s *credits.PaymentSession) { s.Refunds = s.Refunds[:3] })
		if err := e.svc.ProcessPayments(e.ctx); err != nil {
			t.Fatal(err)
		}
		if e.n("SELECT sum(amount_minor) FROM credit_reversals WHERE order_id=$1", o.ID) != int64(o.AmountMinor) {
			t.Fatal("retry did not complete the refund")
		}
	})

	t.Run("completion processed after the refund fulfils once then reverses", func(t *testing.T) {
		l := e.learner(false)
		o, sess := e.order(l, 0)
		e.refund(sess, "re_ooo", 500, o.Currency)
		if err := e.notify("evt_refund_first_"+o.ID, "charge.refunded", sess); err != nil {
			t.Fatal(err)
		}
		if err := e.notify("evt_completed_late_"+o.ID, "checkout.session.completed", sess); err != nil {
			t.Fatal(err)
		}
		if e.n("SELECT count(*) FROM credit_entries WHERE kind='purchase' AND learner_id=$1", l.owner) != 1 {
			t.Fatal("purchase must be fulfilled exactly once")
		}
		if e.n("SELECT count(*) FROM credit_entries WHERE kind='refund' AND learner_id=$1", l.owner) != 1 || e.purchased(l) != 0 {
			t.Fatal("full refund must reverse exactly the source credits")
		}
		if e.n("SELECT credits_reversed FROM credit_reversals WHERE order_id=$1", o.ID) != int64(o.Credits) {
			t.Fatal("full refund reverses the source credits")
		}
	})

	t.Run("cumulative rounding never loses or exceeds credits", func(t *testing.T) {
		l := e.learner(false)
		o, sess := e.order(l, 0)
		if err := e.notify("evt_paid_"+o.ID, "checkout.session.completed", sess); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ { // 1 minor each: floor per refund would reverse 0
			e.refund(sess, fmt.Sprintf("re_tiny_%d", i), 1, o.Currency)
		}
		e.refund(sess, "re_rest", o.AmountMinor-5, o.Currency)
		if err := e.notify("evt_tiny_"+o.ID, "charge.refunded", sess); err != nil {
			t.Fatal(err)
		}
		if got := e.n("SELECT sum(credits_reversed) FROM credit_reversals WHERE order_id=$1", o.ID); got != int64(o.Credits) {
			t.Fatalf("sum reversed=%d want %d", got, o.Credits)
		}
	})

	t.Run("fails closed without evidence and never uses another order", func(t *testing.T) {
		l := e.learner(false)
		earlier, esess := e.order(l, 1)
		if err := e.notify("evt_paid_"+earlier.ID, "checkout.session.completed", esess); err != nil {
			t.Fatal(err)
		}
		o, sess := e.order(l, 0)
		e.canonical(sess, func(s *credits.PaymentSession) { s.PaymentStatus = "unpaid"; s.Status = "open" })
		e.refund(sess, "re_unpaid", 100, o.Currency)
		if err := e.notify("evt_unpaid_ref_"+o.ID, "charge.refunded", sess); err == nil {
			t.Fatal("refund of an unfulfilled payment must fail closed")
		}
		if e.n("SELECT count(*) FROM credit_reversals WHERE order_id IN ($1,$2)", o.ID, earlier.ID) != 0 || e.purchased(l) != int64(earlier.Credits) {
			t.Fatal("another order was reversed")
		}
		e.canonical(sess, func(s *credits.PaymentSession) { s.Refunds = nil })
		if err := e.svc.ProcessPayments(e.ctx); err != nil {
			t.Fatal(err)
		}
		bad := map[string]credits.ProviderRefund{
			"noid":     {AmountMinor: 100, Currency: o.Currency, Status: "succeeded"},
			"noamt":    {ID: "re_noamt", Currency: o.Currency, Status: "succeeded"},
			"currency": {ID: "re_cur", AmountMinor: 100, Currency: "eur", Status: "succeeded"},
		}
		for name, r := range bad {
			l2 := e.learner(false)
			o2, s2 := e.order(l2, 0)
			e.canonical(s2, func(s *credits.PaymentSession) { s.Refunds = []credits.ProviderRefund{r} })
			if err := e.notify("evt_bad_"+name+o2.ID, "charge.refunded", s2); err == nil {
				t.Fatalf("%s: expected failure", name)
			}
			if e.n("SELECT count(*) FROM credit_reversals WHERE order_id=$1", o2.ID) != 0 {
				t.Fatalf("%s: ledger changed", name)
			}
			e.canonical(s2, func(s *credits.PaymentSession) { s.Refunds = nil })
			if err := e.svc.ProcessPayments(e.ctx); err != nil {
				t.Fatal(err)
			}
		}
	})

	t.Run("chargeback of a depleted source records debt and gates paid AI", func(t *testing.T) {
		l := e.learner(false)
		o, sess := e.order(l, 0)
		if err := e.notify("evt_paid_"+o.ID, "checkout.session.completed", sess); err != nil {
			t.Fatal(err)
		}
		e.spend(l, e.sourceID(o.ID), int64(o.Credits))
		e.canonical(sess, func(s *credits.PaymentSession) {
			s.Disputes = []credits.ProviderDispute{{ID: "dp_depleted", AmountMinor: o.AmountMinor, Currency: o.Currency, Status: "needs_response"}}
		})
		if err := e.notify("evt_dispute_"+o.ID, "charge.dispute.created", sess); err != nil {
			t.Fatal(err)
		}
		if debt, _ := e.svc.GetSourceDebt(e.ctx, l.owner); debt != int64(o.Credits) {
			t.Fatalf("debt=%d", debt)
		}
		if e.n("SELECT count(*) FROM credit_dispute_keys WHERE dispute_key='dp_depleted'") != 1 {
			t.Fatal("dispute key")
		}
		quotes := pricing.NewQuotes(e.db.Pool)
		accept := func(action string) string {
			q, err := quotes.Create(e.ctx, l.owner, pricing.QuoteInput{Action: action})
			if err != nil {
				t.Fatal(err)
			}
			if err = quotes.Accept(e.ctx, l.owner, q.ID); err != nil {
				t.Fatal(err)
			}
			return q.ID
		}
		if _, err := e.svc.Reserve(e.ctx, l.owner, accept("lesson_content")); err == nil {
			t.Fatal("paid AI must be blocked while debt is active")
		}
		if _, err := e.svc.EnsureDailyGrant(e.ctx, l.owner, time.Now()); err != nil {
			t.Fatal(err)
		}
		res, err := e.svc.Reserve(e.ctx, l.owner, accept("quick_note"))
		if err != nil {
			t.Fatalf("daily free must stay available: %v", err)
		}
		if _, err = e.svc.Settle(e.ctx, l.owner, res.ID, nil); err != nil {
			t.Fatal(err)
		}

		// Goodwill: operator only, audited in the database, reason code only in analytics.
		if err = e.svc.ApplyGoodwillAdjustment(e.ctx, uuid.NewString(), l.owner, 0, "goodwill_adjustment", "private note"); err == nil {
			t.Fatal("non-operator must be rejected")
		}
		op := e.learner(false).owner
		if _, err = e.db.Pool.Exec(e.ctx, "INSERT INTO pricing_admin_grants(account_id) VALUES($1)", op); err != nil {
			t.Fatal(err)
		}
		if err = e.svc.ApplyGoodwillAdjustment(e.ctx, op, l.owner, 999999, "goodwill_adjustment", "no such source"); err == nil {
			t.Fatal("no match must be an error")
		}
		if e.n("SELECT count(*) FROM credit_goodwill_audit") != 0 {
			t.Fatal("no audit for a non-match")
		}
		if err = e.svc.ApplyGoodwillAdjustment(e.ctx, op, l.owner, 0, "goodwill_adjustment", "Customer called; private details"); err != nil {
			t.Fatal(err)
		}
		if e.n("SELECT count(*) FROM credit_goodwill_audit WHERE notes LIKE 'Customer called%' AND cardinality(debt_ids)=1") != 1 {
			t.Fatal("audit row")
		}
		leaked := e.n(`SELECT count(*) FROM credit_outbox WHERE properties::text LIKE '%Customer called%' OR properties::text LIKE '%'||$1||'%' OR properties ? 'reason' OR properties ? 'actor_id'`, op)
		if leaked != 0 {
			t.Fatal("private goodwill text reached analytics outbox")
		}
		if debt, _ := e.svc.GetSourceDebt(e.ctx, l.owner); debt != 0 {
			t.Fatal("debt not waived")
		}
	})

	t.Run("concurrent notifications apply a refund once", func(t *testing.T) {
		l := e.learner(false)
		o, sess := e.order(l, 0)
		if err := e.notify("evt_paid_"+o.ID, "checkout.session.completed", sess); err != nil {
			t.Fatal(err)
		}
		e.refund(sess, "re_race", 250, o.Currency)
		for _, id := range []string{"evt_race_a_", "evt_race_b_", "evt_race_c_"} {
			raw := fmt.Sprintf(`{"id":%q,"type":"charge.refunded","livemode":false,"data":{"object":{"id":%q}}}`, id+o.ID, sess)
			r := httptest.NewRequestWithContext(e.ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(raw))
			r.Header.Set("Stripe-Signature", signedPack([]byte(raw), time.Now()))
			w := httptest.NewRecorder()
			e.svc.ServeHTTP(w, r)
		}
		var wg sync.WaitGroup
		for range 6 {
			wg.Go(func() { _ = e.svc.ProcessPayments(e.ctx) })
		}
		wg.Wait()
		if e.n("SELECT count(*) FROM credit_reversals WHERE reversal_key='stripe:test:re_race'") != 1 || e.n("SELECT count(*) FROM credit_entries WHERE kind='refund' AND learner_id=$1", l.owner) != 1 {
			t.Fatal("refund applied more than once")
		}
		if e.purchased(l) != 200 {
			t.Fatalf("balance %d", e.purchased(l))
		}
	})

	t.Run("analytics once per reversal with no private text and opt-out honoured", func(t *testing.T) {
		in, out := e.learner(false), e.learner(true)
		refs := map[string]string{}
		for name, l := range map[string]rvLearner{"in": in, "out": out} {
			o, sess := e.order(l, 0)
			refs[o.ID] = name
			if err := e.notify("evt_paid_"+o.ID, "checkout.session.completed", sess); err != nil {
				t.Fatal(err)
			}
			e.refund(sess, "re_an_"+strings.ReplaceAll(l.owner[:8], "-", ""), 250, o.Currency)
			if err := e.notify("evt_an_"+o.ID, "charge.refunded", sess); err != nil {
				t.Fatal(err)
			}
			if err := e.notify("evt_an_dup_"+o.ID, "charge.refunded", sess); err != nil {
				t.Fatal(err)
			}
		}
		if e.n("SELECT count(*) FROM credit_outbox WHERE event='credits_refunded'") < 2 {
			t.Fatal("outbox")
		}
		for range 2 { // dispatching twice must not duplicate
			if err := e.svc.DispatchOutbox(e.ctx); err != nil {
				t.Fatal(err)
			}
		}
		e.events.mu.Lock()
		defer e.events.mu.Unlock()
		sent := map[string]int{}
		for i, name := range e.events.got {
			p := e.events.prop[i]
			ref, _ := p["payment_reference"].(string)
			if name != "credits_refunded" || refs[ref] == "" {
				continue
			}
			sent[refs[ref]]++
			if p["amount"] == nil || p["reason_code"] != "requested_by_customer" || p["reason"] != nil || p["actor_id"] != nil {
				t.Fatalf("props %v", p)
			}
		}
		if sent["in"] != 1 || sent["out"] != 0 {
			t.Fatalf("delivery %v: want exactly one credits_refunded for the opted-in learner and none for opt-out", sent)
		}
	})
}

func TestApplyRefundsToFundedAnnualSubscription(t *testing.T) {
	e := rvSetup(t)
	l := e.learner(false)
	plan := e.cfg.Plans[0]
	o, err := e.svc.CreateCheckout(e.ctx, l.owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: plan.Yearly.SKU, PricingVersion: e.cfg.Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 12, 0, 0, 0, time.UTC)
	ref := accounts.LearnerReference(l.owner)
	e.provider.subscription = credits.ProviderSubscription{ID: "sub_" + o.ID, OrderID: o.ID, OwnerReference: ref, CustomerID: "cus_" + ref, PriceID: e.cfg.ProviderMappings("development")[plan.Yearly.SKU], Status: "active", Interval: "year", IntervalCount: 1, Quantity: 1, PeriodStart: start, PeriodEnd: end, BillingAnchor: start}
	e.provider.invoice = credits.SubscriptionInvoice{ID: "in_" + o.ID, SubscriptionID: e.provider.subscription.ID, OrderID: o.ID, OwnerReference: ref, CustomerID: e.provider.subscription.CustomerID, SKU: plan.Yearly.SKU, PriceID: e.provider.subscription.PriceID, Currency: plan.Yearly.Currency, AmountMinor: plan.Yearly.AmountMinor, AmountPaid: plan.Yearly.AmountMinor, Status: "paid", BillingReason: "subscription_create", PeriodStart: start, PeriodEnd: end, Quantity: 1, LineCount: 1, StripePaid: true}
	if err = e.notify("evt_fund_"+o.ID, "invoice.paid", e.provider.invoice.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = e.svc.IssueAnnualAllowances(e.ctx, time.Date(2024, 3, 31, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	grants := e.n("SELECT count(*) FROM credit_entries WHERE kind='subscription_grant' AND learner_id=$1", l.owner)
	if grants != 3 {
		t.Fatalf("grants=%d", grants)
	}
	// Spend the whole oldest grant, then fully refund the annual invoice.
	var first int64
	if err = e.db.Pool.QueryRow(e.ctx, "SELECT min(id) FROM credit_entries WHERE kind='subscription_grant' AND learner_id=$1", l.owner).Scan(&first); err != nil {
		t.Fatal(err)
	}
	_, err = e.db.Pool.Exec(e.ctx, `INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance) VALUES($1,$2,$3,'spend','sub-spend',$4,jsonb_build_object('source_id',$5::text,'bucket','subscription'))`, l.owner, ref, -int64(plan.MonthlyCredits), e.cfg.Version, fmt.Sprint(first))
	if err != nil {
		t.Fatal(err)
	}
	e.provider.invoice.Refunds = []credits.ProviderRefund{{ID: "re_sub_full", AmountMinor: plan.Yearly.AmountMinor, Currency: plan.Yearly.Currency, Status: "succeeded"}}
	if err = e.notify("evt_sub_ref_"+o.ID, "invoice.updated", e.provider.invoice.ID); err != nil {
		t.Fatal(err)
	}
	// Future grants stop; revocation and entitlement event recorded once.
	if _, err = e.svc.IssueAnnualAllowances(e.ctx, end); err != nil {
		t.Fatal(err)
	}
	if e.n("SELECT count(*) FROM credit_entries WHERE kind='subscription_grant' AND learner_id=$1", l.owner) != 3 {
		t.Fatal("future grants issued after refund")
	}
	if e.n("SELECT count(*) FROM credit_subscription_revocations") != 1 || e.n("SELECT count(*) FROM credit_outbox WHERE event='entitlement_changed' AND event_key LIKE '%:revoked'") != 1 {
		t.Fatal("revocation/entitlement event once")
	}
	// Prior spend preserved: the spent first grant's reversal is debt attributed to it.
	if e.n("SELECT count(*) FROM credit_debt_history WHERE source_id=$1 AND status='active'", first) != 1 {
		t.Fatal("debt must be attributed to the spent source grant")
	}
	if e.n("SELECT count(*) FROM credit_entries WHERE kind='spend' AND reference='sub-spend'") != 1 {
		t.Fatal("prior spend rewritten")
	}
}
