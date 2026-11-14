package credits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func financialObservationID(kind, id string, raw []byte) string {
	sum := sha256.Sum256(append([]byte(kind+":"+id+":"), raw...))
	return "reconcile_" + hex.EncodeToString(sum[:])
}

// ReconcileFinancialOperations repairs lost notifications from canonical TEST
// inventory. Existing fulfillment verifies account, currency, price, funds and
// period, and commits grants/reversals/outbox atomically under business keys.
func (s *Service) ReconcileFinancialOperations(ctx context.Context) error {
	return s.reconcileFinancialOperationsAt(ctx, time.Now())
}

func (s *Service) reconcileFinancialOperationsAt(ctx context.Context, asOf time.Time) error {
	if s.pool() == nil || s.payments.Mode != "test" {
		return nil
	}
	provider, ok := s.payments.Provider.(FinancialProvider)
	if !ok {
		s.reconciliationFailure(ctx, "provider_unavailable")
		return ErrFinancialReconciliation
	}
	var failures []error
	inventory := map[string]bool{}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		call, cancel := context.WithTimeout(ctx, 5*time.Second)
		objects, err := provider.FinancialObjects(call, after, 100)
		cancel()
		if err != nil {
			s.reconciliationFailure(ctx, "provider_unavailable")
			return ErrFinancialReconciliation
		}
		if len(objects) > 100 {
			s.reconciliationFailure(ctx, "invalid_evidence")
			return ErrFinancialReconciliation
		}
		for _, object := range objects {
			if object.Key() <= after || !paymentIdentifier.MatchString(object.ID) {
				s.reconciliationFailure(ctx, "invalid_evidence")
				return ErrFinancialReconciliation
			}
			after = object.Key()
			inventory[object.Key()] = true
			if err = s.reconcileFinancialObject(ctx, provider, object); err != nil {
				failures = append(failures, err)
			}
		}
		if len(objects) < 100 {
			break
		}
	}
	// Compare in both directions. A provider page that silently omits a locally
	// retained payment/term is an unresolved difference, not successful recovery.
	rows, err := s.pool().Query(ctx, `SELECT 'session:'||session_id FROM credit_payment_sessions
 UNION SELECT 'invoice:'||invoice_id FROM credit_subscription_terms
 UNION SELECT 'subscription:'||subscription_id FROM credit_subscriptions`)
	if err != nil {
		s.reconciliationFailure(ctx, "mismatch")
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		s.reconciliationFailure(ctx, "mismatch")
		return err
	}
	for _, key := range keys {
		if !inventory[key] {
			s.reconciliationFailure(ctx, "mismatch")
			failures = append(failures, ErrFinancialReconciliation)
			break
		}
	}
	// Catch up missed funded service months. Revoked and deleted terms are
	// excluded by the same authoritative scheduler used by the normal worker.
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := s.IssueAnnualAllowances(ctx, asOf)
		if err != nil {
			s.reconciliationFailure(ctx, "mismatch")
			failures = append(failures, err)
			break
		}
		if n < 100 {
			break
		}
	}
	if len(failures) > 0 {
		return ErrFinancialReconciliation
	}
	s.financialMonitor(ctx).Incident(ctx, "dependency", "financial-reconciliation", true)
	return nil
}

func (s *Service) reconcileFinancialObject(ctx context.Context, p FinancialProvider, o FinancialObject) error {
	call, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var canonical any
	var kind, order string
	var err error
	switch o.Kind {
	case "session":
		var v PaymentSession
		v, err = p.RetrieveSession(call, o.ID)
		canonical = v
		order = v.OrderID
		kind = "checkout.session.completed"
		if err == nil && (v.ID != o.ID || v.LiveMode) {
			err = errStripeEvidence
		}
	case "invoice":
		var v SubscriptionInvoice
		v, err = p.RetrieveInvoice(call, o.ID)
		canonical = v
		order = v.OrderID
		kind = "invoice.updated"
		if err == nil && (v.ID != o.ID || v.LiveMode) {
			err = errStripeEvidence
		}
	case "subscription":
		var v ProviderSubscription
		v, err = p.RetrieveSubscription(call, o.ID)
		canonical = v
		order = v.OrderID
		kind = "customer.subscription.updated"
		if err == nil && (v.ID != o.ID || v.LiveMode) {
			err = errStripeEvidence
		}
	default:
		err = errStripeEvidence
	}
	if err != nil {
		reason := "provider_unavailable"
		if errors.Is(err, errStripeEvidence) {
			reason = "invalid_evidence"
		}
		s.reconciliationFailure(ctx, reason)
		return ErrFinancialReconciliation
	}
	// The canonical inventory may contain historical objects belonging to an
	// erased account. Retained terms remain, but a replay never resurrects it.
	var deleted bool
	if err = s.pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_checkout_orders WHERE id::text=$1 AND learner_id IS NULL)", order).Scan(&deleted); err != nil {
		s.reconciliationFailure(ctx, "mismatch")
		return err
	}
	if deleted {
		return nil
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return err
	}
	id := financialObservationID(o.Kind, o.ID, raw)
	if _, err = s.pool().Exec(ctx, `INSERT INTO credit_payment_inbox(event_id,environment,event_type,session_id) VALUES($1,'test',$2,$3) ON CONFLICT DO NOTHING`, id, kind, o.ID); err != nil {
		s.reconciliationFailure(ctx, "mismatch")
		return err
	}
	if err = s.processInboxEvent(ctx, id); err != nil {
		s.reconciliationFailure(ctx, "provider_unavailable")
		return ErrFinancialReconciliation
	}
	var resolved bool
	if err = s.pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=$1 AND state NOT IN ('rejected','failed'))", id).Scan(&resolved); err != nil {
		s.reconciliationFailure(ctx, "mismatch")
		return err
	}
	if !resolved {
		s.reconciliationFailure(ctx, "invalid_evidence")
		return ErrFinancialReconciliation
	}
	if err = s.verifyCanonicalOutcome(ctx, canonical); err != nil {
		s.reconciliationFailure(ctx, "mismatch")
		return ErrFinancialReconciliation
	}
	return nil
}

func (s *Service) verifyCanonicalOutcome(ctx context.Context, canonical any) error {
	var paid bool
	var refunds []ProviderRefund
	var disputes []ProviderDispute
	var order, currency, period string
	var good bool
	switch v := canonical.(type) {
	case PaymentSession:
		if v.Mode != "payment" {
			return nil
		}
		paid = v.PaymentStatus == "paid"
		refunds = v.Refunds
		disputes = v.Disputes
		order = v.OrderID
		currency = v.Currency
		if paid {
			if err := s.pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_payment_results r JOIN credit_entries e ON e.id=r.ledger_id JOIN credit_checkout_orders o ON o.id=r.order_id
 WHERE r.order_id::text=$1 AND r.payment_key=$2 AND r.state='paid' AND e.kind='purchase' AND e.amount=o.credits AND e.reference=r.payment_key)`, order, "stripe:test:"+v.PaymentID).Scan(&good); err != nil {
				return err
			}
		}
	case SubscriptionInvoice:
		paid = v.Status == "paid"
		refunds = v.Refunds
		disputes = v.Disputes
		order = v.OrderID
		currency = v.Currency
		period = fmt.Sprintf("stripe:test:%s:%d:%d:0", v.SubscriptionID, v.PeriodStart.Unix(), v.PeriodEnd.Unix())
		if paid {
			if err := s.pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) JOIN credit_entries e ON e.id=t.ledger_id
 WHERE t.period_key=$1 AND t.invoice_id=$2 AND b.order_id::text=$3 AND e.amount=t.monthly_credits AND e.reference=t.period_key AND t.amount_minor=$4)`, period, v.ID, order, v.AmountMinor).Scan(&good); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	if paid && !good {
		return ErrFinancialReconciliation
	}
	items, err := reversalItems(refunds, disputes, currency)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err = s.pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_reversals WHERE reversal_key=$1 AND order_id::text=$2 AND COALESCE(period_key,'')=$3 AND amount_minor=$4 AND upper(currency)=upper($5) AND kind=$6)`, "stripe:test:"+item.id, order, period, item.amountMinor, item.currency, item.kind).Scan(&good); err != nil {
			return err
		}
		if !good {
			return ErrFinancialReconciliation
		}
	}
	return nil
}
