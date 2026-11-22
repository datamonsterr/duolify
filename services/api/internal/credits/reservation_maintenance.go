package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/maintenance"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
)

// expireDailyTx expires the learner's past-day daily grants and returns how many it expired.
// since is the oldest local date worth looking at ("" for every grant): the sweep only needs
// recent days, while a reservation checks everything the learner ever received.
func expireDailyTx(ctx context.Context, tx pgx.Tx, owner, today, version, since string) (int, error) {
	rows, err := tx.Query(ctx, `SELECT g.id,g.provenance->>'date',GREATEST(0,(SELECT COALESCE(sum(e.amount),0) FROM credit_entries e WHERE e.learner_id=g.learner_id AND e.provenance->>'date'=g.provenance->>'date' AND (e.kind='daily_grant' OR e.provenance->>'bucket'='daily_free'))),COALESCE(g.pricing_version,$3) FROM credit_entries g WHERE g.learner_id=$1 AND g.kind='daily_grant' AND g.provenance->>'date'<$2 AND g.provenance->>'date'>=$4`, owner, today, version, since)
	if err != nil {
		return 0, err
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
			return 0, err
		}
		if a.Amount > 0 {
			pending = append(pending, a)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, a := range pending {
		if err = ledgerTx(ctx, tx, owner, "daily_expire:"+owner+":"+a.Date, a.Version, "expire", -a.Amount, map[string]any{"source_id": fmt.Sprint(a.ID), "bucket": "daily_free", "date": a.Date}); err != nil {
			return 0, err
		}
		if err = insertOutbox(ctx, tx, owner, "daily_expire:"+owner+":"+a.Date, "daily_credits_expired", map[string]any{"source": "api", "platform": "web", "app_version": "development", "amount": a.Amount}); err != nil {
			return 0, err
		}
	}
	return len(pending), nil
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
	balance := int64(0)
	if wallet, e := s.GetWallet(ctx, owner); e == nil {
		balance = wallet.Purchased + wallet.Subscription + wallet.CheckIn
		if q.DailyFreeEligible {
			balance += wallet.DailyFree.Available
		}
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
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
	_, err := s.dispatchOutbox(ctx)
	return err
}

// dispatchOutbox reports how many events it delivered. An empty outbox costs one indexed
// probe (credit_outbox_pending) and no transaction.
func (s *Service) dispatchOutbox(ctx context.Context) (int, error) {
	if s.pool() == nil {
		return 0, nil
	}
	var pending bool
	if err := s.pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_outbox WHERE dispatched_at IS NULL)`).Scan(&pending); err != nil || !pending {
		return 0, err
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `UPDATE credit_outbox o SET dispatched_at=now() WHERE event_key IN (SELECT event_key FROM credit_outbox WHERE dispatched_at IS NULL ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED) RETURNING learner_id::text,event,properties,COALESCE((SELECT analytics_opt_out FROM learner_profiles p WHERE p.learner_id=o.learner_id),true)`)
	if err != nil {
		return 0, err
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
			return 0, err
		}
		if owner != nil {
			e.owner = *owner
		}
		if err = json.Unmarshal(raw, &e.properties); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	for _, e := range events {
		if !e.optOut && e.owner != "" && s.events != nil {
			_ = s.events.Capture(e.owner, e.name, e.properties)
		}
		if e.name == "credits_purchased" || e.name == "subscription_grant_issued" || e.name == "daily_credits_granted" {
			s.NotifyCreditArrival(ctx, e.owner)
		}
	}
	return len(events), nil
}

// ReapExpired finalizes abandoned jobs after the API-enforced provider deadline.
// Unknown running usage consumes its held USD; undelivered Credits are released.
func (s *Service) ReapExpired(ctx context.Context) error {
	_, err := s.reapExpired(ctx)
	return err
}

// reapExpired reports how many jobs and orphaned owners it finalized. Both probes are
// served by partial indexes (credit_jobs_orphans, credit_jobs_deadline).
func (s *Service) reapExpired(ctx context.Context) (int, error) {
	// Older backups can contain holds whose learner was already erased. They
	// cannot deliver; finalize by retained ownership without requiring a profile.
	tag, err := s.pool().Exec(ctx, `SELECT release_deleted_credit_jobs(owner_reference) FROM
 (SELECT DISTINCT owner_reference FROM credit_jobs WHERE learner_id IS NULL AND state IN ('reserved','running')) owners`)
	if err != nil {
		return 0, err
	}
	done := int(tag.RowsAffected())
	rows, err := s.pool().Query(ctx, `SELECT id::text,learner_id::text FROM credit_jobs WHERE state IN ('reserved','running') AND deadline_at<=clock_timestamp() AND learner_id IS NOT NULL`)
	if err != nil {
		return done, err
	}
	type job struct{ id, owner string }
	var jobs []job
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.id, &j.owner); err != nil {
			rows.Close()
			return done, err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return done, err
	}
	for _, j := range jobs {
		if _, err = s.Settle(ctx, j.owner, j.id, nil); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// Daily expiry looks back a few days on every pass and over the whole ledger once a day:
// a grant older than the window was already expired by a sweep or by the learner's next
// reservation (Reserve expires every past day for that learner), so only a worker that
// was down for days leaves one behind, and the full pass finds it.
const (
	expiryWindowDays = 3
	expiryFullEvery  = 24 * time.Hour
)

// fullExpiryDue reports whether this pass must look at every grant, and records it.
func (s *Service) fullExpiryDue(asOf time.Time) bool {
	s.expiryMu.Lock()
	defer s.expiryMu.Unlock()
	if s.lastFullExpiry.IsZero() || asOf.Sub(s.lastFullExpiry) >= expiryFullEvery || asOf.Before(s.lastFullExpiry) {
		s.lastFullExpiry = asOf
		return true
	}
	return false
}

// expirePostgres expires past-day daily grants. The due learners come from the indexed
// grant-date range (credit_entries_daily_grant_date), found without the financial lock;
// each learner is then expired in its own short transaction that holds the lock only
// for its own rows, so a sweep never blocks reservations for long.
func (s *Service) expirePostgres(ctx context.Context, asOf time.Time) (int, error) {
	since := ""
	if !s.fullExpiryDue(asOf) {
		since = asOf.UTC().AddDate(0, 0, -expiryWindowDays).Format(time.DateOnly)
	}
	rows, err := s.pool().Query(ctx, `SELECT DISTINCT learner_id::text FROM credit_entries WHERE kind='daily_grant' AND learner_id IS NOT NULL AND provenance->>'date'>=$1`, since)
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
	total := 0
	var failures []error
	for _, owner := range owners {
		n, e := s.expireOwner(ctx, owner, asOf, since)
		total += n
		if e != nil {
			failures = append(failures, e)
		}
	}
	return total, errors.Join(failures...)
}

// ExpireLearnerDailyCredits expires one learner's past-day grants. Wallet reads call it
// instead of sweeping every learner.
func (s *Service) ExpireLearnerDailyCredits(ctx context.Context, owner string, asOf time.Time) (int, error) {
	if s.pool() == nil {
		return s.ExpireDailyCredits(ctx, asOf)
	}
	return s.expireOwner(ctx, owner, asOf, "")
}

func (s *Service) expireOwner(ctx context.Context, owner string, asOf time.Time, since string) (int, error) {
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return 0, err
	}
	tz, err := effectiveTimezone(ctx, tx, owner, asOf)
	if err != nil {
		return 0, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return 0, err
	}
	version := ""
	if s.catalogue != nil {
		version = s.catalogue.Config().Version
	}
	n, err := expireDailyTx(ctx, tx, owner, asOf.In(loc).Format(time.DateOnly), version, since)
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	if n > 0 {
		_ = s.InvalidateCache(ctx, owner)
	}
	return n, nil
}

// Maintain processes disposable projections and abandoned reservations. It runs only in
// the worker, and an idle pass backs off from 5 s to 30 s; the cadence controls
// scheduling only, all quote and cost limits remain config-owned. Daily expiry is not
// here: the River job periodic-daily-credits-expiry runs it, and every wallet read and
// reservation expires its own learner first.
func (s *Service) Maintain(ctx context.Context, logger *slog.Logger) {
	maintenance.Loop(ctx, maintainMin, maintainMax, func(ctx context.Context) bool {
		busy := false
		if err := s.ProcessPayments(ctx); err != nil {
			busy = true
			logger.ErrorContext(ctx, "payment inbox work remains for retry")
		}
		if n, err := s.reapExpired(ctx); err != nil {
			logger.ErrorContext(ctx, "credit job cleanup failed", "error", err)
		} else if n > 0 {
			busy = true
		}
		if n, err := s.dispatchOutbox(ctx); err != nil {
			logger.ErrorContext(ctx, "credit outbox failed", "error", err)
		} else if n > 0 {
			busy = true
		}
		return busy
	})
}

const (
	maintainMin = 5 * time.Second
	maintainMax = 30 * time.Second
)
