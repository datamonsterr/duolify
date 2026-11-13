package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
)

// ErrSourceDebt is returned when an AI action requires paid credits but the learner has unresolved debt.
var ErrSourceDebt = errors.New("source_debt: new paid AI blocked pending resolution")

// RefundNotification represents an incoming verified provider refund or dispute event.
type RefundNotification struct {
	ReversalKey string `json:"reversal_key"`
	OrderID     string `json:"order_id"`
	AmountMinor int    `json:"amount_minor"`
	Currency    string `json:"currency"`
	Kind        string `json:"kind"` // "refund", "chargeback", "goodwill"
	EventID     string `json:"event_id"`
	DisputeID   string `json:"dispute_id,omitempty"`
	Reason      string `json:"reason,omitempty"`
}

// CalculateRefundCredits calculates the credits to reverse proportionally to the refund minor amount.
func CalculateRefundCredits(orderCredits, orderAmountMinor, refundAmountMinor int) int64 {
	if orderAmountMinor <= 0 || refundAmountMinor <= 0 {
		return 0
	}
	if refundAmountMinor >= orderAmountMinor {
		return int64(orderCredits)
	}
	// Proportional: round to nearest integer
	return int64(math.Round(float64(orderCredits) * float64(refundAmountMinor) / float64(orderAmountMinor)))
}

// CalculateSourceDeductionAndDebt splits the reversal credits into deducted unspent and remaining debt.
func CalculateSourceDeductionAndDebt(creditsToReverse, unspentSource int64) (deducted, debt int64) {
	if creditsToReverse <= 0 {
		return 0, 0
	}
	if unspentSource <= 0 {
		return 0, creditsToReverse
	}
	if unspentSource >= creditsToReverse {
		return creditsToReverse, 0
	}
	return unspentSource, creditsToReverse - unspentSource
}

// CheckRefundCap returns true if this refund does not cause cumulative refunds to exceed original amount.
func CheckRefundCap(orderAmountMinor, alreadyRefundedMinor, thisRefundMinor int) bool {
	return alreadyRefundedMinor+thisRefundMinor <= orderAmountMinor
}

// GetSourceDebt returns the sum of unresolved source debt for an owner.
func (s *Service) GetSourceDebt(ctx context.Context, owner string) (int64, error) {
	if s.pool() == nil {
		return 0, nil
	}
	ref := accounts.LearnerReference(owner)
	var debt int64
	err := s.pool().QueryRow(ctx, `SELECT COALESCE(sum(amount), 0)
		FROM credit_debt_history
		WHERE (learner_id=$1::uuid OR owner_reference=$2) AND status='active'`, owner, ref).Scan(&debt)
	if err != nil {
		return 0, err
	}
	return debt, nil
}

// ApplyGoodwillAdjustment waives source debt for an owner with an audited operator record.
func (s *Service) ApplyGoodwillAdjustment(ctx context.Context, actorID string, owner string, sourceID int64, reason string) error {
	if s.pool() == nil {
		return errors.New("database unavailable")
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err = financialLock(ctx, tx); err != nil {
		return err
	}

	ref := accounts.LearnerReference(owner)
	result, err := tx.Exec(ctx, `UPDATE credit_debt_history
		SET status='waived_goodwill', resolved_at=now(), resolution_notes=$3
		WHERE (learner_id=$1::uuid OR owner_reference=$2) AND source_id=$4 AND status='active'`,
		owner, ref, fmt.Sprintf("operator:%s - %s", actorID, reason), sourceID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		// Also allow waiving any active debt for the owner if sourceID is 0
		if sourceID == 0 {
			_, err = tx.Exec(ctx, `UPDATE credit_debt_history
				SET status='waived_goodwill', resolved_at=now(), resolution_notes=$3
				WHERE (learner_id=$1::uuid OR owner_reference=$2) AND status='active'`,
				owner, ref, fmt.Sprintf("operator:%s - %s", actorID, reason))
			if err != nil {
				return err
			}
		}
	}

	// Audit outbox event
	auditKey := fmt.Sprintf("goodwill:%s:%d:%d", owner, sourceID, time.Now().Unix())
	if err = insertOutbox(ctx, tx, owner, auditKey, "entitlement_changed", map[string]any{
		"source":      "api",
		"actor_id":    actorID,
		"reason_code": "goodwill_adjustment",
		"reason":      reason,
	}); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return err
	}
	_ = s.InvalidateCache(ctx, owner)
	return nil
}

// ApplyRefundNotification processes a partial/full refund or chargeback transactionally.
func (s *Service) ApplyRefundNotification(ctx context.Context, n RefundNotification) error {
	if s.pool() == nil {
		return errors.New("database unavailable")
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err = financialLock(ctx, tx); err != nil {
		return err
	}

	if err = s.applyRefundNotificationTx(ctx, tx, n); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return err
	}

	var owner *string
	_ = s.pool().QueryRow(ctx, "SELECT learner_id::text FROM credit_checkout_orders WHERE id=$1", n.OrderID).Scan(&owner)
	if owner != nil {
		_ = s.InvalidateCache(ctx, *owner)
	}
	return nil
}

func (s *Service) applyRefundNotificationTx(ctx context.Context, tx pgx.Tx, n RefundNotification) error {
	if n.ReversalKey == "" || n.OrderID == "" || n.AmountMinor <= 0 {
		return errors.New("invalid reversal input")
	}
	kind := n.Kind
	if kind == "" {
		kind = "refund"
	}
	if kind != "refund" && kind != "chargeback" && kind != "goodwill" {
		return errors.New("unknown reversal kind")
	}

	// 1. Dedupe by reversal_key
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_reversals WHERE reversal_key=$1)", n.ReversalKey).Scan(&exists); err != nil {
		return err
	}
	if exists {
		// Already processed: idempotent no-op
		return nil
	}

	// 2. Lock the order row and fetch details
	var order CheckoutOrder
	var owner *string
	var ownerRef string
	err := tx.QueryRow(ctx, `SELECT id::text, learner_id::text, owner_reference, sku, pricing_version,
		amount_minor, currency, credits, platform, billing_period
		FROM credit_checkout_orders WHERE id=$1 FOR UPDATE`, n.OrderID).
		Scan(&order.ID, &owner, &ownerRef, &order.SKU, &order.PricingVersion,
			&order.AmountMinor, &order.Currency, &order.Credits, &order.Platform, &order.BillingPeriod)
	if err != nil {
		return err
	}

	// 3. Check cumulative refunded money cap
	var alreadyRefundedMinor int
	err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor), 0) FROM credit_reversals WHERE order_id=$1`, n.OrderID).Scan(&alreadyRefundedMinor)
	if err != nil {
		return err
	}
	if !CheckRefundCap(order.AmountMinor, alreadyRefundedMinor, n.AmountMinor) {
		return fmt.Errorf("cumulative refund %d exceeds order amount %d", alreadyRefundedMinor+n.AmountMinor, order.AmountMinor)
	}

	// 4. Calculate credits to reverse
	creditsToReverse := CalculateRefundCredits(order.Credits, order.AmountMinor, n.AmountMinor)

	// 5. Ensure payment inbox entry exists for foreign key
	if n.EventID == "" {
		n.EventID = "evt_" + n.ReversalKey
	}
	_, err = tx.Exec(ctx, `INSERT INTO credit_payment_inbox(event_id, environment, event_type, session_id)
		VALUES($1, 'test', $2, $3) ON CONFLICT DO NOTHING`,
		n.EventID, kind+".created", "cs_test_"+order.ID)
	if err != nil {
		return err
	}

	// 6. Locate source entry in credit_entries
	var sourceID int64
	var sourceAmount int64
	var sourceKind string
	if order.BillingPeriod == "one_time" {
		err = tx.QueryRow(ctx, `SELECT e.id, e.amount, e.kind FROM credit_entries e
			JOIN credit_payment_results r ON r.ledger_id = e.id
			WHERE r.order_id=$1 AND r.state='paid'`, order.ID).Scan(&sourceID, &sourceAmount, &sourceKind)
		if err != nil {
			// Fallback: search by reference prefix
			err = tx.QueryRow(ctx, `SELECT id, amount, kind FROM credit_entries
				WHERE kind='purchase' AND reference LIKE 'stripe:test:%' AND (learner_id=$1::uuid OR owner_reference=$2)
				ORDER BY id DESC LIMIT 1`, owner, ownerRef).Scan(&sourceID, &sourceAmount, &sourceKind)
		}
	} else {
		// Subscription order: locate subscription grant
		err = tx.QueryRow(ctx, `SELECT t.ledger_id, t.monthly_credits, 'subscription_grant'
			FROM credit_subscription_terms t
			JOIN credit_subscriptions s USING(subscription_id)
			WHERE s.order_id=$1 LIMIT 1`, order.ID).Scan(&sourceID, &sourceAmount, &sourceKind)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	var creditsDeducted int64
	var creditsDebt int64
	var ledgerID *int64

	if sourceID > 0 {
		// Calculate remaining unspent credits from this source
		var sumAllocated int64
		err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount), 0) FROM credit_entries
			WHERE provenance->>'source_id'=$1::text`, fmt.Sprint(sourceID)).Scan(&sumAllocated)
		if err != nil {
			return err
		}
		unspentSource := sourceAmount + sumAllocated
		if unspentSource < 0 {
			unspentSource = 0
		}

		creditsDeducted, creditsDebt = CalculateSourceDeductionAndDebt(creditsToReverse, unspentSource)

		// 7. Insert compensating ledger entry if unspent credits exist
		if creditsDeducted > 0 && owner != nil {
			bucket := "purchase"
			if order.BillingPeriod != "one_time" {
				bucket = "subscription"
			}
			prov, _ := json.Marshal(map[string]any{
				"source_id":    fmt.Sprint(sourceID),
				"bucket":       bucket,
				"reversal_key": n.ReversalKey,
				"amount_minor": n.AmountMinor,
				"currency":     n.Currency,
				"reason":       kind,
				"activity":     fmt.Sprintf("Refund · %s", n.ReversalKey),
			})
			var lid int64
			err = tx.QueryRow(ctx, `INSERT INTO credit_entries(learner_id, owner_reference, amount, kind, reference, pricing_version, provenance)
				VALUES($1, $2, $3, 'refund', $4, $5, $6) RETURNING id`,
				*owner, ownerRef, -creditsDeducted, n.ReversalKey, order.PricingVersion, prov).Scan(&lid)
			if err != nil {
				return err
			}
			ledgerID = &lid
		}
	} else {
		// No source entry found: entire reversal becomes debt
		creditsDebt = creditsToReverse
	}

	// 8. Record reversal in credit_reversals
	_, err = tx.Exec(ctx, `INSERT INTO credit_reversals(reversal_key, order_id, environment, event_id, kind, amount_minor, currency, credits_reversed)
		VALUES($1, $2, 'test', $3, $4, $5, $6, $7)`,
		n.ReversalKey, order.ID, n.EventID, kind, n.AmountMinor, n.Currency, creditsToReverse)
	if err != nil {
		return err
	}

	// 9. Record allocation
	if sourceID > 0 {
		_, err = tx.Exec(ctx, `INSERT INTO credit_refund_allocations(reversal_key, source_id, amount_minor, credits_deducted, credits_debt, ledger_id)
			VALUES($1, $2, $3, $4, $5, $6)`,
			n.ReversalKey, sourceID, n.AmountMinor, creditsDeducted, creditsDebt, ledgerID)
		if err != nil {
			return err
		}
	}

	// 10. Record dispute key if chargeback
	if kind == "chargeback" || n.DisputeID != "" {
		disputeKey := n.DisputeID
		if disputeKey == "" {
			disputeKey = "dp_" + n.ReversalKey
		}
		_, err = tx.Exec(ctx, `INSERT INTO credit_dispute_keys(dispute_key, reversal_key, event_id, order_id, environment, status)
			VALUES($1, $2, $3, $4, 'test', 'chargeback') ON CONFLICT (dispute_key) DO NOTHING`,
			disputeKey, n.ReversalKey, n.EventID, order.ID)
		if err != nil {
			return err
		}
	}

	// 11. Record non-spendable source debt if deficit exists
	if creditsDebt > 0 {
		bucket := "purchase"
		if order.BillingPeriod != "one_time" {
			bucket = "subscription"
		}
		var learnerUUID any
		if owner != nil {
			learnerUUID = *owner
		}
		_, err = tx.Exec(ctx, `INSERT INTO credit_debt_history(learner_id, owner_reference, reversal_key, source_id, bucket, amount, status)
			VALUES($1, $2, $3, $4, $5, $6, 'active')`,
			learnerUUID, ownerRef, n.ReversalKey, sourceID, bucket, creditsDebt)
		if err != nil {
			return err
		}
	}

	// 12. If subscription, revoke future grants and active term
	if order.BillingPeriod == "monthly" || order.BillingPeriod == "yearly" {
		var periodKey, planID, entitlementVersion string
		err = tx.QueryRow(ctx, `SELECT t.period_key, t.plan_id, t.entitlement_version
			FROM credit_subscription_terms t
			JOIN credit_subscriptions s USING(subscription_id)
			WHERE s.order_id=$1 LIMIT 1`, order.ID).Scan(&periodKey, &planID, &entitlementVersion)
		if err == nil {
			// Revoke period
			reason := "refunded"
			if kind == "chargeback" {
				reason = "disputed"
			}
			_, _ = tx.Exec(ctx, `INSERT INTO credit_subscription_revocations(period_key, event_id, reason)
				VALUES($1, $2, $3) ON CONFLICT DO NOTHING`, periodKey, n.EventID, reason)

			// Entitlement changed event
			if owner != nil {
				_ = insertOutbox(ctx, tx, *owner, periodKey+":revoked", "entitlement_changed", map[string]any{
					"source":              "api",
					"platform":            order.Platform,
					"app_version":         "development",
					"plan_id":             planID,
					"entitlement_version": entitlementVersion,
					"reason_code":         reason,
				})
			}
		}
	}

	// 13. Financial outbox event: credits_refunded
	if owner != nil {
		reasonCode := "requested_by_customer"
		if kind == "chargeback" {
			reasonCode = "dispute"
		}
		if err = insertOutbox(ctx, tx, *owner, n.ReversalKey+":credits_refunded", "credits_refunded", map[string]any{
			"source":            "api",
			"platform":          order.Platform,
			"app_version":       "development",
			"amount":            creditsToReverse,
			"payment_reference": order.ID,
			"reason_code":       reasonCode,
		}); err != nil {
			return err
		}
	}

	return nil
}
