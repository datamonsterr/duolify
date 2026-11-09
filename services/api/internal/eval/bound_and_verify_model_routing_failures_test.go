package eval_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/eval"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSanitizeErrorCode(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"nil error", nil, ""},
		{"grpc 429 rate limit", status.Error(codes.ResourceExhausted, "rate_limited"), "rate_limited"},
		{"grpc 503 outage", status.Error(codes.Unavailable, "provider_outage"), "provider_outage"},
		{"grpc deadline exceeded", status.Error(codes.DeadlineExceeded, "context deadline exceeded"), "timeout"},
		{"grpc canceled", status.Error(codes.Canceled, "canceled"), "cancelled"},
		{"invalid argument schema", status.Error(codes.InvalidArgument, "invalid_schema"), "invalid_schema"},
		{"standard context deadline", context.DeadlineExceeded, "timeout"},
		{"standard context canceled", context.Canceled, "cancelled"},
		{"custom rate limit error", errors.New("provider returned HTTP 429"), "rate_limited"},
		{"custom 500 error", errors.New("HTTP 500 Internal Server Error"), "provider_outage"},
		{"custom 503 error", errors.New("HTTP 503 Service Unavailable"), "provider_outage"},
		{"unknown error", errors.New("some unexpected error"), "unknown_failure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := eval.SanitizeErrorCode(tt.err)
			if got != tt.expected {
				t.Errorf("SanitizeErrorCode(%v) = %q, want %q", tt.err, got, tt.expected)
			}
		})
	}
}

func TestRoutingBoundsPolicy(t *testing.T) {
	// Attempt ceiling: max 1 fallback after primary => max 2 attempts total
	res := eval.VerifyModelRoutingBounds(eval.RoutingConfig{
		MaxAttempts: 2,
	})
	if !res.Valid {
		t.Fatalf("expected valid for 2 attempts: %s", res.Reason)
	}

	invalidAttempts := eval.VerifyModelRoutingBounds(eval.RoutingConfig{
		MaxAttempts: 3, // More than 1 repair/fallback after primary is prohibited at launch
	})
	if invalidAttempts.Valid {
		t.Fatal("expected invalid when max_attempts > 2")
	}

	zeroAttempts := eval.VerifyModelRoutingBounds(eval.RoutingConfig{
		MaxAttempts: 0,
	})
	if zeroAttempts.Valid {
		t.Fatal("expected invalid when max_attempts == 0")
	}
}

func TestIsExpensiveFallback(t *testing.T) {
	if !eval.IsExpensiveFallback(1000, 1001) {
		t.Fatal("expected cost > cap to be expensive")
	}
	if eval.IsExpensiveFallback(1000, 1000) {
		t.Fatal("expected cost == cap to not be expensive")
	}
	if eval.IsExpensiveFallback(1000, 500) {
		t.Fatal("expected cost < cap to not be expensive")
	}
}

func TestStreamingDeadlines(t *testing.T) {
	d := eval.StreamingDeadlines{
		TaskDeadline:      5 * time.Second,
		FirstTokenTimeout: 2 * time.Second,
		IdleTimeout:       1 * time.Second,
	}

	if err := d.Validate(); err != nil {
		t.Fatalf("valid streaming deadlines rejected: %v", err)
	}

	// First token timeout cannot exceed task deadline
	invalid := eval.StreamingDeadlines{
		TaskDeadline:      1 * time.Second,
		FirstTokenTimeout: 2 * time.Second,
		IdleTimeout:       500 * time.Millisecond,
	}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected error when first-token timeout > task deadline")
	}

	// Idle timeout cannot exceed task deadline
	invalidIdle := eval.StreamingDeadlines{
		TaskDeadline:      1 * time.Second,
		FirstTokenTimeout: 500 * time.Millisecond,
		IdleTimeout:       2 * time.Second,
	}
	if err := invalidIdle.Validate(); err == nil {
		t.Fatal("expected error when idle timeout > task deadline")
	}
}

func TestVerifyDecimalAliases(t *testing.T) {
	models := map[string]string{
		"preview":             "anthropic/claude-sonnet-5.5",
		"outline":             "anthropic/claude-sonnet-5.5",
		"lesson":              "anthropic/claude-sonnet-5.5",
		"chat":                "anthropic/claude-haiku-5.5",
		"note":                "anthropic/claude-haiku-5.5",
		"setup_chat":          "anthropic/claude-haiku-5.5",
		"course_brief_chat":   "anthropic/claude-haiku-5.5",
		"outline_chat_change": "anthropic/claude-haiku-5.5",
	}

	if !eval.VerifyDecimalAliases(models) {
		t.Fatal("expected valid decimal aliases")
	}

	// Hyphenated instead of decimal is invalid
	invalidModels := map[string]string{
		"preview": "anthropic/claude-sonnet-5-5",
	}
	if eval.VerifyDecimalAliases(invalidModels) {
		t.Fatal("expected invalid for hyphenated OpenRouter model")
	}
}
