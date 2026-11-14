package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/credits/creditstest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	dbsql "github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRestoreDrillFinancialSnapshotReplaysMissingAndDuplicateHTTPNotifications(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	provider := f.FinancialProvider()
	cat, err := pricing.Load(t.Context(), f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	svc := credits.NewServiceWithCatalogue(f.DB.Pool, nil, cat, nil)
	if err = svc.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: creditstest.WebhookSecret}); err != nil {
		t.Fatal(err)
	}
	const owner = "a1111111-1111-4111-8111-111111111111"
	order, err := svc.CreateCheckout(t.Context(), owner, credits.CheckoutInput{RequestID: uuid.NewString(), SKU: cat.Config().Packs[0].SKU, PricingVersion: cat.Config().Version, Platform: "web", ReturnTo: "/credits"})
	if err != nil {
		t.Fatal(err)
	}
	// Provider is paid before the snapshot, but its notification is lost.
	raw, signature, err := provider.Notification("cs_test_"+order.ID, "paid", "checkout.session.completed")
	if err != nil {
		t.Fatal(err)
	}
	f.UseFinancialCopy(t)
	f.RunJob(t, "2026-11-14")
	options := f.DrillOptions(t, time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC))
	options.Inspect = func(ctx context.Context, pool *pgxpool.Pool) error {
		catalogue, e := pricing.Load(ctx, dbsql.New(pool))
		if e != nil {
			return e
		}
		restored := credits.NewServiceWithCatalogue(pool, nil, catalogue, nil)
		if e = restored.ConfigurePayments(credits.PaymentOptions{Mode: "test", Provider: provider, WebhookSecret: creditstest.WebhookSecret}); e != nil {
			return e
		}
		reconciler, ok := any(restored).(interface{ ReconcileFinancialOperations(context.Context) error })
		if !ok {
			return fmt.Errorf("E7-11 canonical financial reconciler missing")
		}
		if e = reconciler.ReconcileFinancialOperations(ctx); e != nil {
			return e
		}
		before, e := restored.GetWallet(ctx, owner)
		if e != nil {
			return e
		}
		server := httptest.NewServer(restored)
		defer server.Close()
		for range 2 {
			req, e := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/payments/stripe/webhook", bytes.NewReader(raw))
			if e != nil {
				return e
			}
			req.Header.Set("Stripe-Signature", signature)
			res, e := server.Client().Do(req)
			if e != nil {
				return e
			}
			_ = res.Body.Close()
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("webhook status %d", res.StatusCode)
			}
			if e = restored.ProcessPayments(ctx); e != nil {
				return e
			}
			if e = reconciler.ReconcileFinancialOperations(ctx); e != nil {
				return e
			}
		}
		after, e := restored.GetWallet(ctx, owner)
		if e != nil {
			return e
		}
		var count int
		e = pool.QueryRow(ctx, "SELECT count(*) FROM credit_payment_results WHERE order_id=$1 AND state='paid'", order.ID).Scan(&count)
		if e != nil {
			return e
		}
		if count != 1 || before.Purchased != after.Purchased {
			return fmt.Errorf("duplicate restored payment: count=%d", count)
		}
		return nil
	}
	report, err := backup.RunDrill(t.Context(), options)
	if err != nil || !report.OK || report.FinancialReplay != "passed" {
		t.Fatalf("financial restore/replay: %v %+v", err, report)
	}
	// A schema-valid snapshot with a missing funded month must fail the same
	// ready-pipeline hook; the replay cannot silently invent a missing schedule.
	corrupt := f.DrillOptions(t, time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC))
	corrupt.BeforeAnonymize = func(ctx context.Context, pool *pgxpool.Pool) error {
		_, err := pool.Exec(ctx, `SET session_replication_role='replica'; DELETE FROM credit_subscription_schedule WHERE month_index=11; RESET session_replication_role`)
		return err
	}
	bad, err := backup.RunDrill(t.Context(), corrupt)
	if err == nil || bad.OK || bad.FinancialReplay != "failed" {
		t.Fatalf("corrupt financial schedule passed: %+v %v", bad, err)
	}
	f.AssertDrillEvents(t, []bool{true, false})
}
