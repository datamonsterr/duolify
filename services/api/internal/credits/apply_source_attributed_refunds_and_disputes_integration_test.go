//go:build integration

package credits_test

import (
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

func TestApplySourceAttributedRefundsAndDisputesIntegration(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("reversals_test"),
		postgres.WithUsername("fixture"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
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

	learner, err := db.Queries.CreateLearner(ctx, "ReversalLearner")
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.UUID(learner.ID.Bytes).String()
	if _, err = db.Pool.Exec(ctx, "INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1,false)", owner); err != nil {
		t.Fatal(err)
	}

	pack := cfg.Packs[0] // 400 credits, $5.00
	provider := &lifecycleProvider{
		monthlyProvider: monthlyProvider{
			packProvider: packProvider{sessions: map[string]credits.PaymentSession{}},
		},
		transitions: map[string]credits.SubscriptionTransition{},
	}
	events := new(packEvents)
	svc := credits.NewServiceWithCatalogue(db.Pool, nil, cat, events)
	if err = svc.ConfigurePayments(credits.PaymentOptions{
		Mode:          "test",
		Provider:      provider,
		WebhookSecret: "test-only-webhook-secret",
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Purchase Pack A
	orderA, err := svc.CreateCheckout(ctx, owner, credits.CheckoutInput{
		RequestID:      uuid.NewString(),
		SKU:            pack.SKU,
		PricingVersion: cfg.Version,
		Platform:       "web",
		ReturnTo:       "/credits",
	})
	if err != nil {
		t.Fatal(err)
	}

	sessionA := credits.PaymentSession{
		ID:             "cs_test_" + orderA.ID,
		OrderID:        orderA.ID,
		OwnerReference: accounts.LearnerReference(owner),
		SKU:            orderA.SKU,
		PriceID:        cfg.ProviderMappings("development")[orderA.SKU],
		AmountMinor:    orderA.AmountMinor,
		Currency:       orderA.Currency,
		Mode:           "payment",
		Status:         "complete",
		PaymentStatus:  "paid",
		PaymentID:      "pi_" + orderA.ID,
	}
	provider.sessions[sessionA.ID] = sessionA

	notify := func(id, kind, object string) {
		t.Helper()
		raw := []byte(fmt.Sprintf(`{"id":%q,"type":%q,"livemode":false,"data":{"object":{"id":%q}}}`, id, kind, object))
		req := httptest.NewRequestWithContext(ctx, "POST", "/v1/payments/stripe/webhook", strings.NewReader(string(raw)))
		req.Header.Set("Stripe-Signature", signedPack(raw, time.Now()))
		w := httptest.NewRecorder()
		svc.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("webhook returned %d: %s", w.Code, w.Body.String())
		}
		if err := svc.ProcessPayments(ctx); err != nil {
			t.Fatalf("processPayments failed: %v", err)
		}
	}

	notify("evt_paid_"+orderA.ID, "checkout.session.completed", sessionA.ID)

	wallet, err := svc.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Purchased != int64(pack.Credits) {
		t.Fatalf("expected %d purchased credits, got %d", pack.Credits, wallet.Purchased)
	}

	// 2. Partly spend from Pack A (e.g. 140 credits)
	var sourceAEntryID int64
	if err := db.Pool.QueryRow(ctx, "SELECT id FROM credit_entries WHERE learner_id=$1 AND kind='purchase' AND reference=$2", owner, "stripe:test:"+sessionA.PaymentID).Scan(&sourceAEntryID); err != nil {
		t.Fatal(err)
	}

	// Insert spend entry linked to sourceAEntryID
	_, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, owner_reference, amount, kind, reference, pricing_version, provenance)
		VALUES($1, $2, -140, 'spend', 'lesson_job_1', $3, jsonb_build_object('source_id', $4::text, 'bucket', 'purchase'))`,
		owner, accounts.LearnerReference(owner), cfg.Version, fmt.Sprintf("%d", sourceAEntryID))
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.InvalidateCache(ctx, owner)

	wallet, err = svc.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Purchased != 260 {
		t.Fatalf("expected 260 purchased credits remaining, got %d", wallet.Purchased)
	}

	// 3. Purchase unrelated Pack B (1350 credits for $15.00)
	packB := cfg.Packs[1]
	orderB, err := svc.CreateCheckout(ctx, owner, credits.CheckoutInput{
		RequestID:      uuid.NewString(),
		SKU:            packB.SKU,
		PricingVersion: cfg.Version,
		Platform:       "web",
		ReturnTo:       "/credits",
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionB := credits.PaymentSession{
		ID:             "cs_test_" + orderB.ID,
		OrderID:        orderB.ID,
		OwnerReference: accounts.LearnerReference(owner),
		SKU:            orderB.SKU,
		PriceID:        cfg.ProviderMappings("development")[orderB.SKU],
		AmountMinor:    orderB.AmountMinor,
		Currency:       orderB.Currency,
		Mode:           "payment",
		Status:         "complete",
		PaymentStatus:  "paid",
		PaymentID:      "pi_" + orderB.ID,
	}
	provider.sessions[sessionB.ID] = sessionB
	notify("evt_paid_"+orderB.ID, "checkout.session.completed", sessionB.ID)

	wallet, err = svc.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	// Pack A (260) + Pack B (1350) = 1610
	if wallet.Purchased != 1610 {
		t.Fatalf("expected 1610 purchased credits, got %d", wallet.Purchased)
	}

	// 4. Test Concurrency & Reversal Key Deduping:
	// Concurrent refund notifications with the exact same reversal key must only apply ONCE.
	reversalKey1 := "stripe:test:re_part_1:refund"
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_ = svc.ApplyRefundNotification(ctx, credits.RefundNotification{
				ReversalKey: reversalKey1,
				OrderID:     orderA.ID,
				AmountMinor: 250, // partial: half of $5.00 = 200 credits
				Currency:    orderA.Currency,
				Kind:        "refund",
				EventID:     fmt.Sprintf("evt_refund_part1_%d", idx),
			})
		}(i)
	}
	wg.Wait()

	_ = svc.InvalidateCache(ctx, owner)
	wallet, err = svc.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	// Pack A had 260 unspent, reversed 200 -> 60 remaining unspent.
	// Pack B has 1350 untouched.
	// Total purchased = 60 + 1350 = 1410!
	if wallet.Purchased != 1410 {
		t.Fatalf("expected 1410 purchased credits after partial refund, got %d", wallet.Purchased)
	}

	// 5. Test Cumulative Cap:
	// Refunding $4.00 more on orderA ($2.50 already refunded, total would be $6.50 > $5.00) must be rejected!
	err = svc.ApplyRefundNotification(ctx, credits.RefundNotification{
		ReversalKey: "stripe:test:re_part_excess:refund",
		OrderID:     orderA.ID,
		AmountMinor: 400,
		Currency:    orderA.Currency,
		Kind:        "refund",
		EventID:     "evt_refund_excess",
	})
	if err == nil {
		t.Fatal("expected excess cumulative refund to be rejected")
	}

	// 6. Deplete Pack A's remaining 60 credits
	_, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, owner_reference, amount, kind, reference, pricing_version, provenance)
		VALUES($1, $2, -60, 'spend', 'lesson_job_2', $3, jsonb_build_object('source_id', $4::text, 'bucket', 'purchase'))`,
		owner, accounts.LearnerReference(owner), cfg.Version, fmt.Sprintf("%d", sourceAEntryID))
	if err != nil {
		t.Fatal(err)
	}
	_ = svc.InvalidateCache(ctx, owner)

	// Now Pack A unspent = 0.
	// 7. Force reversal / chargeback on Pack A for remaining $2.50 (200 credits):
	// Attributable credits = 200. Unspent = 0.
	// Deficit = 200 credits -> records non-spendable source debt!
	err = svc.ApplyRefundNotification(ctx, credits.RefundNotification{
		ReversalKey: "stripe:test:dp_chargeback_1:chargeback",
		OrderID:     orderA.ID,
		AmountMinor: 250,
		Currency:    orderA.Currency,
		Kind:        "chargeback",
		EventID:     "evt_dispute_1",
	})
	if err != nil {
		t.Fatalf("chargeback failed: %v", err)
	}

	// Verify Pack B balance remains completely preserved (1350 credits)!
	_ = svc.InvalidateCache(ctx, owner)
	wallet, err = svc.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Purchased != 1350 {
		t.Fatalf("expected Pack B balance 1350 preserved, got %d", wallet.Purchased)
	}

	// Verify debt history has 200 credits debt
	debt, err := svc.GetSourceDebt(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if debt != 200 {
		t.Fatalf("expected 200 source debt, got %d", debt)
	}

	// 8. Verify Paid AI reservation is BLOCKED while debt is active
	quotes := pricing.NewQuotes(db.Pool)
	q, err := quotes.Create(ctx, owner, pricing.QuoteInput{
		Action: "lesson_content",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = quotes.Accept(ctx, owner, q.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Reserve(ctx, owner, q.ID)
	if err == nil {
		t.Fatal("expected paid reservation to be blocked when debt exists")
	}

	// 9. Verify Daily free credits are NOT blocked
	// Ensure a daily free grant exists
	_, err = svc.EnsureDailyGrant(ctx, owner, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	freeQuote, err := quotes.Create(ctx, owner, pricing.QuoteInput{
		Action: "quick_note",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = quotes.Accept(ctx, owner, freeQuote.ID); err != nil {
		t.Fatal(err)
	}
	// quick_note is daily-free eligible and costs 2 credits (well within daily free allowance)
	freeRes, err := svc.Reserve(ctx, owner, freeQuote.ID)
	if err != nil {
		t.Fatalf("expected daily-free reservation to succeed despite debt, got: %v", err)
	}
	if freeRes.State != "reserved" {
		t.Fatalf("expected state reserved, got %s", freeRes.State)
	}
	if _, err = svc.Settle(ctx, owner, freeRes.ID, nil); err != nil {
		t.Fatalf("failed to settle free reservation: %v", err)
	}

	// 10. Operator Goodwill Adjustment
	// Operator waives the 200 credits debt
	operatorID := uuid.NewString()
	err = svc.ApplyGoodwillAdjustment(ctx, operatorID, owner, sourceAEntryID, "Customer goodwill resolution")
	if err != nil {
		t.Fatalf("goodwill adjustment failed: %v", err)
	}

	debt, err = svc.GetSourceDebt(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if debt != 0 {
		t.Fatalf("expected 0 source debt after goodwill adjustment, got %d", debt)
	}

	// Now paid reservation can proceed
	q2, err := quotes.Create(ctx, owner, pricing.QuoteInput{
		Action: "lesson_content",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = quotes.Accept(ctx, owner, q2.ID); err != nil {
		t.Fatal(err)
	}
	paidRes, err := svc.Reserve(ctx, owner, q2.ID)
	if err != nil {
		t.Fatalf("expected paid reservation to succeed after debt resolution, got: %v", err)
	}
	if paidRes.State != "reserved" {
		t.Fatalf("expected state reserved, got %s", paidRes.State)
	}
}
