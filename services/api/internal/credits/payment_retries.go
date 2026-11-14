package credits

import (
	"context"
	"errors"
	"runtime/debug"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/jackc/pgx/v5"
)

const paymentMaxAttempts = 5

// ErrFinancialReconciliation deliberately contains no provider or learner data.
var ErrFinancialReconciliation = errors.New("financial reconciliation unresolved")

func inboxRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 6 {
		return time.Hour
	}
	return time.Minute * time.Duration(1<<uint(attempt-1))
}

// processInboxEvent claims one durable lease before calling the canonical provider.
// A crashed call consumes an attempt; expired leases can be retried by any worker.
func (s *Service) processInboxEvent(ctx context.Context, id string) error {
	if _, err := s.pool().Exec(ctx, `INSERT INTO credit_payment_attempts(event_id) SELECT $1
 WHERE NOT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=$1) ON CONFLICT DO NOTHING`, id); err != nil {
		return err
	}
	var attempt int
	err := s.pool().QueryRow(ctx, `UPDATE credit_payment_attempts a SET attempts=attempts+1,total_attempts=total_attempts+1,
	 next_attempt_at=now()+interval '1 hour',updated_at=now(),waiting_for_binding=false
 WHERE event_id=$1 AND attempts<5 AND dead_letter_at IS NULL AND (next_attempt_at<=now() OR (waiting_for_binding AND EXISTS(SELECT 1 FROM credit_payment_inbox i JOIN credit_payment_sessions p ON p.session_id=i.session_id AND p.environment=i.environment WHERE i.event_id=a.event_id)))
 AND NOT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=a.event_id)
 RETURNING attempts`, id).Scan(&attempt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var awaitingBinding bool
	err = s.pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credit_payment_inbox i WHERE i.event_id=$1
 AND i.event_type LIKE 'checkout.%' AND NOT EXISTS(SELECT 1 FROM credit_payment_sessions p WHERE p.session_id=i.session_id AND p.environment=i.environment))`, id).Scan(&awaitingBinding)
	if err == nil && awaitingBinding {
		err = ErrFinancialReconciliation
	}
	if err == nil {
		err = s.processPayment(ctx, id)
	}
	var completed bool
	if err == nil {
		err = s.pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=$1)", id).Scan(&completed)
		if err == nil && !completed {
			err = ErrFinancialReconciliation
		}
	}
	if err == nil {
		var rejected bool
		if e := s.pool().QueryRow(ctx, "SELECT state='rejected' FROM credit_payment_results WHERE event_id=$1", id).Scan(&rejected); e != nil {
			return e
		}
		if rejected {
			s.reconciliationFailure(ctx, "invalid_evidence")
		}
		return nil
	}
	reason := "provider_unavailable"
	if errors.Is(err, ErrReversalEvidence) || errors.Is(err, errStripeEvidence) {
		reason = "invalid_evidence"
	}
	// Use a fresh bounded context so a cancellation cannot strand the final claim
	// without a terminal marker. Attempts>=5 also recover this state on restart.
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, recordErr := s.pool().Exec(recordCtx, `UPDATE credit_payment_attempts SET next_attempt_at=now()+$2::interval,
 dead_letter_at=CASE WHEN attempts>=5 THEN now() ELSE NULL END,last_reason_code=$3,waiting_for_binding=$4,updated_at=now() WHERE event_id=$1`, id, inboxRetryDelay(attempt).String(), reason, awaitingBinding)
	if attempt >= paymentMaxAttempts {
		s.reconciliationFailure(ctx, "dead_letter")
	}
	if awaitingBinding {
		return recordErr
	}
	return errors.Join(err, recordErr)
}

// terminalizeAbandonedClaims recovers a crash after the fifth lease was claimed.
// The lease must expire first so a still-running final attempt can finish.
func (s *Service) terminalizeAbandonedClaims(ctx context.Context) error {
	tag, err := s.pool().Exec(ctx, `UPDATE credit_payment_attempts a SET dead_letter_at=now(),last_reason_code='dead_letter',updated_at=now()
 WHERE attempts>=5 AND dead_letter_at IS NULL AND next_attempt_at<=now()
 AND NOT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=a.event_id)`)
	if err == nil && tag.RowsAffected() > 0 {
		s.reconciliationFailure(ctx, "dead_letter")
	}
	return err
}

// ReplayDeadLetter opens one new bounded cycle after operators repair the cause.
// Counters survive restores. Completed events are an idempotent no-op.
func (s *Service) ReplayDeadLetter(ctx context.Context, eventID string) error {
	if s.pool() == nil || s.payments.Mode != "test" {
		return errPaymentConfig
	}
	tag, err := s.pool().Exec(ctx, `UPDATE credit_payment_attempts a SET attempts=0,replay_count=replay_count+1,
 next_attempt_at=now(),dead_letter_at=NULL,last_reason_code='',updated_at=now()
 WHERE event_id=$1 AND (dead_letter_at IS NOT NULL OR (attempts>=5 AND next_attempt_at<=now()))
 AND NOT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=a.event_id)`, eventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var done bool
		if err = s.pool().QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM credit_payment_results WHERE event_id=$1)", eventID).Scan(&done); err != nil {
			return err
		}
		if done {
			return nil
		}
		return ErrFinancialReconciliation
	}
	return s.processInboxEvent(ctx, eventID)
}

func (s *Service) reconciliationFailure(ctx context.Context, reason string) {
	switch reason {
	case "provider_unavailable", "invalid_evidence", "dead_letter", "mismatch":
	default:
		reason = "mismatch"
	}
	s.financialMonitor(ctx).Incident(ctx, "dependency", "financial-reconciliation", false)
	if s.events == nil {
		return
	}
	version := "development"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = info.Main.Version
	}
	_ = s.events.Capture("00000000-0000-4000-8000-000000000011", "payment_reconciliation_failed", map[string]any{"source": "api", "platform": "web", "app_version": version, "channel": "stripe", "reason_code": reason})
}

// UseOperationalMonitor shares background inbox alerts with the private metrics
// listener. Configure it before starting workers or request handling.
func (s *Service) UseOperationalMonitor(monitor *telemetry.Monitor) { s.operational = monitor }

func (s *Service) financialMonitor(ctx context.Context) *telemetry.Monitor {
	if s.operational != nil {
		return s.operational
	}
	return telemetry.FromContext(ctx)
}

// PurgeFinancialDiagnostics is invoked only by its own durable retention job.
// Outbox keys remain seven-year audit/dedupe evidence after derived properties expire.
func (s *Service) PurgeFinancialDiagnostics(ctx context.Context) error {
	if s.pool() == nil {
		return nil
	}
	tx, err := s.pool().Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "DELETE FROM credit_payment_payloads WHERE expires_at<=now()"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE credit_outbox SET properties='{}',dispatched_at=COALESCE(dispatched_at,now()) WHERE created_at<=now()-interval '90 days' AND (properties<>'{}' OR dispatched_at IS NULL)`); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
