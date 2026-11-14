package credits

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FinancialState measures outstanding paid Credits (including held Credits) and
// the configured cost reserve in USD nanos. No private content leaves the drill.
type FinancialState struct{ PaidLiability, PaidHeld, ReserveNanos int64 }

// VerifyFinancialState checks retained business invariants independently of the
// provider. Missing notifications are repairable; inconsistent retained history
// is a drill failure, never silently rewritten from a fixture.
func VerifyFinancialState(ctx context.Context, p *pgxpool.Pool, at time.Time) (FinancialState, error) {
	var state FinancialState
	queries := []string{
		`SELECT count(*) FROM credit_entries e LEFT JOIN pricing_versions p ON p.version=e.pricing_version WHERE e.pricing_version IS NOT NULL AND p.version IS NULL`,
		`SELECT count(*) FROM credit_payment_results r JOIN credit_entries e ON e.id=r.ledger_id JOIN credit_checkout_orders o ON o.id=r.order_id WHERE r.state='paid' AND (e.kind<>'purchase' OR e.amount<>o.credits OR e.reference<>r.payment_key OR e.pricing_version<>o.pricing_version OR NOT EXISTS(SELECT 1 FROM credit_outbox WHERE event_key=r.payment_key||':purchased'))`,
		`SELECT count(*) FROM credit_subscription_terms t JOIN credit_entries e ON e.id=t.ledger_id WHERE e.kind<>'subscription_grant' OR e.amount<>t.monthly_credits OR e.reference<>t.period_key OR e.pricing_version<>t.pricing_version OR NOT EXISTS(SELECT 1 FROM credit_outbox WHERE event_key=t.period_key||':subscription_grant_issued')`,
		`SELECT count(*) FROM credit_subscription_schedule g JOIN credit_subscription_terms t USING(period_key) JOIN credit_entries e ON e.reference=g.grant_key AND e.kind='subscription_grant' WHERE e.amount<>t.monthly_credits OR e.pricing_version<>t.pricing_version OR NOT EXISTS(SELECT 1 FROM credit_outbox WHERE event_key=g.grant_key||':subscription_grant_issued')`,
		`SELECT count(*) FROM credit_reversals r WHERE r.credits_reversed<>COALESCE((SELECT sum(credits_deducted+credits_debt) FROM credit_refund_allocations a WHERE a.reversal_key=r.reversal_key),0) OR r.amount_minor<>COALESCE((SELECT sum(amount_minor) FROM credit_refund_allocations a WHERE a.reversal_key=r.reversal_key),0)`,
		`SELECT count(*) FROM credit_refund_allocations a LEFT JOIN credit_entries e ON e.id=a.ledger_id WHERE (a.credits_deducted>0 AND (e.id IS NULL OR e.kind<>'refund' OR e.amount<>-a.credits_deducted OR e.provenance->>'source_id'<>a.source_id::text)) OR a.credits_debt<>COALESCE((SELECT sum(d.amount) FROM credit_debt_history d WHERE d.reversal_key=a.reversal_key AND d.source_id=a.source_id),0)`,
		`SELECT count(*) FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.state IN ('reserved','running') AND NOT EXISTS(SELECT 1 FROM credit_cost_counters c WHERE c.scope='learner' AND c.owner_reference=j.owner_reference AND c.day=j.local_date AND c.held_nanos>=(SELECT sum(x.held_nanos) FROM credit_jobs x WHERE x.owner_reference=j.owner_reference AND x.local_date=j.local_date AND x.state IN ('reserved','running')))`,
		`SELECT count(*) FROM credit_allocations a JOIN credit_jobs j ON j.id=a.job_id WHERE j.state IN ('reserved','running') AND NOT EXISTS(SELECT 1 FROM credit_entries e WHERE e.kind='reserve' AND e.reference=j.id::text||':'||a.component||':'||a.source_id::text AND e.amount=-a.amount)`,
		`SELECT count(*) FROM account_deletion_tombstones t JOIN learners l ON t.learner_reference=encode(sha256(convert_to(l.id::text,'UTF8')),'hex')`,
		`SELECT count(*) FROM credit_checkout_orders o JOIN account_deletion_tombstones t ON o.owner_reference=t.learner_reference WHERE o.learner_id IS NOT NULL`,
		`SELECT count(*) FROM credit_subscription_schedule g JOIN credit_subscription_revocations r USING(period_key) JOIN credit_entries e ON e.reference=g.grant_key AND e.kind='subscription_grant' WHERE e.created_at>r.created_at`,
	}
	for i, q := range queries {
		var n int64
		if err := p.QueryRow(ctx, q).Scan(&n); err != nil {
			return state, fmt.Errorf("financial check %d unavailable: %w", i, err)
		}
		if n != 0 {
			return state, fmt.Errorf("financial invariant %d failed", i)
		}
	}
	rows, err := p.Query(ctx, `SELECT DISTINCT v.config FROM pricing_versions v WHERE v.version IN (SELECT pricing_version FROM credit_entries WHERE pricing_version IS NOT NULL UNION SELECT pricing_version FROM credit_checkout_orders UNION SELECT pricing_version FROM credit_subscription_terms)`)
	if err != nil {
		return state, err
	}
	configs, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return state, err
	}
	for _, raw := range configs {
		if _, err = pricing.Parse(raw); err != nil {
			return state, ErrFinancialReconciliation
		}
	}
	if err = verifyRestoredSchedules(ctx, p); err != nil {
		return state, err
	}
	// Check every paid source's available balance plus still-held allocation.
	rows, err = p.Query(ctx, `SELECT e.id,e.amount+COALESCE((SELECT sum(c.amount) FROM credit_entries c WHERE c.provenance->>'source_id'=e.id::text),0),
 COALESCE((SELECT sum(a.amount) FROM credit_allocations a JOIN credit_jobs j ON j.id=a.job_id WHERE a.source_id=e.id AND j.state IN ('reserved','running')),0),
 ceil((v.config->'limits'->>'protected_cost_ceiling_usd_per_credit')::numeric*1000000000)::bigint
 FROM credit_entries e JOIN pricing_versions v ON v.version=e.pricing_version WHERE e.kind IN ('purchase','subscription_grant')`)
	if err != nil {
		return state, err
	}
	type source struct {
		id, available, held int64
		rate                int64
	}
	sources, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (source, error) {
		var s source
		err := r.Scan(&s.id, &s.available, &s.held, &s.rate)
		return s, err
	})
	if err != nil {
		return state, err
	}
	for _, s := range sources {
		if err = addPaidSource(&state, s.available, s.held, s.rate); err != nil {
			return state, err
		}
	}
	// Cross-check the API's displayed paid wallet against an independent signed
	// ledger sum; a stale or clamped balance cannot hide negative paid liability.
	rows, err = p.Query(ctx, `SELECT DISTINCT learner_id::text FROM credit_entries WHERE learner_id IS NOT NULL`)
	if err != nil {
		return state, err
	}
	owners, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return state, err
	}
	svc := NewService(p, nil)
	for _, owner := range owners {
		wallet, e := svc.GetWalletAt(ctx, owner, at)
		if e != nil {
			return state, e
		}
		var purchase, subscription int64
		e = p.QueryRow(ctx, `SELECT COALESCE(sum(amount) FILTER(WHERE kind='purchase' OR (kind IN ('refund','reserve','release','spend') AND COALESCE(provenance->>'bucket','purchase')='purchase')),0),
 COALESCE(sum(amount) FILTER(WHERE kind='subscription_grant' OR (kind IN ('refund','reserve','release','spend') AND provenance->>'bucket'='subscription')),0) FROM credit_entries WHERE learner_id=$1`, owner).Scan(&purchase, &subscription)
		if e != nil {
			return state, e
		}
		if wallet.Purchased != purchase || wallet.Subscription != subscription {
			return state, ErrFinancialReconciliation
		}
	}
	return state, nil
}

func addPaidSource(state *FinancialState, available, held, rate int64) error {
	if available < 0 || held < 0 || rate <= 0 || held > math.MaxInt64-available {
		return ErrFinancialReconciliation
	}
	liability := available + held
	if liability > math.MaxInt64/rate || state.PaidLiability > math.MaxInt64-liability || state.PaidHeld > math.MaxInt64-held {
		return ErrFinancialReconciliation
	}
	reserve := liability * rate
	if state.ReserveNanos > math.MaxInt64-reserve {
		return ErrFinancialReconciliation
	}
	state.PaidLiability += liability
	state.PaidHeld += held
	state.ReserveNanos += reserve
	return nil
}

func verifyRestoredSchedules(ctx context.Context, p *pgxpool.Pool) error {
	rows, err := p.Query(ctx, `SELECT t.period_key,t.service_start,t.paid_through FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id WHERE o.billing_period='yearly'`)
	if err != nil {
		return err
	}
	type term struct {
		key        string
		start, end time.Time
	}
	terms, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (term, error) {
		var t term
		err := r.Scan(&t.key, &t.start, &t.end)
		return t, err
	})
	if err != nil {
		return err
	}
	for _, t := range terms {
		rows, err = p.Query(ctx, "SELECT grant_key,month_index,due_at FROM credit_subscription_schedule WHERE period_key=$1 ORDER BY month_index", t.key)
		if err != nil {
			return err
		}
		type grant struct {
			key   string
			index int
			due   time.Time
		}
		grants, e := pgx.CollectRows(rows, func(r pgx.CollectableRow) (grant, error) {
			var g grant
			err := r.Scan(&g.key, &g.index, &g.due)
			return g, err
		})
		if e != nil {
			return e
		}
		if len(grants) != 12 {
			return ErrFinancialReconciliation
		}
		for i, g := range grants {
			if g.index != i || g.key != annualGrantKey(t.key, i) || !g.due.Equal(annualServiceMonth(t.start, i)) || !g.due.Before(t.end) {
				return ErrFinancialReconciliation
			}
		}
	}
	return nil
}

var financialReplayTables = []string{"credit_entries", "credit_checkout_orders", "credit_payment_sessions", "credit_payment_inbox", "credit_payment_results", "credit_payment_attempts", "credit_outbox", "credit_subscriptions", "credit_subscription_terms", "credit_subscription_schedule", "credit_subscription_revocations", "credit_subscription_cancellations", "credit_subscription_lifecycle", "credit_subscription_transitions", "credit_reversals", "credit_refund_allocations", "credit_dispute_keys", "credit_debt_history", "credit_goodwill_audit", "credit_jobs", "credit_allocations", "credit_usage", "credit_cost_counters", "pricing_versions", "pricing_quote_policies", "pricing_quotes", "account_deletion_tombstones"}

func financialFingerprint(ctx context.Context, p *pgxpool.Pool) (string, error) {
	h := sha256.New()
	for _, table := range financialReplayTables {
		_, _ = h.Write([]byte(table + "\n"))
		rows, err := p.Query(ctx, "SELECT to_jsonb(t)::text FROM "+pgx.Identifier{table}.Sanitize()+" t ORDER BY to_jsonb(t)::text")
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var row string
			if err = rows.Scan(&row); err != nil {
				rows.Close()
				return "", err
			}
			_, _ = h.Write([]byte(row + "\n"))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// NewFinancialReplay implements the restore-drill extension. The independently
// injected TEST provider is reconciled once; every retained row must be identical
// after a second reconciliation. Only an isolated, anonymized pool may be passed.
func NewFinancialReplay(provider FinancialProvider, events analytics.Events, now func() time.Time) func(context.Context, *pgxpool.Pool) error {
	return func(ctx context.Context, p *pgxpool.Pool) error {
		s := NewServiceWithCatalogue(p, nil, nil, events)
		fail := func(err error) error { s.reconciliationFailure(ctx, "mismatch"); return err }
		if provider == nil || now == nil {
			return fail(ErrFinancialReconciliation)
		}
		asOf := now()
		if _, err := VerifyFinancialState(ctx, p, asOf); err != nil {
			return fail(err)
		}
		if err := s.ConfigurePayments(PaymentOptions{Mode: "test", WebhookSecret: "restore-fixture-only", Provider: provider}); err != nil {
			return fail(err)
		}
		if err := s.reconcileFinancialOperationsAt(ctx, asOf); err != nil {
			return err
		}
		if _, err := VerifyFinancialState(ctx, p, asOf); err != nil {
			return fail(err)
		}
		first, err := financialFingerprint(ctx, p)
		if err != nil {
			return fail(err)
		}
		if err = s.reconcileFinancialOperationsAt(ctx, asOf); err != nil {
			return err
		}
		second, err := financialFingerprint(ctx, p)
		if err != nil {
			return fail(err)
		}
		if first != second {
			return fail(ErrFinancialReconciliation)
		}
		return nil
	}
}
