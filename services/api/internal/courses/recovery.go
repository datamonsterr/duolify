package courses

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// ReapExpired owns deadline handling outside read requests. Validated output is
// durable and can be persisted/delivered without another provider request.
func (s *Service) ReapExpired(ctx context.Context) error {
	rows, err := s.opts.Pool.Query(ctx, `SELECT c.id::text,c.learner_id::text,c.state,COALESCE(j.id::text,''),c.quote_id::text FROM course_creations c JOIN pricing_quotes q ON q.id=c.quote_id LEFT JOIN credit_jobs j ON j.quote_id=c.quote_id WHERE c.state='validated' OR (c.state IN ('queued','generating') AND clock_timestamp()>=COALESCE(j.deadline_at,c.created_at+(q.snapshot#>>'{bounds,timeout_ms}')::bigint*interval '1 millisecond'+interval '2 seconds'))`)
	if err != nil {
		return err
	}
	type work struct{ id, owner, state, job, quote string }
	var creations []work
	for rows.Next() {
		var w work
		if err = rows.Scan(&w.id, &w.owner, &w.state, &w.job, &w.quote); err != nil {
			rows.Close()
			return err
		}
		creations = append(creations, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var result error
	for _, w := range creations {
		if w.state == "validated" {
			// RunCreation's advisory lock may be held by its live finalizer. Skip it
			// rather than blocking expiry of other jobs; the next tick retries.
			err = s.RunCreation(ctx, w.id)
			var snooze *river.JobSnoozeError
			if errors.As(err, &snooze) {
				err = nil
			}
		} else {
			err = s.failCreation(ctx, w.owner, w.id, w.quote, w.state, w.job, nil)
		}
		result = errors.Join(result, err)
	}
	rows, err = s.opts.Pool.Query(ctx, `SELECT p.id::text,p.learner_id::text,p.state,COALESCE(j.id::text,''),p.quote_id::text FROM outline_proposals p JOIN pricing_quotes q ON q.id=p.quote_id LEFT JOIN credit_jobs j ON j.quote_id=p.quote_id WHERE p.state='validated' OR (p.state='pending' AND clock_timestamp()>=COALESCE(j.deadline_at,p.created_at+(q.snapshot#>>'{bounds,timeout_ms}')::bigint*interval '1 millisecond'))`)
	if err != nil {
		return errors.Join(result, err)
	}
	var proposals []work
	for rows.Next() {
		var w work
		if err = rows.Scan(&w.id, &w.owner, &w.state, &w.job, &w.quote); err != nil {
			rows.Close()
			return errors.Join(result, err)
		}
		proposals = append(proposals, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errors.Join(result, err)
	}
	for _, w := range proposals {
		if w.state == "validated" {
			p, e := s.readProposal(ctx, w.owner, w.id, "")
			if errors.Is(e, pgx.ErrNoRows) {
				continue
			}
			if e != nil {
				result = errors.Join(result, e)
				continue
			}
			_, err = s.deliverProposal(ctx, w.owner, p)
		} else {
			err = s.failProposal(ctx, w.owner, w.id, "pending", w.job, nil)
		}
		result = errors.Join(result, err)
	}
	return result
}

// Maintain follows the credit maintenance worker's scheduling pattern. Only the
// stored quote/reservation deadline decides expiry; the ticker sets retry cadence.
func (s *Service) Maintain(ctx context.Context, logger *slog.Logger) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.ReapExpired(ctx); err != nil {
				logger.ErrorContext(ctx, "course job recovery remains for retry")
			}
		}
	}
}
