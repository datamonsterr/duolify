package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"
)

// Financial guards fail closed without involving analytics or optional caches.
var (
	ErrConcurrency  = errors.New("concurrency_limit")
	ErrBudget       = errors.New("spend_cap")
	ErrInsufficient = errors.New("insufficient_credits")
	ErrReservation  = errors.New("reservation_unavailable")
	ErrUsage        = errors.New("usage_outside_quote")
)

// Reservation is an idempotent provider job and its durable final result.
type Reservation struct {
	ID             string `json:"id"`
	PricingVersion string `json:"pricing_version"`
	State          string `json:"state"`
	ContentSpent   int64  `json:"content_spent"`
	AudioSpent     int64  `json:"audio_spent"`
	CostNanos      int64  `json:"cost_nanos"`
}

func (s *Service) pool() *pgxpool.Pool {
	if store, ok := s.store.(*pgStore); ok {
		return store.pool
	}
	return nil
}
func financialLock(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('credits:financial',0))")
	return err
}

// ReserveAction binds a feature to its own quote before acquiring funds.
func (s *Service) ReserveAction(ctx context.Context, owner, quoteID, action string) (Reservation, error) {
	if s.pool() == nil {
		return Reservation{}, ErrReservation
	}
	q, err := pricing.NewQuotes(s.pool()).Get(ctx, owner, quoteID)
	if err != nil || q.Action != action {
		return Reservation{}, ErrReservation
	}
	return s.Reserve(ctx, owner, quoteID)
}

// Reserve allocates eligible sources, a job and operational capacity in one transaction.
func (s *Service) Reserve(ctx context.Context, owner, quoteID string) (Reservation, error) {
	if s.pool() == nil {
		return Reservation{}, ErrReservation
	}
	if err := s.ReapExpired(ctx); err != nil {
		return Reservation{}, err
	}
	_, err := s.EnsureDailyGrant(ctx, owner, time.Now())
	if err != nil {
		return Reservation{}, err
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return Reservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return Reservation{}, err
	}
	q, now, err := readQuote(ctx, tx, owner, quoteID)
	if err != nil {
		return Reservation{}, err
	}
	var existing []byte
	err = tx.QueryRow(ctx, "SELECT result FROM credit_jobs WHERE quote_id=$1", quoteID).Scan(&existing)
	if err == nil {
		var result Reservation
		err = json.Unmarshal(existing, &result)
		return result, err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, err
	}
	if !q.Accepted || !now.Before(q.ExpiresAt) || q.Bounds["max_attempts"] <= 0 || q.Bounds["timeout_ms"] <= 0 || q.Bounds["concurrency"] <= 0 {
		return Reservation{}, ErrReservation
	}
	tz, err := s.store.GetEffectiveTimezone(ctx, owner, now)
	if err != nil {
		return Reservation{}, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return Reservation{}, err
	}
	localDay := now.In(loc).Format(time.DateOnly)
	platformDay := now.UTC().Format(time.DateOnly)
	ref := accounts.LearnerReference(owner)
	if err = expireDailyTx(ctx, tx, owner, localDay, q.PricingVersion); err != nil {
		return Reservation{}, err
	}
	var paid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_entries WHERE learner_id=$1 AND kind IN ('purchase','subscription_grant'))`, owner).Scan(&paid)
	if err != nil {
		return Reservation{}, err
	}
	concurrency := q.Bounds["concurrency"]
	// Benefits expire with the funded term; an old non-expiring credit grant is
	// no longer evidence of an active plan. Honor its original configured limit.
	err = tx.QueryRow(ctx, `SELECT COALESCE((SELECT t.concurrent_generation FROM credit_subscription_terms t JOIN credit_subscriptions s USING(subscription_id) JOIN credit_checkout_orders o ON o.id=s.order_id WHERE o.learner_id=$1 AND t.service_start<=now() AND t.paid_through>now() AND COALESCE((SELECT l.provider_status FROM credit_subscription_lifecycle l WHERE l.subscription_id=s.subscription_id ORDER BY l.id DESC LIMIT 1),'active') IN ('active','past_due','canceled') AND NOT EXISTS(SELECT 1 FROM credit_subscription_revocations r WHERE r.period_key=t.period_key) ORDER BY t.paid_through DESC LIMIT 1),$2::bigint)`, owner, concurrency).Scan(&concurrency)
	if err != nil {
		return Reservation{}, err
	}
	var active int64
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state IN ('reserved','running')", owner).Scan(&active); err != nil {
		return Reservation{}, err
	}
	hold := reservationCost(q)
	if hold < 0 || hold > q.Bounds["max_cost_nanos"] {
		return Reservation{}, ErrBudget
	}
	capNanos := q.Bounds["free_day_nanos"]
	if paid {
		capNanos = q.Bounds["paid_day_nanos"]
	}
	for _, counter := range []struct {
		scope, ref, day string
		cap             int64
	}{{"learner", ref, localDay, capNanos}, {"platform", "platform", platformDay, q.Bounds["platform_day_nanos"]}} {
		_, err = tx.Exec(ctx, `INSERT INTO credit_cost_counters(scope,owner_reference,day) VALUES($1,$2,$3::date) ON CONFLICT DO NOTHING`, counter.scope, counter.ref, counter.day)
		if err != nil {
			return Reservation{}, err
		}
		var total int64
		if err = tx.QueryRow(ctx, "SELECT held_nanos+spent_nanos FROM credit_cost_counters WHERE scope=$1 AND owner_reference=$2 AND day=$3::date", counter.scope, counter.ref, counter.day).Scan(&total); err != nil {
			return Reservation{}, err
		}
		if hold > counter.cap-total {
			return Reservation{}, ErrBudget
		}
	}
	result := Reservation{PricingVersion: q.PricingVersion, State: "reserved"}
	err = tx.QueryRow(ctx, `INSERT INTO credit_jobs(quote_id,learner_id,owner_reference,state,local_date,platform_date,held_nanos,deadline_at) VALUES($1,$2,$3,'reserved',$4::date,$5::date,$6,$7) RETURNING id::text`, quoteID, owner, ref, localDay, platformDay, hold, now.Add(time.Duration(q.Bounds["timeout_ms"])*time.Millisecond)).Scan(&result.ID)
	if err != nil {
		return Reservation{}, err
	}
	if err = allocateTx(ctx, tx, owner, result.ID, q, localDay); err != nil {
		if errors.Is(err, ErrInsufficient) {
			// Persist the refusal event independently, without committing a partial job.
			_ = tx.Rollback(ctx)
			_ = s.refusal(ctx, owner, quoteID, q)
		}
		return Reservation{}, err
	}
	if active >= concurrency {
		return Reservation{}, ErrConcurrency
	}
	raw, _ := json.Marshal(result)
	if _, err = tx.Exec(ctx, "UPDATE credit_jobs SET result=$2 WHERE id=$1", result.ID, raw); err != nil {
		return Reservation{}, err
	}
	for _, counter := range []struct{ scope, ref, day string }{{"learner", ref, localDay}, {"platform", "platform", platformDay}} {
		if _, err = tx.Exec(ctx, "UPDATE credit_cost_counters SET held_nanos=held_nanos+$4 WHERE scope=$1 AND owner_reference=$2 AND day=$3::date", counter.scope, counter.ref, counter.day, hold); err != nil {
			return Reservation{}, err
		}
	}
	if err = outboxTx(ctx, tx, owner, result.ID+":reserved", "credits_reserved", q, q.ContentCeiling+q.AudioCeiling); err != nil {
		return Reservation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Reservation{}, err
	}
	_ = s.InvalidateCache(ctx, owner)
	return result, nil
}
func readQuote(ctx context.Context, tx pgx.Tx, owner, id string) (pricing.Quote, time.Time, error) {
	var q pricing.Quote
	var raw []byte
	var now time.Time
	var accepted bool
	var expiry time.Time
	err := tx.QueryRow(ctx, `SELECT snapshot,expires_at,clock_timestamp(),EXISTS(SELECT 1 FROM pricing_quote_consents WHERE quote_id=q.id) FROM pricing_quotes q WHERE id=$1 AND learner_id=$2`, id, owner).Scan(&raw, &expiry, &now, &accepted)
	if err != nil {
		return q, now, ErrReservation
	}
	err = json.Unmarshal(raw, &q)
	q.ExpiresAt = expiry
	q.Accepted = accepted
	q.ID = id
	return q, now, err
}

type allocation struct {
	ID                      int64
	Component, Bucket, Date string
	Amount                  int64
}

func allocateTx(ctx context.Context, tx pgx.Tx, owner, job string, q pricing.Quote, day string) error {
	rows, err := tx.Query(ctx, `SELECT e.id,e.kind,COALESCE(e.provenance->>'date',''), e.amount+COALESCE((SELECT sum(x.amount) FROM credit_entries x WHERE x.learner_id=e.learner_id AND x.provenance->>'source_id'=e.id::text),0) AS remaining
 FROM credit_entries e WHERE e.learner_id=$1 AND e.kind IN ('daily_grant','subscription_grant','purchase')
 ORDER BY CASE e.kind WHEN 'daily_grant' THEN 0 ELSE 1 END,e.created_at,e.id`, owner)
	if err != nil {
		return err
	}
	var sources []allocation
	for rows.Next() {
		var a allocation
		var kind string
		if err = rows.Scan(&a.ID, &kind, &a.Date, &a.Amount); err != nil {
			rows.Close()
			return err
		}
		a.Bucket = map[string]string{"daily_grant": "daily_free", "subscription_grant": "subscription", "purchase": "purchase"}[kind]
		if a.Amount > 0 && (a.Bucket != "daily_free" || (q.DailyFreeEligible && a.Date == day)) {
			sources = append(sources, a)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Honor pre-E7-03 ledger debits lacking per-source links, too.
	bucketAvailable := map[string]int64{}
	for _, bucket := range []string{"daily_free", "subscription", "purchase"} {
		var available int64
		err = tx.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE learner_id=$1 AND kind!='reward' AND
  (CASE WHEN kind='daily_grant' THEN 'daily_free' WHEN kind='subscription_grant' THEN 'subscription' WHEN kind='purchase' THEN 'purchase' ELSE COALESCE(provenance->>'bucket','purchase') END)=$2 AND ($2!='daily_free' OR provenance->>'date'=$3)`, owner, bucket, day).Scan(&available)
		if err != nil {
			return err
		}
		bucketAvailable[bucket] = available
	}
	for _, component := range []struct {
		name   string
		amount int64
	}{{"content", q.ContentCeiling}, {"audio", q.AudioCeiling}} {
		remaining := component.amount
		for i := range sources {
			a := &sources[i]
			take := min(remaining, a.Amount, bucketAvailable[a.Bucket])
			if take <= 0 {
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO credit_allocations(job_id,source_id,component,bucket,grant_date,amount) VALUES($1,$2,$3,$4,$5,$6)`, job, a.ID, component.name, a.Bucket, a.Date, take); err != nil {
				return err
			}
			prov := map[string]any{"source_id": fmt.Sprint(a.ID), "bucket": a.Bucket, "date": a.Date, "component": component.name}
			if err = ledgerTx(ctx, tx, owner, job+":"+component.name+":"+fmt.Sprint(a.ID), q.PricingVersion, "reserve", -take, prov); err != nil {
				return err
			}
			remaining -= take
			a.Amount -= take
			bucketAvailable[a.Bucket] -= take
		}
		if remaining > 0 {
			return ErrInsufficient
		}
	}
	return nil
}
func ledgerTx(ctx context.Context, tx pgx.Tx, owner, reference, version, kind string, amount int64, provenance map[string]any) error {
	raw, err := json.Marshal(provenance)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance) VALUES($1,$2,$3,$4,$5,$6)`, owner, amount, kind, reference, version, raw)
	return err
}

// Claim prevents duplicate workers from starting a provider call for one reservation.
func (s *Service) Claim(ctx context.Context, owner, id string) error {
	if s.pool() == nil {
		return ErrReservation
	}
	// Resolve priority from funded config every time, including while waiting.
	// Reservation already acquired concurrency and USD capacity; this ordering
	// cannot increase either bound. Equal-priority jobs can run concurrently.
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		tx, err := s.pool().Begin(ctx)
		if err != nil {
			return err
		}
		if err = financialLock(ctx, tx); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		command, err := tx.Exec(ctx, `WITH waiting AS (
          SELECT j.id,EXISTS(SELECT 1 FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id
            WHERE o.learner_id=j.learner_id AND t.service_start<=now() AND t.paid_through>now() AND t.benefits ? 'Priority generation queue'
            AND NOT EXISTS(SELECT 1 FROM credit_subscription_revocations r WHERE r.period_key=t.period_key)
            AND COALESCE((SELECT l.provider_status FROM credit_subscription_lifecycle l WHERE l.subscription_id=b.subscription_id ORDER BY l.id DESC LIMIT 1),'active') IN ('active','past_due','canceled')) AS priority
          FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.state='reserved' AND j.deadline_at>clock_timestamp() AND q.snapshot->>'action' IN ('preview','outline','lesson_content','outline_replan')
        ) UPDATE credit_jobs SET state='running',result=jsonb_set(result,'{state}','"running"')
          WHERE id=$1 AND learner_id=$2 AND state='reserved' AND deadline_at>clock_timestamp()
          AND (SELECT count(*) FROM credit_jobs running WHERE running.learner_id=$2 AND running.state='running') < COALESCE((SELECT t.concurrent_generation FROM credit_subscription_terms t JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id WHERE o.learner_id=$2 AND t.service_start<=now() AND t.paid_through>now() AND NOT EXISTS(SELECT 1 FROM credit_subscription_revocations r WHERE r.period_key=t.period_key) AND COALESCE((SELECT l.provider_status FROM credit_subscription_lifecycle l WHERE l.subscription_id=b.subscription_id ORDER BY l.id DESC LIMIT 1),'active') IN ('active','past_due','canceled') ORDER BY t.paid_through DESC LIMIT 1),(SELECT (q.snapshot->'bounds'->>'concurrency')::integer FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.id=$1))
          AND (NOT EXISTS(SELECT 1 FROM waiting WHERE id=$1) OR EXISTS(SELECT 1 FROM waiting WHERE id=$1 AND priority) OR NOT EXISTS(SELECT 1 FROM waiting WHERE priority))`, id, owner)
		if err == nil {
			err = tx.Commit(ctx)
		} else {
			_ = tx.Rollback(ctx)
		}
		if err != nil {
			return err
		}
		if command.RowsAffected() == 1 {
			return nil
		}
		var eligible bool
		if err = s.pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_jobs WHERE id=$1 AND learner_id=$2 AND state='reserved' AND deadline_at>clock_timestamp())`, id, owner).Scan(&eligible); err != nil {
			return err
		}
		if !eligible {
			return ErrReservation
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Limits returns trusted bounds for the engine; HTTP input cannot override them.
func (s *Service) Limits(ctx context.Context, owner, id, action string) (*enginev1.UsageLimits, error) {
	if s.pool() == nil {
		return nil, ErrReservation
	}
	var raw []byte
	if err := s.pool().QueryRow(ctx, `SELECT q.snapshot FROM credit_jobs j JOIN pricing_quotes q ON q.id=j.quote_id WHERE j.id=$1 AND j.learner_id=$2 AND j.state IN ('reserved','running')`, id, owner).Scan(&raw); err != nil {
		return nil, ErrReservation
	}
	var q pricing.Quote
	if json.Unmarshal(raw, &q) != nil || q.Action != action {
		return nil, ErrReservation
	}
	fallback := q.Policy.Models[q.Policy.FallbackModels[q.Input.Model]]
	toUint32 := func(n int64) (uint32, error) {
		if n < 0 || n > math.MaxUint32 {
			return 0, ErrUsage
		}
		return uint32(n), nil
	}
	input, err := toUint32(q.Bounds["input_tokens"])
	if err != nil {
		return nil, err
	}
	output, err := toUint32(q.Bounds["output_tokens"])
	if err != nil {
		return nil, err
	}
	timeout, err := toUint32(q.Bounds["timeout_ms"])
	if err != nil {
		return nil, err
	}
	attempts, err := toUint32(q.Bounds["max_attempts"])
	if err != nil {
		return nil, err
	}
	return &enginev1.UsageLimits{InputTokens: input, OutputTokens: output, TimeoutMs: timeout, MaxAttempts: attempts, MaxCostNanos: nonnegativeUint64(min(reservationCost(q), q.Bounds["max_cost_nanos"])), InputNanos: nonnegativeUint64(max(q.Rate.Input, fallback.Input)), OutputNanos: nonnegativeUint64(max(q.Rate.Output, fallback.Output)), CacheWriteNanos: nonnegativeUint64(max(q.Rate.CacheWrite, fallback.CacheWrite)), CacheReadNanos: nonnegativeUint64(max(q.Rate.CacheRead, fallback.CacheRead)), Provider: q.Rate.Provider, Model: q.Rate.Model, FallbackProvider: fallback.Provider, FallbackModel: fallback.Model}, nil
}

// Release cancels only queued work; it cannot release another executor's running job.
func (s *Service) Release(ctx context.Context, owner, id string) (Reservation, error) {
	return s.finalize(ctx, owner, id, nil, true)
}

// Settle is an internal trusted engine consumer; no public usage submission route exists.
func (s *Service) Settle(ctx context.Context, owner, id string, usage []*enginev1.Usage) (Reservation, error) {
	return s.finalize(ctx, owner, id, usage, false)
}
func (s *Service) finalize(ctx context.Context, owner, id string, usage []*enginev1.Usage, cancel bool) (Reservation, error) {
	if s.pool() == nil {
		return Reservation{}, ErrReservation
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return Reservation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = financialLock(ctx, tx); err != nil {
		return Reservation{}, err
	}
	var quoteID, state, localDay, platformDay string
	var hold int64
	var saved []byte
	err = tx.QueryRow(ctx, `SELECT quote_id::text,state,local_date::text,platform_date::text,held_nanos,result FROM credit_jobs WHERE id=$1 AND learner_id=$2 FOR UPDATE`, id, owner).Scan(&quoteID, &state, &localDay, &platformDay, &hold, &saved)
	if err != nil {
		return Reservation{}, ErrReservation
	}
	if state == "settled" || state == "released" {
		var result Reservation
		err = json.Unmarshal(saved, &result)
		return result, err
	}
	if cancel && state != "reserved" {
		return Reservation{}, ErrReservation
	}
	q, now, err := readQuote(ctx, tx, owner, quoteID)
	if err != nil {
		return Reservation{}, err
	}
	_, validationErr := CalculateSettlement(q, usage)
	// Keep the complete provider attempt log; duplicate delivered artifacts are free.
	rawUsage := make([]json.RawMessage, 0, len(usage))
	billable := make([]*enginev1.Usage, 0, len(usage))
	for _, u := range usage {
		if u == nil {
			return Reservation{}, ErrUsage
		}
		raw, e := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(u)
		if e != nil {
			return Reservation{}, e
		}
		rawUsage = append(rawUsage, raw)
		// Copy without copying protobuf's internal mutex.
		copyU := new(enginev1.Usage)
		if e = protojson.Unmarshal(raw, copyU); e != nil {
			return Reservation{}, e
		}
		if validationErr == nil && u.Delivered && u.ArtifactId != "" {
			command, e := tx.Exec(ctx, `INSERT INTO credit_artifacts(owner_reference,artifact_id,component,job_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, accounts.LearnerReference(owner), u.ArtifactId, u.Component, id)
			if e != nil {
				return Reservation{}, e
			}
			if command.RowsAffected() == 0 {
				copyU.Delivered = false
			}
		}
		billable = append(billable, copyU)
	}
	result, err := CalculateSettlement(q, billable)
	if err != nil {
		validationErr = err
		result = Reservation{State: "released", PricingVersion: q.PricingVersion, CostNanos: hold}
	}
	result.ID = id
	if len(usage) == 0 && state == "running" {
		result.CostNanos = hold
	}
	if result.CostNanos > hold {
		return Reservation{}, ErrUsage
	}
	rows, err := tx.Query(ctx, "SELECT source_id,component,bucket,grant_date,amount FROM credit_allocations WHERE job_id=$1 ORDER BY id", id)
	if err != nil {
		return result, err
	}
	var allocations []allocation
	for rows.Next() {
		var a allocation
		if err = rows.Scan(&a.ID, &a.Component, &a.Bucket, &a.Date, &a.Amount); err != nil {
			rows.Close()
			return result, err
		}
		allocations = append(allocations, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	tz, err := s.store.GetEffectiveTimezone(ctx, owner, now)
	if err != nil {
		return result, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return result, err
	}
	today := now.In(loc).Format(time.DateOnly)
	left := map[string]int64{"content": result.ContentSpent, "audio": result.AudioSpent}
	var returned int64
	for _, a := range allocations {
		reference := id + ":" + a.Component + ":" + fmt.Sprint(a.ID)
		provenance := map[string]any{"source_id": fmt.Sprint(a.ID), "component": a.Component, "bucket": a.Bucket, "date": a.Date}
		spent := min(a.Amount, left[a.Component])
		left[a.Component] -= spent
		// Release the whole hold before appending spend. The transaction is indivisible.
		if err = ledgerTx(ctx, tx, owner, reference, q.PricingVersion, "release", a.Amount, provenance); err != nil {
			return result, err
		}
		if spent > 0 {
			if err = ledgerTx(ctx, tx, owner, reference, q.PricingVersion, "spend", -spent, provenance); err != nil {
				return result, err
			}
		}
		unused := a.Amount - spent
		returned += unused
		if a.Bucket == "daily_free" && a.Date < today && unused > 0 {
			if err = ledgerTx(ctx, tx, owner, reference+":expired", q.PricingVersion, "expire", -unused, provenance); err != nil {
				return result, err
			}
		}
	}
	raw, _ := json.Marshal(result)
	rawAttempts, _ := json.Marshal(rawUsage)
	if _, err = tx.Exec(ctx, `INSERT INTO credit_usage(job_id,usage,cost_nanos) VALUES($1,$2,$3)`, id, rawAttempts, result.CostNanos); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE credit_jobs SET state=$2,result=$3,finished_at=$4 WHERE id=$1`, id, result.State, raw, now); err != nil {
		return result, err
	}
	for _, counter := range []struct{ scope, ref, day string }{{"learner", accounts.LearnerReference(owner), localDay}, {"platform", "platform", platformDay}} {
		if _, err = tx.Exec(ctx, `UPDATE credit_cost_counters SET held_nanos=held_nanos-$4,spent_nanos=spent_nanos+$5 WHERE scope=$1 AND owner_reference=$2 AND day=$3::date`, counter.scope, counter.ref, counter.day, hold, result.CostNanos); err != nil {
			return result, err
		}
	}
	if result.State == "settled" {
		properties := map[string]any{"source": "api", "platform": "web", "app_version": "development", "feature": q.Action, "pricing_version": q.PricingVersion, "amount": result.ContentSpent + result.AudioSpent}
		var input, output uint64
		for _, u := range usage {
			if u.Delivered && !u.Cached {
				input += uint64(u.InputTokens)
				output += uint64(u.OutputTokens)
				properties["model"] = u.Model
			}
		}
		properties["input_tokens"], properties["output_tokens"] = input, output
		if err = insertOutbox(ctx, tx, owner, id+":settled", "credits_settled", properties); err != nil {
			return result, err
		}
	}
	if returned > 0 {
		if err = outboxTx(ctx, tx, owner, id+":released", "credits_released", q, returned); err != nil {
			return result, err
		}
	}
	if len(usage) == 0 && state == "running" {
		if err = insertOutbox(ctx, tx, owner, id+":usage:unavailable", "$ai_generation", map[string]any{"source": "api", "platform": "web", "app_version": "development", "feature": q.Action, "pricing_version": q.PricingVersion, "provider": q.Rate.Provider, "model": q.Rate.Model, "usage_available": false, "succeeded": false, "provider_cost_nanos": hold, "error_code": "timeout"}); err != nil {
			return result, err
		}
	}
	for i, u := range usage {
		properties := map[string]any{"source": "api", "platform": "web", "app_version": "development", "feature": q.Action, "pricing_version": q.PricingVersion, "provider": u.Provider, "model": u.Model, "input_tokens": u.InputTokens, "output_tokens": u.OutputTokens, "cache_write_tokens": u.CacheWriteTokens, "cache_read_tokens": u.CacheReadTokens, "narration_characters": u.NarrationCharacters, "fallback_used": u.FallbackUsed, "attempt": u.Attempt, "latency_ms": u.LatencyMs, "succeeded": u.Delivered && validationErr == nil, "usage_available": !u.UsageUnavailable}
		if u.ErrorCode != "" {
			properties["error_code"] = u.ErrorCode
		}
		if priced, e := CalculateSettlement(q, []*enginev1.Usage{u}); e == nil {
			properties["provider_cost_nanos"] = priced.CostNanos
		}
		if err = insertOutbox(ctx, tx, owner, fmt.Sprintf("%s:usage:%d", id, i), "$ai_generation", properties); err != nil {
			return result, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return result, err
	}
	_ = s.InvalidateCache(ctx, owner)
	return result, validationErr
}

func (s *Service) handleReserve(w http.ResponseWriter, r *http.Request) {
	owner := accounts.LearnerID(r.Context())
	result, err := s.Reserve(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		next := time.Time{}
		if wallet, e := s.GetWallet(r.Context(), owner); e == nil {
			next = wallet.DailyFree.ResetsAt
		}
		code := http.StatusConflict
		if errors.Is(err, ErrInsufficient) {
			code = http.StatusPaymentRequired
		}
		respondJSON(w, code, map[string]any{"error": err.Error(), "next_grant_at": next, "top_up_options": "/credits"})
		return
	}
	respondJSON(w, http.StatusOK, result)
}
func (s *Service) handleRelease(w http.ResponseWriter, r *http.Request) {
	result, err := s.Release(r.Context(), accounts.LearnerID(r.Context()), r.PathValue("id"))
	if err != nil {
		respondJSON(w, http.StatusConflict, map[string]string{"error": "reservation_unavailable"})
		return
	}
	respondJSON(w, http.StatusOK, result)
}

func nonnegativeUint64(n int64) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}
