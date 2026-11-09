package credits

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
)

func expireDailyTx(ctx context.Context, tx pgx.Tx, owner, today, version string) error {
	rows, err := tx.Query(ctx, `SELECT g.id,g.provenance->>'date',GREATEST(0,(SELECT COALESCE(sum(e.amount),0) FROM credit_entries e WHERE e.learner_id=g.learner_id AND e.provenance->>'date'=g.provenance->>'date' AND (e.kind='daily_grant' OR e.provenance->>'bucket'='daily_free'))),COALESCE(g.pricing_version,$3) FROM credit_entries g WHERE g.learner_id=$1 AND g.kind='daily_grant' AND g.provenance->>'date'<$2`, owner, today, version)
	if err != nil {
		return err
	}
	type expiration struct {
		allocation
		Version string
	}
	var pending []expiration
	for rows.Next() {
		var a expiration
		if err = rows.Scan(&a.ID, &a.Date, &a.Amount, &a.Version); err != nil {
			rows.Close()
			return err
		}
		if a.Amount > 0 {
			pending = append(pending, a)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, a := range pending {
		if err = ledgerTx(ctx, tx, owner, "daily_expire:"+owner+":"+a.Date, a.Version, "expire", -a.Amount, map[string]any{"source_id": fmt.Sprint(a.ID), "bucket": "daily_free", "date": a.Date}); err != nil {
			return err
		}
		if err = insertOutbox(ctx, tx, owner, "daily_expire:"+owner+":"+a.Date, "daily_credits_expired", map[string]any{"source": "api", "platform": "web", "app_version": "development", "amount": a.Amount}); err != nil {
			return err
		}
	}
	return nil
}
func insertOutbox(ctx context.Context, tx pgx.Tx, owner, key, event string, properties map[string]any) error {
	raw, err := json.Marshal(properties)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO credit_outbox(event_key,learner_id,owner_reference,event,properties) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, key, owner, accounts.LearnerReference(owner), event, raw)
	return err
}
func outboxTx(ctx context.Context, tx pgx.Tx, owner, key, event string, q pricing.Quote, amount int64) error {
	return insertOutbox(ctx, tx, owner, key, event, map[string]any{"source": "api", "platform": "web", "app_version": "development", "feature": q.Action, "pricing_version": q.PricingVersion, "amount": amount, "model": q.Rate.Model})
}
func (s *Service) refusal(ctx context.Context, owner, id string, q pricing.Quote) error {
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	balance := int64(0)
	if wallet, e := s.GetWallet(ctx, owner); e == nil {
		balance = wallet.Purchased + wallet.Subscription
		if q.DailyFreeEligible {
			balance += wallet.DailyFree.Available
		}
	}
	properties := map[string]any{"source": "api", "platform": "web", "app_version": "development", "feature": q.Action, "pricing_version": q.PricingVersion, "balance": balance, "estimate": q.ContentCeiling + q.AudioCeiling, "bucket": "eligible"}
	for _, event := range []string{"credits_insufficient", "credits_exhausted"} {
		if err = insertOutbox(ctx, tx, owner, id+":"+event, event, properties); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// DispatchOutbox delivers each recorded event once through the nonblocking wrapper.
// Operational accounting never depends on this best-effort derived projection.
func (s *Service) DispatchOutbox(ctx context.Context) error {
	if s.pool() == nil {
		return nil
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `UPDATE credit_outbox o SET dispatched_at=now() WHERE event_key IN (SELECT event_key FROM credit_outbox WHERE dispatched_at IS NULL ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED) RETURNING learner_id::text,event,properties,COALESCE((SELECT analytics_opt_out FROM learner_profiles p WHERE p.learner_id=o.learner_id),true)`)
	if err != nil {
		return err
	}
	type event struct {
		owner, name string
		properties  map[string]any
		optOut      bool
	}
	var events []event
	for rows.Next() {
		var owner *string
		var e event
		var raw []byte
		if err = rows.Scan(&owner, &e.name, &raw, &e.optOut); err != nil {
			rows.Close()
			return err
		}
		if owner != nil {
			e.owner = *owner
		}
		if err = json.Unmarshal(raw, &e.properties); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	for _, e := range events {
		if !e.optOut && e.owner != "" && s.events != nil {
			_ = s.events.Capture(e.owner, e.name, e.properties)
		}
	}
	return nil
}

// ReapExpired finalizes abandoned jobs after the API-enforced provider deadline.
// Unknown running usage consumes its held USD; undelivered Credits are released.
func (s *Service) ReapExpired(ctx context.Context) error {
	rows, err := s.pool().Query(ctx, `SELECT id::text,learner_id::text FROM credit_jobs WHERE state IN ('reserved','running') AND deadline_at<=clock_timestamp() AND learner_id IS NOT NULL`)
	if err != nil {
		return err
	}
	type job struct{ id, owner string }
	var jobs []job
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.id, &j.owner); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if _, err = s.Settle(ctx, j.owner, j.id, nil); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) expirePostgres(ctx context.Context, asOf time.Time) (int, error) {
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT learner_id::text FROM credit_entries WHERE kind='daily_grant' AND learner_id IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	var owners []string
	for rows.Next() {
		var owner string
		if err = rows.Scan(&owner); err != nil {
			rows.Close()
			return 0, err
		}
		owners = append(owners, owner)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	var before, after int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE kind='expire'").Scan(&before); err != nil {
		return 0, err
	}
	for _, owner := range owners {
		tz, e := s.store.GetEffectiveTimezone(ctx, owner, asOf)
		if e != nil {
			return 0, e
		}
		loc, e := time.LoadLocation(tz)
		if e != nil {
			return 0, e
		}
		version := ""
		if s.catalogue != nil {
			version = s.catalogue.Config().Version
		}
		if err = expireDailyTx(ctx, tx, owner, asOf.In(loc).Format(time.DateOnly), version); err != nil {
			return 0, err
		}
	}
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE kind='expire'").Scan(&after); err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	for _, owner := range owners {
		_ = s.InvalidateCache(ctx, owner)
	}
	return after - before, nil
}

// Maintain processes disposable projections and abandoned reservations. The tick
// controls worker scheduling only; all quote and cost limits remain config-owned.
func (s *Service) Maintain(ctx context.Context, logger *slog.Logger) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	expiryTick := time.NewTicker(time.Minute)
	defer expiryTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-expiryTick.C:
			if err := s.PrunePaymentPayloads(ctx); err != nil {
				logger.ErrorContext(ctx, "payment payload cleanup failed")
			}
			if _, err := s.ExpireDailyCredits(ctx, time.Now()); err != nil {
				logger.ErrorContext(ctx, "daily credit cleanup failed", "error", err)
			}
		case <-tick.C:
			if err := s.ProcessPayments(ctx); err != nil {
				logger.ErrorContext(ctx, "payment inbox work remains for retry")
			}
			if err := s.ReapExpired(ctx); err != nil {
				logger.ErrorContext(ctx, "credit job cleanup failed", "error", err)
			}
			if err := s.DispatchOutbox(ctx); err != nil {
				logger.ErrorContext(ctx, "credit outbox failed", "error", err)
			}
		}
	}
}
