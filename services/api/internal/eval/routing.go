// Package eval provides bounded model routing failure verification and error sanitization.
package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RoutingConfig holds configuration constraints for model routing.
type RoutingConfig struct {
	MaxAttempts int
}

// RoutingResult represents the result of validating routing configuration.
type RoutingResult struct {
	Valid  bool
	Reason string
}

// VerifyModelRoutingBounds validates that routing bounds adhere to the launch attempt ceiling:
// at most 1 repair or fallback after primary (meaning at most 2 attempts total).
func VerifyModelRoutingBounds(cfg RoutingConfig) RoutingResult {
	if cfg.MaxAttempts <= 0 {
		return RoutingResult{Valid: false, Reason: "max_attempts must be greater than zero"}
	}
	if cfg.MaxAttempts > 2 {
		return RoutingResult{Valid: false, Reason: "launch attempt ceiling permits at most 1 repair/fallback after primary (max 2 attempts)"}
	}
	return RoutingResult{Valid: true}
}

// IsExpensiveFallback checks whether the worst-case cost of a fallback attempt
// exceeds the reservation max cost ceiling.
func IsExpensiveFallback(budgetNanos, worstNanos int64) bool {
	return worstNanos > budgetNanos
}

// StreamingDeadlines specifies deadline constraints for streaming responses.
type StreamingDeadlines struct {
	TaskDeadline      time.Duration
	FirstTokenTimeout time.Duration
	IdleTimeout       time.Duration
}

// Validate ensures first-token and idle timeouts do not exceed the overall task deadline.
func (d StreamingDeadlines) Validate() error {
	if d.FirstTokenTimeout > d.TaskDeadline {
		return fmt.Errorf("first-token timeout %v exceeds task deadline %v", d.FirstTokenTimeout, d.TaskDeadline)
	}
	if d.IdleTimeout > d.TaskDeadline {
		return fmt.Errorf("idle timeout %v exceeds task deadline %v", d.IdleTimeout, d.TaskDeadline)
	}
	return nil
}

// VerifyDecimalAliases ensures all OpenRouter Anthropic aliases use canonical decimal format
// (e.g. anthropic/claude-sonnet-5.5) rather than hyphenated aliases.
func VerifyDecimalAliases(models map[string]string) bool {
	if len(models) == 0 {
		return false
	}
	for _, model := range models {
		if !strings.HasPrefix(model, "anthropic/") || !strings.HasSuffix(model, "-5.5") {
			return false
		}
	}
	return true
}

// SanitizeErrorCode translates raw internal errors or gRPC statuses into safe,
// non-leaking categorical error codes for telemetry and client status mapping.
func SanitizeErrorCode(err error) string {
	if err == nil {
		return ""
	}

	st, ok := status.FromError(err)
	if ok {
		switch st.Code() {
		case codes.ResourceExhausted:
			return "rate_limited"
		case codes.Unavailable:
			return "provider_outage"
		case codes.DeadlineExceeded:
			return "timeout"
		case codes.Canceled:
			return "cancelled"
		case codes.InvalidArgument:
			return "invalid_schema"
		}
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}

	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "429") || strings.Contains(msg, "rate_limit") || strings.Contains(msg, "rate limited") {
		return "rate_limited"
	}
	if strings.Contains(msg, "500") || strings.Contains(msg, "502") || strings.Contains(msg, "503") || strings.Contains(msg, "504") || strings.Contains(msg, "outage") || strings.Contains(msg, "unavailable") {
		return "provider_outage"
	}
	if strings.Contains(msg, "invalid_schema") || strings.Contains(msg, "schema") {
		return "invalid_schema"
	}
	if strings.Contains(msg, "expensive") || strings.Contains(msg, "budget") {
		return "expensive_fallback"
	}
	if strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline") {
		return "timeout"
	}
	if strings.Contains(msg, "cancel") {
		return "cancelled"
	}

	return "unknown_failure"
}
