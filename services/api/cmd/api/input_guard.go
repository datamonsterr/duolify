package main

import (
	"context"
	"log/slog"
	"os"
	"strconv"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
)

// enableInputGuard turns on the ADR 0025 input guard when INPUT_GUARD_ENABLED is true. It stays off by
// default: the engine answers only with passing live `input_guard` evidence, so a live deployment enables
// it after the evidence refresh. Events carry the surface, reason code and source, never the text.
func enableInputGuard(backend *engine.Client, events analytics.Events, logger *slog.Logger) {
	on, err := strconv.ParseBool(os.Getenv("INPUT_GUARD_ENABLED"))
	if err != nil || !on {
		return
	}
	backend.EnableGuard(func(ctx context.Context, e engine.GuardEvent) {
		learner, ok := accounts.LearnerIDFromContext(ctx)
		if !ok {
			return
		}
		if err := events.Capture(learner, "input_guard_decision", map[string]any{
			"source": "api", "platform": "web", "app_version": "0.1.0",
			"surface": e.Surface, "allowed": e.Allowed, "reason_code": e.ReasonCode,
			"guard_source": e.Source, "late": e.Late, "latency_ms": int(e.Latency.Milliseconds()),
		}); err != nil {
			logger.WarnContext(ctx, "input guard analytics failed", "code", "capture_failed")
		}
	})
	logger.Info("input guard enabled")
}
