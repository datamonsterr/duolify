package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func annualServiceMonth(start time.Time, index int) time.Time {
	start = start.UTC()
	return serviceBoundary(start.Year(), start.Month()+time.Month(index), start)
}
func annualGrantKey(term string, index int) string {
	return strings.TrimSuffix(term, ":0") + fmt.Sprintf(":%d", index)
}
func annualFundingMatches(i SubscriptionInvoice, s ProviderSubscription, r PaymentRequest, now time.Time) bool {
	if s.Interval != "year" || s.IntervalCount != 1 || i.PeriodStart.UTC().Month() != s.BillingAnchor.UTC().Month() || !i.PeriodEnd.Equal(serviceBoundary(i.PeriodStart.UTC().Year()+1, i.PeriodStart.UTC().Month(), s.BillingAnchor)) || s.PeriodEnd.Before(i.PeriodEnd) {
		return false
	}
	// Apply the existing exact-funds/account/price/month-end checks to the first
	// service month while independently validating the full funded annual period.
	s.Interval = "month"
	i.PeriodEnd = serviceBoundary(i.PeriodStart.UTC().Year(), i.PeriodStart.UTC().Month()+1, s.BillingAnchor)
	return monthlyFundingMatches(i, s, r, now)
}

func recordAnnualCancellation(ctx context.Context, tx pgx.Tx, event string, sub ProviderSubscription) error {
	if !sub.CancelAtPeriodEnd {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO credit_subscription_cancellations(period_key,event_id)
 SELECT t.period_key,$2 FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id
 WHERE b.subscription_id=$1 AND b.environment='test' AND b.customer_id=$3 AND o.id::text=$4 AND o.owner_reference=$5 AND o.billing_period='yearly'
 ON CONFLICT DO NOTHING`, sub.ID, event, sub.CustomerID, sub.OrderID, sub.OwnerReference)
	return err
}

func (s *Service) revokeAnnualTerm(ctx context.Context, tx pgx.Tx, event string, i SubscriptionInvoice, sub ProviderSubscription, r PaymentRequest, order CheckoutOrder, owner string) error {
	// Full-term revocation must identify previously verified funding exactly. A
	// changed status may be legitimate, but identity, amounts and service periods
	// still have to match the canonical term and original configured order.
	reason := "refunded"
	if i.Disputed {
		reason = "disputed"
	}
	checked := i
	checked.Refunded = false
	checked.Disputed = false
	canonical := sub
	canonical.Status = "active"
	canonical.Trial = false
	if !annualFundingMatches(checked, canonical, r, time.Now()) {
		return paymentResult(ctx, tx, event, order.ID, "rejected", "", 0)
	}
	var term, plan, version string
	err := tx.QueryRow(ctx, `SELECT t.period_key,t.plan_id,t.entitlement_version FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) WHERE t.invoice_id=$1 AND t.subscription_id=$2 AND b.order_id=$3 AND t.service_start=$4 AND t.paid_through=$5`, i.ID, sub.ID, order.ID, i.PeriodStart, i.PeriodEnd).Scan(&term, &plan, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return paymentResult(ctx, tx, event, order.ID, "rejected", "", 0)
	}
	if err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `INSERT INTO credit_subscription_revocations(period_key,event_id,reason) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, term, event, reason)
	if err != nil {
		return err
	}
	if result.RowsAffected() > 0 {
		if err = insertOutbox(ctx, tx, owner, term+":revoked", "entitlement_changed", map[string]any{"source": "api", "platform": order.Platform, "app_version": "development", "plan_id": plan, "entitlement_version": version, "reason_code": reason}); err != nil {
			return err
		}
	}
	if err = paymentResult(ctx, tx, event, order.ID, "pending", "", 0); err != nil {
		return err
	}
	_ = s.InvalidateCache(ctx, owner)
	return nil
}

// IssueAnnualAllowances catches up a bounded batch from durable funded schedules.
// FinancialLock serializes grants with inbox revocation and initial funding. Every
// ledger/outbox pair commits together, with DB-unique service-month business keys.
func (s *Service) IssueAnnualAllowances(ctx context.Context, asOf time.Time) (int, error) {
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT g.grant_key,g.period_key,g.month_index,g.due_at,t.paid_through,t.monthly_credits,t.pricing_version,t.plan_id,t.subscription_id,t.invoice_id,o.learner_id::text,o.owner_reference,o.platform
 FROM credit_subscription_schedule g JOIN credit_subscription_terms t USING(period_key) JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id
 WHERE g.due_at<=$1 AND g.due_at>=t.service_start AND g.due_at<t.paid_through AND o.learner_id IS NOT NULL AND o.billing_period='yearly' AND b.environment='test'
 AND NOT EXISTS(SELECT 1 FROM credit_subscription_revocations r WHERE r.period_key=t.period_key)
 AND NOT EXISTS(SELECT 1 FROM credit_entries e WHERE e.kind='subscription_grant' AND e.reference=g.grant_key)
 ORDER BY g.due_at,g.grant_key LIMIT 100`, asOf.UTC())
	if err != nil {
		return 0, err
	}
	type grant struct {
		key, term                                                        string
		month                                                            int
		due, end                                                         time.Time
		amount                                                           int
		version, plan, subscription, invoice, owner, reference, platform string
	}
	var pending []grant
	for rows.Next() {
		var g grant
		if err = rows.Scan(&g.key, &g.term, &g.month, &g.due, &g.end, &g.amount, &g.version, &g.plan, &g.subscription, &g.invoice, &g.owner, &g.reference, &g.platform); err != nil {
			rows.Close()
			return 0, err
		}
		pending = append(pending, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, g := range pending {
		provenance, _ := json.Marshal(map[string]any{"bucket": "subscription", "provider": "stripe", "environment": "test", "subscription_id": g.subscription, "invoice_id": g.invoice, "plan_id": g.plan, "period_key": g.key, "funded_term": g.term, "month_index": g.month, "service_start": g.due, "paid_through": g.end})
		if _, err = tx.Exec(ctx, `INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance) VALUES($1,$2,$3,'subscription_grant',$4,$5,$6)`, g.owner, g.reference, g.amount, g.key, g.version, provenance); err != nil {
			return 0, err
		}
		if err = insertOutbox(ctx, tx, g.owner, g.key+":subscription_grant_issued", "subscription_grant_issued", map[string]any{"source": "api", "platform": g.platform, "app_version": "development", "plan_id": g.plan, "amount": g.amount, "period_key": g.key, "price_table_version": g.version}); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	for _, g := range pending {
		_ = s.InvalidateCache(ctx, g.owner)
	}
	return len(pending), nil
}
