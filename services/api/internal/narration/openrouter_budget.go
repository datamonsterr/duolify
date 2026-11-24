package narration

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	openRouterProvider = "openrouter"
	elevenLabsProvider = "elevenlabs"
)

// DefaultOpenRouterMonthlyCapNanos is USD 5. An explicit zero cap suspends this route.
const DefaultOpenRouterMonthlyCapNanos = 5_000_000_000

// OpenRouterBudget caps UTC-month spend across all OpenRouter speech models.
type OpenRouterBudget struct {
	MonthlyCapNanos int64
}

// OpenRouterMonthlyBudget parses USD into integer nanodollars. A malformed
// override suspends the route so a mistyped cap cannot authorize more spend.
func OpenRouterMonthlyBudget(value string) OpenRouterBudget {
	if strings.TrimSpace(value) == "" {
		return OpenRouterBudget{MonthlyCapNanos: DefaultOpenRouterMonthlyCapNanos}
	}
	whole, fractional, _ := strings.Cut(strings.TrimSpace(value), ".")
	if len(fractional) > 9 || strings.HasPrefix(whole, "-") {
		return OpenRouterBudget{}
	}
	dollars, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || dollars < 0 || dollars > (math.MaxInt64-999_999_999)/1_000_000_000 {
		return OpenRouterBudget{}
	}
	nanos, err := strconv.ParseInt(fractional+strings.Repeat("0", 9-len(fractional)), 10, 64)
	if err != nil || nanos < 0 {
		return OpenRouterBudget{}
	}
	return OpenRouterBudget{MonthlyCapNanos: dollars*1_000_000_000 + nanos}
}

func openRouterMonthStart(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func openRouterCostNanos(characters, nanosPerCharacter int64) int64 {
	if characters <= 0 || nanosPerCharacter <= 0 || characters > math.MaxInt64/nanosPerCharacter {
		return 0
	}
	return characters * nanosPerCharacter
}

// reserveOpenRouter admits all OpenRouter TTS models against one shared UTC month.
func (s *Service) reserveOpenRouter(ctx context.Context, usdNanos int64, now time.Time) (string, int64, error) {
	allowed := s.opts.OpenRouterBudget.MonthlyCapNanos
	if usdNanos <= 0 || allowed <= 0 {
		return "", 0, nil
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('openrouter-narration-budget',0))`); err != nil {
		return "", 0, err
	}
	month := openRouterMonthStart(now)
	var used int64
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(usd_nanos),0) FROM openrouter_narration_budget_ledger
		WHERE occurred_at >= $1 AND occurred_at < $2`, month, month.AddDate(0, 1, 0)).Scan(&used); err != nil {
		return "", 0, err
	}
	if usdNanos > allowed-used {
		return "", used, nil
	}
	var id string
	if err = tx.QueryRow(ctx, `INSERT INTO openrouter_narration_budget_ledger(occurred_at,usd_nanos) VALUES($1,$2) RETURNING id::text`, now, usdNanos).Scan(&id); err != nil {
		return "", 0, err
	}
	return id, used, tx.Commit(ctx)
}

func (s *Service) settleOpenRouter(ctx context.Context, id string, actual int64) error {
	_, err := s.opts.Pool.Exec(ctx, `INSERT INTO openrouter_narration_budget_ledger(occurred_at,usd_nanos,adjusts)
		SELECT occurred_at,$2-usd_nanos,id FROM openrouter_narration_budget_ledger
		WHERE id=$1::uuid AND adjusts IS NULL AND $2 BETWEEN 0 AND usd_nanos
		ON CONFLICT (adjusts) DO NOTHING`, id, actual)
	return err
}

func (s *Service) alertOpenRouterBudget(owner, model string, used, requested int64) {
	limit := s.opts.OpenRouterBudget.MonthlyCapNanos
	s.opts.Logger.Warn("OpenRouter narration monthly cost cap reached", "usd_nanos_used", used, "usd_nanos_limit", limit, "usd_nanos_requested", requested)
	s.emit(owner, "narration_budget_exhausted", map[string]any{
		"provider": openRouterProvider, "model": model, "reason_code": "budget_exhausted",
		"usd_nanos_used": used, "usd_nanos_limit": limit, "usd_nanos_requested": requested, "items_denied": 1,
	})
}
