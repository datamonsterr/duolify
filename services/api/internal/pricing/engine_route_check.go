package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/datamonsterr/duolify/services/api/internal/store/db"
)

// RouteGap is a quoted action whose route the engine will refuse: the engine compares the
// quote's provider, model and first fallback with its own route and returns
// FailedPrecondition on any difference.
type RouteGap struct {
	Action  string
	Version string
	Quote   string // provider/model -> fallback provider/model
	Engine  string // engine primary -> fallback provider
}

// UnservedRoutes compares the providers each quoted action would use with the providers the
// engine reports (Health: primary, fallback). The engine does not report its models, so a
// model drift is caught by TestHy4RoutingPolicyCoversEngineRoutes instead.
func UnservedRoutes(c Config, p QuotePolicy, engineProviders []string) []RouteGap {
	if len(engineProviders) < 2 {
		return nil
	}
	var gaps []RouteGap
	for action, key := range p.ActionModels {
		if _, ok := c.Actions[action]; !ok {
			continue
		}
		primary := p.Models[key]
		fallback := p.Models[p.FallbackModels[key]]
		if primary.Provider == engineProviders[0] && fallback.Provider == engineProviders[1] {
			continue
		}
		gaps = append(gaps, RouteGap{
			Action:  action,
			Version: c.Version,
			Quote:   fmt.Sprintf("%s/%s -> %s/%s", primary.Provider, primary.Model, fallback.Provider, fallback.Model),
			Engine:  fmt.Sprintf("%s -> %s", engineProviders[0], engineProviders[1]),
		})
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].Action < gaps[j].Action })
	return gaps
}

// ReportUnservedRoutes logs one error per quoted action the engine will refuse. It only
// signals; quoting and serving are unchanged.
func ReportUnservedRoutes(ctx context.Context, logger *slog.Logger, c Config, p QuotePolicy, providers func(context.Context) ([]string, error)) {
	engine, err := providers(ctx)
	if err != nil {
		logger.WarnContext(ctx, "pricing route check skipped: engine providers unavailable", "error", err)
		return
	}
	for _, g := range UnservedRoutes(c, p, engine) {
		logger.ErrorContext(ctx, "active pricing quote route is not served by the engine; this action will fail",
			"action", g.Action, "pricing_version", g.Version, "quote_route", g.Quote, "engine_route", g.Engine)
	}
}

// CheckActiveRoutes runs ReportUnservedRoutes against the active pricing version.
func CheckActiveRoutes(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger, providers func(context.Context) ([]string, error)) {
	row, err := db.New(pool).ActiveQuoteConfig(ctx)
	if err != nil {
		logger.WarnContext(ctx, "pricing route check skipped: no active quote config", "error", err)
		return
	}
	c, err := Parse(row.Config)
	if err != nil {
		logger.WarnContext(ctx, "pricing route check skipped", "error", err)
		return
	}
	var p QuotePolicy
	if err := json.Unmarshal(row.Policy, &p); err != nil {
		logger.WarnContext(ctx, "pricing route check skipped", "error", err)
		return
	}
	ReportUnservedRoutes(ctx, logger, c, p, providers)
}
