package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func applyLifecycle(p *FundedPlan, status string, cancelled bool, reason string, at time.Time) {
	p.CancelAtPeriodEnd = cancelled || status == "canceled"
	if p.Revoked {
		p.Active = false
		p.Status = reason
		return
	}
	p.Active = p.Active && at.Before(p.PaidThrough)
	switch status {
	case "active", "past_due", "canceled":
	default:
		p.Active = false
	}
	p.Status = "expired"
	if status == "past_due" {
		p.Status = "past-due"
	} else if p.Active {
		p.Status = "active"
		if p.CancelAtPeriodEnd {
			p.Status = "cancel-scheduled"
		}
	}
}

// recordLifecycle stores only bound canonical objects, never notification fields.
// Funding is deliberately independent: provider periods cannot extend benefits.
func (s *Service) recordLifecycle(ctx context.Context, tx pgx.Tx, event string, sub ProviderSubscription, failedInvoice string) error {
	if sub.Trial {
		sub.Status = "trialing"
	}
	switch sub.Status {
	case "active", "past_due", "canceled", "unpaid", "paused", "incomplete", "incomplete_expired", "trialing":
	default:
		sub.Status = "unknown"
	}
	if sub.LiveMode || !sub.PeriodEnd.After(sub.PeriodStart) {
		return errStripeEvidence
	}
	var owner *string
	var platform, plan, version, interval string
	err := tx.QueryRow(ctx, `SELECT o.learner_id::text,o.platform,t.plan_id,t.entitlement_version,o.billing_period FROM credit_subscriptions b JOIN credit_checkout_orders o ON o.id=b.order_id JOIN credit_subscription_terms t USING(subscription_id) WHERE b.subscription_id=$1 AND b.customer_id=$2 AND o.id::text=$3 AND o.owner_reference=$4 ORDER BY t.paid_through DESC LIMIT 1`, sub.ID, sub.CustomerID, sub.OrderID, sub.OwnerReference).Scan(&owner, &platform, &plan, &version, &interval)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || owner == nil {
		return err
	}
	var previousStatus string
	var previousCancelled bool
	priorErr := tx.QueryRow(ctx, `SELECT provider_status,cancel_at_period_end FROM credit_subscription_lifecycle WHERE subscription_id=$1 ORDER BY id DESC LIMIT 1`, sub.ID).Scan(&previousStatus, &previousCancelled)
	if priorErr != nil && !errors.Is(priorErr, pgx.ErrNoRows) {
		return priorErr
	}
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO credit_subscription_lifecycle(subscription_id,event_id,provider_status,period_start,period_end,cancel_at_period_end)
 SELECT $1,NULLIF($2,''),$3,$4,$5,$6 WHERE NOT EXISTS(SELECT 1 FROM (SELECT * FROM credit_subscription_lifecycle WHERE subscription_id=$1 ORDER BY id DESC LIMIT 1) l WHERE l.provider_status=$3 AND l.period_start=$4 AND l.period_end=$5 AND l.cancel_at_period_end=$6) RETURNING id`, sub.ID, event, sub.Status, sub.PeriodStart, sub.PeriodEnd, sub.CancelAtPeriodEnd).Scan(&id)
	changed := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	props := map[string]any{"source": "api", "platform": platform, "app_version": "development", "plan_id": plan, "interval": interval, "channel": "stripe", "entitlement_version": version, "billing_period_start": sub.PeriodStart.UTC().Format(time.RFC3339), "billing_period_end": sub.PeriodEnd.UTC().Format(time.RFC3339)}
	if changed && (sub.CancelAtPeriodEnd || sub.Status == "canceled") && !previousCancelled && previousStatus != "canceled" {
		if err = insertOutbox(ctx, tx, *owner, fmt.Sprintf("lifecycle:%d:cancelled", id), "subscription_cancelled", props); err != nil {
			return err
		}
	}
	// Period observations do not duplicate the funded term's entitlement event.
	if changed && priorErr == nil && (previousStatus != sub.Status || previousCancelled != sub.CancelAtPeriodEnd) {
		if err = insertOutbox(ctx, tx, *owner, fmt.Sprintf("lifecycle:%d:entitlement", id), "entitlement_changed", props); err != nil {
			return err
		}
	}
	if failedInvoice != "" && sub.Status == "past_due" {
		props["reason_code"] = "payment_failed"
		if err = insertOutbox(ctx, tx, *owner, "stripe:test:"+failedInvoice+":failed", "subscription_payment_failed", props); err != nil {
			return err
		}
	}
	_ = s.InvalidateCache(ctx, *owner)
	return nil
}

func (s *Service) decorateLifecycle(ctx context.Context, owner string, p *FundedPlan, at time.Time) error {
	var status, reason string
	var cancelled bool
	err := s.pool().QueryRow(ctx, `SELECT COALESCE(l.provider_status,'active'),COALESCE(l.cancel_at_period_end,EXISTS(SELECT 1 FROM credit_subscription_cancellations c WHERE c.period_key=t.period_key)),COALESCE(r.reason,'') FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id LEFT JOIN LATERAL(SELECT * FROM credit_subscription_lifecycle WHERE subscription_id=b.subscription_id ORDER BY id DESC LIMIT 1) l ON true LEFT JOIN credit_subscription_revocations r USING(period_key) WHERE o.learner_id=$1 ORDER BY t.paid_through DESC LIMIT 1`, owner).Scan(&status, &cancelled, &reason)
	if err != nil {
		return err
	}
	applyLifecycle(p, status, cancelled, reason, at)
	var raw []byte
	var effective time.Time
	err = s.pool().QueryRow(ctx, `SELECT x.terms,x.effective_at FROM credit_subscription_transitions x JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id WHERE o.learner_id=$1 AND x.kind='plan' AND NOT EXISTS(SELECT 1 FROM credit_subscription_terms t WHERE t.subscription_id=x.subscription_id AND t.service_start>=x.effective_at) ORDER BY x.effective_at DESC LIMIT 1`, owner).Scan(&raw, &effective)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var terms transitionTerms
	if err = json.Unmarshal(raw, &terms); err != nil {
		return err
	}
	p.PendingPlan = &PendingPlan{Name: terms.Plan.Name, EffectiveAt: effective}
	return nil
}

// ReconcileSubscriptions recovers missed notifications from canonical provider
// state. It never infers funding from status; invoices enter the existing durable
// verification path and retain the same period/grant/outbox business keys.
func (s *Service) ReconcileSubscriptions(ctx context.Context) error {
	provider, ok := s.payments.Provider.(SubscriptionProvider)
	if s.pool() == nil || s.payments.Mode != "test" || !ok {
		return nil
	}
	rows, err := s.pool().Query(ctx, `SELECT b.subscription_id FROM credit_subscriptions b JOIN credit_checkout_orders o ON o.id=b.order_id WHERE o.learner_id IS NOT NULL ORDER BY b.subscription_id`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		sub, e := provider.RetrieveSubscription(callCtx, id)
		cancel()
		if e != nil {
			return e
		}
		if sub.ID != id || sub.LiveMode {
			return errStripeEvidence
		}
		if e = s.reconcileSubscription(ctx, sub); e != nil {
			return e
		}
	}
	return s.ProcessPayments(ctx)
}
func (s *Service) reconcileSubscription(ctx context.Context, sub ProviderSubscription) error {
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return err
	}
	if err = s.recordLifecycle(ctx, tx, "", sub, ""); err != nil {
		return err
	}
	if sub.LatestInvoiceID != "" {
		if !paymentIdentifier.MatchString(sub.LatestInvoiceID) {
			return errStripeEvidence
		}
		// One synthetic durable observation per invoice; normal notifications still
		// independently converge on the same funded business key. Failed invoices
		// stay eligible for subsequent daily reconciliation after provider recovery.
		key := fmt.Sprintf("reconcile_%s_%s", sub.LatestInvoiceID, time.Now().UTC().Format("20060102"))
		if _, err = tx.Exec(ctx, `INSERT INTO credit_payment_inbox(event_id,environment,event_type,session_id) VALUES($1,'test','invoice.updated',$2) ON CONFLICT DO NOTHING`, key, sub.LatestInvoiceID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
