// Package analytics defines the server event boundary. Bootstrap emits no learner events.
package analytics

import (
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/posthog/posthog-go"
)

// Events supports server-side events and flushes queued events during shutdown.
type Events interface {
	Capture(learnerID, event string, properties map[string]any) error
	Close() error
}
type noop struct{}

func (noop) Capture(string, string, map[string]any) error { return nil }
func (noop) Close() error                                 { return nil }

type client struct {
	posthog.Client
	queue    chan posthog.Capture
	done     chan struct{}
	mu       sync.RWMutex
	closed   bool
	once     sync.Once
	closeErr error
}

func (c *client) Capture(learnerID, event string, properties map[string]any) error {
	if len(c.queue) == cap(c.queue) {
		return nil
	}
	id, err := uuid.Parse(learnerID)
	if !allowlisted(event) || err != nil || id == uuid.Nil {
		return nil
	}
	filtered := safeProperties(event, properties)
	if filtered["source"] == nil || filtered["platform"] == nil || filtered["app_version"] == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return nil
	}
	select {
	case c.queue <- posthog.Capture{DistinctId: id.String(), Event: event, Properties: posthog.Properties(filtered)}:
	default:
	}
	return nil
}

// Close drains the bounded adapter queue before the SDK's bounded shutdown.
func (c *client) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		close(c.queue)
		c.mu.Unlock()
		<-c.done
		c.closeErr = c.Client.Close()
	})
	return c.closeErr
}

func allowlisted(event string) bool {
	switch event {
	case "learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed",
		"backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed",
		"$ai_generation", "ui_control_activated",
		"paywall_viewed", "credits_upsell_shown", "pricing_version_exposed", "ui_purchase_option_selected", "ui_checkout_requested",
		"pricing_version_created", "pricing_version_approved", "pricing_version_rejected",
		"pricing_version_activated", "pricing_version_rolled_back", "price_table_activated",
		"daily_credits_granted", "daily_credits_expired", "credits_reserved", "credits_settled", "credits_released", "credits_insufficient", "credits_exhausted":
		return true
	default:
		return false
	}
}

// New disables analytics completely when the key is empty.
func New(key, host string) (Events, error) {
	return NewWithOptions(key, host, Options{OptOut: strings.EqualFold(os.Getenv("ANALYTICS_OPT_OUT"), "true")})
}

// Options can disable delivery without changing the caller's operational monitor.
type Options struct{ OptOut bool }

// NewWithOptions uses the SDK's bounded nonblocking queue (ErrQueueFull drops).
func NewWithOptions(key, host string, options Options) (Events, error) {
	if key == "" || options.OptOut || strings.EqualFold(os.Getenv("ANALYTICS_OPT_OUT"), "true") {
		return noop{}, nil
	}
	sdk, err := posthog.NewWithConfig(key, posthog.Config{Endpoint: host, MaxQueueSize: 1024, MaxEnqueuedRequests: 2, ShutdownTimeout: 3 * time.Second, BatchUploadTimeout: 2 * time.Second, Logger: posthog.StdLogger(log.New(io.Discard, "", 0), false)})
	if err != nil {
		return nil, err
	}
	c := &client{Client: sdk, queue: make(chan posthog.Capture, 1024), done: make(chan struct{})}
	go func() {
		defer close(c.done)
		for event := range c.queue {
			_ = sdk.Enqueue(event)
		}
	}()
	return c, nil
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9_./:+-]{1,100}$`)

// safeProperties copies event schemas rather than mutating the emitter's map.
// No arbitrary string, nested object, prompt, completion or error field is accepted.
func safeProperties(event string, input map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"source", "platform", "app_version"} {
		v, ok := input[key].(string)
		if !ok {
			continue
		}
		switch key {
		case "source":
			if v != "api" && v != "ci" {
				continue
			}
		case "platform":
			if v != "web" && v != "desktop" && v != "ios" && v != "android" {
				continue
			}
		case "app_version":
			if !identifier.MatchString(v) {
				continue
			}
		}
		out[key] = v
	}
	numeric := func(keys ...string) {
		for _, key := range keys {
			switch v := input[key].(type) {
			case int:
				if v >= 0 {
					out[key] = v
				}
			case int64:
				if v >= 0 {
					out[key] = v
				}
			case uint64, uint32:
				out[key] = v
			case float64:
				if v >= 0 && v < 1e15 {
					out[key] = v
				}
			}
		}
	}
	boolean := func(keys ...string) {
		for _, key := range keys {
			if v, ok := input[key].(bool); ok {
				out[key] = v
			}
		}
	}
	ids := func(keys ...string) {
		for _, key := range keys {
			if v, ok := input[key].(string); ok && identifier.MatchString(v) {
				out[key] = v
			}
		}
	}
	switch event {
	case "credits_reserved", "credits_settled", "credits_released", "credits_insufficient", "credits_exhausted":
		numeric("amount", "input_tokens", "output_tokens", "balance", "estimate")
		ids("feature", "model", "pricing_version", "bucket")
	case "daily_credits_granted", "daily_credits_expired":
		numeric("amount")
	case "backup_completed", "backup_failed", "restore_drill_completed":
		numeric("size_bytes", "duration_ms")
		boolean("ok")
	case "eval_run_completed":
		numeric("score", "threshold")
		boolean("passed")
		ids("task")
	case "$ai_generation":
		numeric("$ai_input_tokens", "$ai_output_tokens", "$ai_latency")
		boolean("fallback_used", "$ai_is_error", "succeeded", "usage_available")
		ids("$ai_provider", "$ai_model", "feature")
		numeric("input_tokens", "output_tokens", "latency_ms", "attempt", "cache_write_tokens", "cache_read_tokens", "narration_characters", "provider_cost_nanos")
		ids("provider", "model", "task", "pricing_version")
		for legacy, canonical := range map[string]string{"input_tokens": "$ai_input_tokens", "output_tokens": "$ai_output_tokens", "latency_ms": "$ai_latency", "provider": "$ai_provider", "model": "$ai_model", "task": "feature"} {
			if out[canonical] == nil && out[legacy] != nil {
				out[canonical] = out[legacy]
			}
		}
		if out["$ai_is_error"] == nil {
			succeeded, known := out["succeeded"].(bool)
			out["$ai_is_error"] = known && !succeeded
		}
	case "analytics_opt_out_changed":
		boolean("enabled")
	case "learner_profile_updated":
		allowed := map[string]bool{}
		for _, key := range strings.Fields("goal topic background level daily_minutes daily_xp locale time_zone reminder_enabled reminder_time appearance motion narration narration_speed captions text_size ui_sounds haptics analytics_opt_out") {
			allowed[key] = true
		}
		if fields, ok := input["fields_changed"].([]string); ok {
			safe := []string{}
			for _, field := range fields {
				if allowed[field] {
					safe = append(safe, field)
				}
			}
			out["fields_changed"] = safe
		}
	case "ui_control_activated":
		ids("control_id", "screen")
	case "paywall_viewed", "credits_upsell_shown", "pricing_version_exposed", "ui_purchase_option_selected", "ui_checkout_requested":
		ids("placement", "feature", "plan_id", "price_table_version", "pricing_version", "surface", "offer_id")
		numeric("balance")
		if period := input["billing_period"]; period == "one_time" || period == "monthly" || period == "yearly" {
			out["billing_period"] = period
		}
		if reset, ok := input["next_daily_grant_at"].(string); ok {
			if _, err := time.Parse(time.RFC3339, reset); err == nil {
				out["next_daily_grant_at"] = reset
			}
		}
	case "pricing_version_created", "pricing_version_approved", "pricing_version_rejected", "pricing_version_activated", "pricing_version_rolled_back", "price_table_activated":
		ids("pricing_version")
		if class := input["change_class"]; class == "adverse" || class == "non_adverse" {
			out["change_class"] = class
		}
		if v, ok := input["actor_id"].(string); ok {
			if id, err := uuid.Parse(v); err == nil {
				out["actor_id"] = id.String()
			}
		}
	}
	return out
}
