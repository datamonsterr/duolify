package credits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
)

// ErrSourceDebt is returned when an AI action requires paid credits but the learner has unresolved debt.
var ErrSourceDebt = errors.New("source_debt: new paid AI blocked pending resolution")

// ErrReversalEvidence is returned for missing, invalid or unattributable refund/dispute evidence.
// The caller leaves the inbox event unprocessed so it retries and can dead-letter.
var ErrReversalEvidence = errors.New("invalid refund or dispute evidence")

// ErrNotOperator rejects goodwill adjustments from accounts without an operator grant.
var ErrNotOperator = errors.New("goodwill adjustment requires an operator")

// ProviderRefund is one canonical provider refund object. Provider amount_refunded
// is cumulative per charge; each refund is its own object with its own id.
type ProviderRefund struct {
	ID          string
	AmountMinor int
	Currency    string
	Status      string
}

// ProviderDispute is one canonical provider dispute object.
type ProviderDispute struct {
	ID          string
	AmountMinor int
	Currency    string
	Status      string
}

// MoneyToCredits converts cumulative money to cumulative credits with integer
// arithmetic. Floor division means the sum of reversals never exceeds the source
// and a reversal of the full amount is exactly the source credits.
func MoneyToCredits(totalCredits, amountMinor, moneyMinor int64) int64 {
	if totalCredits <= 0 || amountMinor <= 0 || moneyMinor <= 0 {
		return 0
	}
	if moneyMinor >= amountMinor {
		return totalCredits
	}
	return totalCredits * moneyMinor / amountMinor
}

// CalculateSourceDeductionAndDebt splits a source's reversal into deducted unspent credits and debt.
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

// CheckRefundCap returns true if cumulative reversed money stays within the payment amount.
func CheckRefundCap(orderAmountMinor, alreadyRefundedMinor, thisRefundMinor int) bool {
	return thisRefundMinor > 0 && alreadyRefundedMinor+thisRefundMinor <= orderAmountMinor
}

// GetSourceDebt returns the sum of unresolved source debt for an owner.
func (s *Service) GetSourceDebt(ctx context.Context, owner string) (int64, error) {
	if s.pool() == nil {
		return 0, nil
	}
	var debt int64
	err := s.pool().QueryRow(ctx, `SELECT COALESCE(sum(amount), 0)
		FROM credit_debt_history
		WHERE (learner_id=$1::uuid OR owner_reference=$2) AND status='active'`, owner, accounts.LearnerReference(owner)).Scan(&debt)
	return debt, err
}

// hasActiveDebt reports unresolved source debt inside the caller's transaction.
func hasActiveDebt(ctx context.Context, tx pgx.Tx, owner string) (bool, error) {
	var debt bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_debt_history WHERE (learner_id=$1::uuid OR owner_reference=$2) AND status='active')`, owner, accounts.LearnerReference(owner)).Scan(&debt)
	return debt, err
}

// ApplyGoodwillAdjustment is the operator-only path that waives active source debt.
// It never creates spendable credits. The actor and free-text notes are kept in
// credit_goodwill_audit only; analytics receives a reason code and nothing else.
// A zero sourceID waives all of the owner's active debt. No match is an error.
func (s *Service) ApplyGoodwillAdjustment(ctx context.Context, actorID, owner string, sourceID int64, reasonCode, notes string) error {
	if s.pool() == nil {
		return errors.New("database unavailable")
	}
	notes = strings.TrimSpace(notes)
	if notes == "" || len(notes) > 2000 {
		return errors.New("goodwill notes required (1-2000 characters)")
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return err
	}
	var operator bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pricing_admin_grants WHERE account_id=$1::uuid)`, actorID).Scan(&operator); err != nil {
		return err
	}
	if !operator {
		return ErrNotOperator
	}
	ref := accounts.LearnerReference(owner)
	rows, err := tx.Query(ctx, `SELECT id, amount FROM credit_debt_history
		WHERE (learner_id=$1::uuid OR owner_reference=$2) AND status='active' AND ($3::bigint=0 OR source_id=$3)
		ORDER BY id FOR UPDATE`, owner, ref, sourceID)
	if err != nil {
		return err
	}
	var ids []int64
	var total int64
	for rows.Next() {
		var id, amount int64
		if err = rows.Scan(&id, &amount); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
		total += amount
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return errors.New("no active source debt matches")
	}
	sum := sha256.Sum256([]byte(actorID))
	var auditID int64
	if err = tx.QueryRow(ctx, `INSERT INTO credit_goodwill_audit(actor_hash,owner_reference,reason_code,notes,debt_ids,waived_credits)
		VALUES($1,$2,$3,$4,$5,$6) RETURNING id`, hex.EncodeToString(sum[:]), ref, reasonCode, notes, ids, total).Scan(&auditID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE credit_debt_history SET status='waived_goodwill', resolved_at=now(), goodwill_audit_id=$2 WHERE id = ANY($1)`, ids, auditID); err != nil {
		return err
	}
	if err = insertOutbox(ctx, tx, owner, fmt.Sprintf("goodwill:%d", auditID), "entitlement_changed", map[string]any{
		"source": "api", "platform": "web", "app_version": "development", "reason_code": reasonCode,
	}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	_ = s.InvalidateCache(ctx, owner)
	return nil
}

// reversalUnit is one refundable payment: a pack order, or one subscription
// funded term (invoice). Refund and dispute objects belong to exactly one unit.
type reversalUnit struct {
	orderID, owner, ownerRef, platform, pricingVersion string
	periodKey                                          string // "" for packs
	amountMinor                                        int
	currency                                           string
	subscription                                       bool
}

type reversalItem struct {
	id, kind, status string
	amountMinor      int
	currency         string
}

type reversalSource struct {
	id, amount, reversed, unspent int64
}

// reversalItems normalizes provider refunds and disputes. Missing ids, amounts
// or currencies never default; unknown statuses fail closed.
func reversalItems(refunds []ProviderRefund, disputes []ProviderDispute, currency string) ([]reversalItem, error) {
	var items []reversalItem
	seen := map[string]bool{}
	add := func(kind, id, status string, amount int, cur string, apply, skip []string) error {
		if !paymentIdentifier.MatchString(id) || seen[id] || amount <= 0 || !strings.EqualFold(cur, currency) {
			return ErrReversalEvidence
		}
		seen[id] = true
		for _, v := range skip {
			if status == v {
				return nil
			}
		}
		for _, v := range apply {
			if status == v {
				items = append(items, reversalItem{id: id, kind: kind, status: status, amountMinor: amount, currency: cur})
				return nil
			}
		}
		return ErrReversalEvidence
	}
	for _, r := range refunds {
		if err := add("refund", r.ID, r.Status, r.AmountMinor, r.Currency, []string{"succeeded"}, []string{"pending", "requires_action", "failed", "canceled"}); err != nil {
			return nil, err
		}
	}
	for _, d := range disputes {
		if err := add("chargeback", d.ID, d.Status, d.AmountMinor, d.Currency, []string{"needs_response", "under_review", "lost"}, []string{"won", "charge_refunded", "warning_needs_response", "warning_under_review", "warning_closed"}); err != nil {
			return nil, err
		}
	}
	return items, nil
}

func (u reversalUnit) bucket() string {
	if u.subscription {
		return "subscription"
	}
	return "purchase"
}

// loadSources returns only this payment's own grant ledger entries, newest first.
// No source means fail closed: a reversal is never guessed from another order.
func (u reversalUnit) loadSources(ctx context.Context, tx pgx.Tx) ([]reversalSource, error) {
	var rows pgx.Rows
	var err error
	if u.subscription {
		rows, err = tx.Query(ctx, `SELECT e.id, e.amount FROM credit_entries e
			WHERE e.kind='subscription_grant' AND (e.reference IN (SELECT grant_key FROM credit_subscription_schedule WHERE period_key=$1)
			   OR e.id=(SELECT ledger_id FROM credit_subscription_terms WHERE period_key=$1))
			ORDER BY e.id DESC`, u.periodKey)
	} else {
		rows, err = tx.Query(ctx, `SELECT e.id, e.amount FROM credit_payment_results r JOIN credit_entries e ON e.id=r.ledger_id
			WHERE r.order_id=$1 AND r.state='paid' AND e.kind='purchase' ORDER BY e.id DESC`, u.orderID)
	}
	if err != nil {
		return nil, err
	}
	var sources []reversalSource
	for rows.Next() {
		var src reversalSource
		if err = rows.Scan(&src.id, &src.amount); err != nil {
			rows.Close()
			return nil, err
		}
		sources = append(sources, src)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: no fulfilled source for payment", ErrReversalEvidence)
	}
	for i := range sources {
		src := &sources[i]
		if err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT sum(credits_deducted+credits_debt) FROM credit_refund_allocations WHERE source_id=$1),0),
			$2 + COALESCE((SELECT sum(amount) FROM credit_entries WHERE provenance->>'source_id'=$1::text),0)`, src.id, src.amount).Scan(&src.reversed, &src.unspent); err != nil {
			return nil, err
		}
	}
	return sources, nil
}

// reconcileReversals applies every provider refund/dispute not yet applied, each
// keyed by its own object id, inside the caller's transaction. It must run after
// fulfilment so out-of-order delivery still fulfils once and then reverses.
func (s *Service) reconcileReversals(ctx context.Context, tx pgx.Tx, eventID string, u reversalUnit, refunds []ProviderRefund, disputes []ProviderDispute) error {
	items, err := reversalItems(refunds, disputes, u.currency)
	if err != nil {
		return err
	}
	for _, item := range items {
		if err = s.applyReversal(ctx, tx, eventID, u, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) applyReversal(ctx context.Context, tx pgx.Tx, eventID string, u reversalUnit, item reversalItem) error {
	key := "stripe:test:" + item.id
	var applied bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_reversals WHERE reversal_key=$1)", key).Scan(&applied); err != nil || applied {
		return err
	}
	var moneyBefore int
	var creditsBefore int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount_minor),0), COALESCE(sum(credits_reversed),0) FROM credit_reversals
		WHERE order_id=$1 AND period_key IS NOT DISTINCT FROM NULLIF($2,'')`, u.orderID, u.periodKey).Scan(&moneyBefore, &creditsBefore); err != nil {
		return err
	}
	if !CheckRefundCap(u.amountMinor, moneyBefore, item.amountMinor) {
		return fmt.Errorf("%w: cumulative reversal %d exceeds payment amount %d", ErrReversalEvidence, moneyBefore+item.amountMinor, u.amountMinor)
	}
	sources, err := u.loadSources(ctx, tx)
	if err != nil {
		return err
	}
	var basis int64
	for _, src := range sources {
		basis += src.amount
	}
	// Cumulative money → cumulative credits; this item reverses the difference.
	reverse := MoneyToCredits(basis, int64(u.amountMinor), int64(moneyBefore+item.amountMinor)) - creditsBefore
	if reverse < 0 {
		reverse = 0
	}
	if _, err = tx.Exec(ctx, `INSERT INTO credit_reversals(reversal_key,order_id,environment,event_id,kind,provider_object_id,period_key,amount_minor,currency,credits_reversed)
		VALUES($1,$2,'test',$3,$4,$5,NULLIF($6,''),$7,$8,$9)`, key, u.orderID, eventID, item.kind, item.id, u.periodKey, item.amountMinor, item.currency, reverse); err != nil {
		return err
	}
	if item.kind == "chargeback" {
		if _, err = tx.Exec(ctx, `INSERT INTO credit_dispute_keys(dispute_key,reversal_key,event_id,order_id,environment,status) VALUES($1,$2,$3,$4,'test',$5)`, item.id, key, eventID, u.orderID, item.status); err != nil {
			return err
		}
	}
	remaining, moneyLeft := reverse, int64(item.amountMinor)
	type portion struct {
		src                    reversalSource
		take, deduct, debt, mm int64
	}
	var portions []portion
	for _, src := range sources {
		take := min(remaining, max(src.amount-src.reversed, 0))
		if take <= 0 {
			continue
		}
		deduct, debt := CalculateSourceDeductionAndDebt(take, src.unspent)
		portions = append(portions, portion{src: src, take: take, deduct: deduct, debt: debt})
		remaining -= take
	}
	if remaining > 0 {
		return fmt.Errorf("%w: reversal exceeds attributable source credits", ErrReversalEvidence)
	}
	if len(portions) == 0 { // sub-credit refund: retain the money allocation on the newest source
		portions = append(portions, portion{src: sources[0]})
	}
	for i := range portions {
		p := &portions[i]
		p.mm = moneyLeft
		if i < len(portions)-1 {
			p.mm = int64(item.amountMinor) * p.take / max(reverse, 1)
		}
		moneyLeft -= p.mm
	}
	for _, p := range portions {
		var ledgerID *int64
		if p.deduct > 0 {
			prov, _ := json.Marshal(map[string]any{"source_id": fmt.Sprint(p.src.id), "bucket": u.bucket(), "reversal_key": key, "reason": item.kind, "activity": "Refund"})
			var lid int64
			if err = tx.QueryRow(ctx, `INSERT INTO credit_entries(learner_id,owner_reference,amount,kind,reference,pricing_version,provenance)
				VALUES($1,$2,$3,'refund',$4,$5,$6) RETURNING id`, u.owner, u.ownerRef, -p.deduct, fmt.Sprintf("%s:%d", key, p.src.id), u.pricingVersion, prov).Scan(&lid); err != nil {
				return err
			}
			ledgerID = &lid
		}
		if _, err = tx.Exec(ctx, `INSERT INTO credit_refund_allocations(reversal_key,source_id,amount_minor,credits_deducted,credits_debt,ledger_id) VALUES($1,$2,$3,$4,$5,$6)`,
			key, p.src.id, p.mm, p.deduct, p.debt, ledgerID); err != nil {
			return err
		}
		if p.debt > 0 {
			if _, err = tx.Exec(ctx, `INSERT INTO credit_debt_history(learner_id,owner_reference,reversal_key,source_id,bucket,amount,status) VALUES($1,$2,$3,$4,$5,$6,'active')`,
				u.owner, u.ownerRef, key, p.src.id, u.bucket(), p.debt); err != nil {
				return err
			}
		}
	}
	reason := "refunded"
	reasonCode := "requested_by_customer"
	if item.kind == "chargeback" {
		reason, reasonCode = "disputed", "dispute"
	}
	if u.subscription {
		if err = revokeTerm(ctx, tx, eventID, u, reason); err != nil {
			return err
		}
	}
	if err = insertOutbox(ctx, tx, u.owner, key+":credits_refunded", "credits_refunded", map[string]any{
		"source": "api", "platform": u.platform, "app_version": "development", "amount": reverse, "payment_reference": u.orderID, "reason_code": reasonCode,
	}); err != nil {
		return err
	}
	return nil
}

// revokeTerm cancels the term's entitlement and every unissued future grant once.
func revokeTerm(ctx context.Context, tx pgx.Tx, eventID string, u reversalUnit, reason string) error {
	res, err := tx.Exec(ctx, `INSERT INTO credit_subscription_revocations(period_key,event_id,reason) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, u.periodKey, eventID, reason)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return nil
	}
	var plan, version string
	if err = tx.QueryRow(ctx, `SELECT plan_id, entitlement_version FROM credit_subscription_terms WHERE period_key=$1`, u.periodKey).Scan(&plan, &version); err != nil {
		return err
	}
	return insertOutbox(ctx, tx, u.owner, u.periodKey+":revoked", "entitlement_changed", map[string]any{
		"source": "api", "platform": u.platform, "app_version": "development", "plan_id": plan, "entitlement_version": version, "reason_code": reason,
	})
}
