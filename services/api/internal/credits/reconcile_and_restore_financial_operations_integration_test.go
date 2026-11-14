//go:build integration

package credits_test

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/credits/creditstest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
)

type unavailableFinancialProvider struct {
	*creditstest.Stripe
	fail  bool
	calls int
}

func (p *unavailableFinancialProvider) RetrieveSession(ctx context.Context, id string) (credits.PaymentSession, error) {
	p.calls++
	if p.fail {
		return credits.PaymentSession{}, errors.New("private provider diagnostic")
	}
	return p.Stripe.RetrieveSession(ctx, id)
}

func TestReconcileAndRestoreFinancialOperationsDeadLetterAndRetention(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	ctx := t.Context()
	cat, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	provider := &unavailableFinancialProvider{Stripe: f.FinancialProvider()}
	svc := credits.NewServiceWithCatalogue(f.DB.Pool, nil, cat, f.Events())
	if err = svc.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: creditstest.WebhookSecret}); err != nil {
		t.Fatal(err)
	}
	const owner = "a1111111-1111-4111-8111-111111111111"
	order, err := svc.CreateCheckout(ctx, owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: cat.Config().Packs[0].SKU, PricingVersion: cat.Config().Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	// A separately funded annual learner has lost its initial notification and
	// two monthly anniversaries. Restore reconciliation must fund/catch up once.
	annualLearner, err := f.DB.Queries.CreateLearner(ctx, "Annual reconciliation fixture")
	if err != nil {
		t.Fatal(err)
	}
	annualOwner := uuid.UUID(annualLearner.ID.Bytes).String()
	annual, err := svc.CreateCheckout(ctx, annualOwner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: cat.Config().Plans[0].Yearly.SKU, PricingVersion: cat.Config().Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(time.Now().UTC().Year(), time.Now().UTC().Month()-2, 1, 0, 0, 0, 0, time.UTC)
	if err = provider.SetAnnualStart("cs_test_"+annual.ID, start); err != nil {
		t.Fatal(err)
	}
	if _, _, err = provider.Notification("cs_test_"+annual.ID, "paid", "invoice.paid"); err != nil {
		t.Fatal(err)
	}
	raw, signature, err := provider.Notification("cs_test_"+order.ID, "paid", "checkout.session.completed")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequestWithContext(ctx, "POST", "/v1/payments/stripe/webhook", bytes.NewReader(raw))
	req.Header.Set("Stripe-Signature", signature)
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var event string
	if err = f.DB.Pool.QueryRow(ctx, "SELECT event_id FROM credit_payment_inbox WHERE session_id=$1", "cs_test_"+order.ID).Scan(&event); err != nil {
		t.Fatal(err)
	}
	provider.fail = true
	for range 5 {
		if _, err = f.DB.Pool.Exec(ctx, "UPDATE credit_payment_attempts SET next_attempt_at=now()"); err != nil {
			t.Fatal(err)
		}
		if err = svc.ProcessPayments(ctx); err == nil {
			t.Fatal("provider outage succeeded")
		}
	}
	for range 3 {
		_ = svc.ProcessPayments(ctx)
	}
	if provider.calls != 5 {
		t.Fatalf("unbounded provider retries: %d", provider.calls)
	}
	var dead bool
	if err = f.DB.Pool.QueryRow(ctx, "SELECT dead_letter_at IS NOT NULL FROM credit_payment_attempts WHERE event_id=$1", event).Scan(&dead); err != nil || !dead {
		t.Fatalf("dead letter missing: %v", err)
	}
	// Restore the dead letter through encrypted financial/ storage, preserving the
	// original receipt and attempts even though diagnostics are short-lived.
	f.UseFinancialCopy(t)
	f.RunJob(t, "2026-11-14")
	restored := f.Restore(t, f.Ciphertext(t, "financial/"+f.ArchiveKey("2026-11-14")))
	replay := credits.NewServiceWithCatalogue(restored.Pool, nil, cat, f.Events())
	provider.fail = false
	if err = replay.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: creditstest.WebhookSecret}); err != nil {
		t.Fatal(err)
	}
	if err = replay.ReplayDeadLetter(ctx, event); err != nil {
		t.Fatal(err)
	}
	if err = replay.ReplayDeadLetter(ctx, event); err != nil {
		t.Fatal(err)
	}
	if _, _, err = provider.NotificationWithDetails("cs_test_"+order.ID, "partially_refunded", "charge.refunded", order.AmountMinor/2, "re_missing_notification"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = replay.ReconcileFinancialOperations(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var annualGrants int
	if err = restored.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries e JOIN credit_subscription_schedule g ON g.grant_key=e.reference JOIN credit_subscription_terms t USING(period_key) JOIN credit_subscriptions b USING(subscription_id) WHERE b.order_id=$1 AND e.kind='subscription_grant'`, annual.ID).Scan(&annualGrants); err != nil || annualGrants != 3 {
		t.Fatalf("missed annual grants=%d: %v", annualGrants, err)
	}
	var purchases, refunds int
	if err = restored.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_payment_results WHERE order_id=$1 AND state='paid'", order.ID).Scan(&purchases); err != nil {
		t.Fatal(err)
	}
	if err = restored.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_reversals WHERE order_id=$1", order.ID).Scan(&refunds); err != nil || purchases != 1 || refunds != 1 {
		t.Fatalf("business dedupe: purchases=%d refunds=%d %v", purchases, refunds, err)
	}
	if _, _, err = provider.NotificationWithDetails("cs_test_"+order.ID, "disputed", "charge.dispute.created", order.AmountMinor-order.AmountMinor/2, "dp_missing_notification"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = replay.ReconcileFinancialOperations(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err = restored.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_reversals WHERE order_id=$1", order.ID).Scan(&refunds); err != nil || refunds != 2 {
		t.Fatalf("missing dispute dedupe=%d %v", refunds, err)
	}
	if err = replay.DispatchOutbox(ctx); err != nil {
		t.Fatal(err)
	}
	// Purge only diagnostic and derived properties; never financial keys/history.
	if _, err = restored.Pool.Exec(ctx, "UPDATE credit_payment_payloads SET expires_at=now(); UPDATE credit_outbox SET created_at=now()-interval '90 days',dispatched_at=now()"); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err = restored.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries").Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if err = replay.PurgeFinancialDiagnostics(ctx); err != nil {
		t.Fatal(err)
	}
	var payloads, properties, after int
	if err = restored.Pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM credit_payment_payloads),(SELECT count(*) FROM credit_outbox WHERE properties<>'{}'),(SELECT count(*) FROM credit_entries)").Scan(&payloads, &properties, &after); err != nil || payloads != 0 || properties != 0 || retained != after {
		t.Fatalf("purge touched history: %d %d %d/%d %v", payloads, properties, retained, after, err)
	}
	if _, err = restored.Pool.Exec(ctx, `INSERT INTO credit_payment_payloads(event_id,payload,expires_at) VALUES($1,'{}',now()+interval '1 day')`, event); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Pool.Exec(ctx, `INSERT INTO credit_outbox(event_key,owner_reference,event,properties,created_at) VALUES('retention-pending',repeat('1',64),'credits_purchased','{"amount":1}',now()-interval '90 days'),('retention-young',repeat('1',64),'credits_purchased','{"amount":1}',now()-interval '89 days')`); err != nil {
		t.Fatal(err)
	}
	if err = replay.PurgeFinancialDiagnostics(ctx); err != nil {
		t.Fatal(err)
	}
	var preserved, expired bool
	if err = restored.Pool.QueryRow(ctx, `SELECT (SELECT count(*)=1 FROM credit_payment_payloads) AND EXISTS(SELECT 1 FROM credit_outbox WHERE event_key='retention-young' AND properties<>'{}'),EXISTS(SELECT 1 FROM credit_outbox WHERE event_key='retention-pending' AND properties='{}' AND dispatched_at IS NOT NULL)`).Scan(&preserved, &expired); err != nil || !preserved || !expired {
		t.Fatalf("retention boundary: young=%t expired=%t %v", preserved, expired, err)
	}
	if backup.DefaultRetention.FinancialDays != 35 {
		t.Fatal("financial snapshot policy drifted")
	}
	if err = credits.NewFinancialReplay(provider, f.Events(), time.Now)(ctx, restored.Pool); err != nil {
		t.Fatal(err)
	}
	if _, err = restored.Pool.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1) ON CONFLICT DO NOTHING", accounts.LearnerReference(owner)); err != nil {
		t.Fatal(err)
	}
	if err = accounts.ApplyDeletionTombstones(ctx, restored.Pool); err != nil {
		t.Fatal(err)
	}
	if err = credits.NewFinancialReplay(provider, f.Events(), time.Now)(ctx, restored.Pool); err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"event_id", "last_reason_code"} {
		if backup.ClassifyColumn("credit_payment_attempts", column) == backup.Unclassified {
			t.Fatal("unclassified retry column", column)
		}
	}
	if accounts.ClassifiedLearnerTables["credit_payment_attempts"] == "" {
		t.Fatal("missing retry privacy policy")
	}
	for _, statement := range []string{"DELETE FROM credit_payment_attempts", "TRUNCATE credit_payment_attempts"} {
		if _, err = restored.Pool.Exec(ctx, statement); err == nil {
			t.Fatal("retry provenance was deletable", statement)
		}
	}
	// Restored attempt state is retained and contains no private diagnostic text.
	var state string
	if err = restored.Pool.QueryRow(ctx, "SELECT row_to_json(a)::text FROM credit_payment_attempts a WHERE event_id=$1", event).Scan(&state); err != nil || strings.Contains(state, "private") {
		t.Fatal(state, err)
	}
}
