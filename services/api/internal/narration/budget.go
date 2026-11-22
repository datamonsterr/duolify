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

// CharactersUsed sums the provider characters stored since the cycle began: every audio asset
// generated in it (one row per content hash, so shared and reused audio adds nothing) and every
// completed regeneration of a lost asset.
func (s *Service) CharactersUsed(ctx context.Context, since time.Time) (int64, error) {
	var used int64
	err := s.opts.Pool.QueryRow(ctx, `SELECT
		(SELECT COALESCE(sum(characters),0) FROM narration_assets WHERE created_at >= $1) +
		(SELECT COALESCE(sum(characters),0) FROM narration_regenerations WHERE status='completed' AND completed_at >= $1)`, since).Scan(&used)
	return used, err
}

// budgetRoom returns how many more characters may be sent live now, and the characters used.
func (s *Service) budgetRoom(ctx context.Context) (room, used int64, err error) {
	b := s.opts.Budget
	if used, err = s.CharactersUsed(ctx, b.CycleStart(time.Now())); err != nil {
		return 0, 0, err
	}
	return max(b.Allowed()-used, 0), used, nil
}

// alertBudgetExhausted records one refusal run: a PostHog event and an error log line for alerting.
func (s *Service) alertBudgetExhausted(owner string, used, requested int64, denied int) {
	b := s.opts.Budget
	s.opts.Logger.Error("narration monthly character budget exhausted", "characters_used", used, "characters_limit", b.Limit, "characters_requested", requested, "stop_percent", b.StopPercent, "items_denied", denied)
	s.emit(owner, "narration_budget_exhausted", map[string]any{"characters_used": used, "characters_limit": b.Limit, "characters_requested": requested, "stop_percent": int64(b.StopPercent), "items_denied": int64(denied)})
}
