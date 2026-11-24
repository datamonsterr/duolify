package narration

import (
	"context"
	"errors"
	"time"
)

// Defaults for the ElevenLabs Starter plan (ADR 0015): 40,000 characters per subscription month,
// and a stop line below the limit so a concurrent run cannot push the account into overage.
const (
	DefaultMonthlyCharLimit  = 40000
	DefaultBudgetStopPercent = 95
)

// CodeBudgetExhausted is the failure code of a slide parked captions-only by the budget.
const CodeBudgetExhausted = "narration_budget_exhausted"

var errBudgetExhausted = errors.New("narration monthly character budget exhausted")

// Budget caps the characters sent to the live TTS provider in one subscription cycle.
// A zero Limit or StopPercent disables enforcement (mock speech, tests).
type Budget struct {
	// Limit is the plan's characters per cycle.
	Limit int64
	// StopPercent is the share of Limit after which no live request is made (1 to 100).
	StopPercent int
	// CycleDay is the day of month (1 to 28) the subscription cycle resets, in UTC.
	CycleDay int
}

// Enabled reports whether live requests are checked against the budget.
func (b Budget) Enabled() bool { return b.Limit > 0 && b.StopPercent > 0 }

// Allowed is the most characters a cycle may use before live requests stop.
func (b Budget) Allowed() int64 { return b.Limit * int64(min(b.StopPercent, 100)) / 100 }

// CycleStart is the most recent reset instant at or before now. Days outside 1 to 28 clamp so every
// month has the day.
func (b Budget) CycleStart(now time.Time) time.Time {
	day := min(max(b.CycleDay, 1), 28)
	now = now.UTC()
	start := time.Date(now.Year(), now.Month(), day, 0, 0, 0, 0, time.UTC)
	if start.After(now) {
		start = start.AddDate(0, -1, 0)
	}
	return start
}

// CharactersUsed includes durable admissions and their adjustments, regardless of asset retention.
func (s *Service) CharactersUsed(ctx context.Context, since time.Time) (int64, error) {
	var used int64
	err := s.opts.Pool.QueryRow(ctx, `SELECT COALESCE(sum(characters),0) FROM narration_budget_ledger WHERE occurred_at >= $1`, since).Scan(&used)
	return used, err
}

// reserveBudget serializes the cycle check and insert across all API and worker processes.
// An empty id means the requested allowance does not fit. A committed reservation counts
// even if the process dies before it can report the outcome.
func (s *Service) reserveBudget(ctx context.Context, characters int64, now time.Time) (string, int64, error) {
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('narration-budget',0))`); err != nil {
		return "", 0, err
	}
	var used int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(characters),0) FROM narration_budget_ledger WHERE occurred_at >= $1`, s.opts.Budget.CycleStart(now)).Scan(&used); err != nil {
		return "", 0, err
	}
	if characters > s.opts.Budget.Allowed()-used {
		return "", used, nil
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO narration_budget_ledger(occurred_at,characters) VALUES($1,$2) RETURNING id::text`, now, characters).Scan(&id); err != nil {
		return "", 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return id, used, nil
}

// settleBudget appends a single adjustment; failure leaves the conservative reservation counted.
func (s *Service) settleBudget(ctx context.Context, id string, actual int64) error {
	_, err := s.opts.Pool.Exec(ctx, `INSERT INTO narration_budget_ledger(occurred_at,characters,adjusts)
 SELECT occurred_at,$2-characters,id FROM narration_budget_ledger WHERE id=$1::uuid AND adjusts IS NULL AND $2 BETWEEN 0 AND characters ON CONFLICT (adjusts) DO NOTHING`, id, actual)
	return err
}

// alertBudgetExhausted records one refusal run: a PostHog event and an error log line for alerting.
func (s *Service) alertBudgetExhausted(owner string, used, requested int64, denied int, model string) {
	b := s.opts.Budget
	s.opts.Logger.Error("narration monthly character budget exhausted", "characters_used", used, "characters_limit", b.Limit, "characters_requested", requested, "stop_percent", b.StopPercent, "items_denied", denied)
	s.emit(owner, "narration_budget_exhausted", map[string]any{"provider": elevenLabsProvider, "model": model, "reason_code": "budget_exhausted", "characters_used": used, "characters_limit": b.Limit, "characters_requested": requested, "stop_percent": int64(b.StopPercent), "items_denied": int64(denied)})
}
