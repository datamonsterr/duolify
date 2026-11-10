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
	case "slide_viewed", "slide_replayed", "slide_paused":
		return true
	case "lesson_started", "lesson_player_error", "course_outline_generated", "course_enrolled", "outline_proposal_requested", "outline_proposal_delivered", "outline_proposal_failed", "outline_proposal_applied", "outline_proposal_discarded", "outline_proposal_undone", "course_preview_requested", "course_preview_viewed", "course_generation_failed", "ui_recovery_selected", "learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed",
		"checkout_started", "credits_purchased", "credits_purchase_failed",
		"plan_purchased", "subscription_grant_issued", "entitlement_changed", "subscription_renewed", "subscription_cancelled", "subscription_payment_failed",
		"backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed", "ai_cost_budget_alert",
		"$ai_generation", "ui_control_activated", "ui_navigation_selected",
		"paywall_viewed", "credits_upsell_shown", "pricing_version_exposed", "ui_purchase_option_selected", "ui_checkout_requested",
		"pricing_version_created", "pricing_version_approved", "pricing_version_rejected",
		"pricing_version_activated", "pricing_version_rolled_back", "price_table_activated",
		"character_rendered", "character_render_fallback",
		"character_create_quoted", "character_created", "character_regenerated", "character_create_failed",
		"character_updated", "character_deleted", "character_rejected", "course_cast_selected",
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
	case "character_updated":
		ids("character_id")
		numeric("character_version")
		if fields, ok := input["fields_changed"].([]string); ok {
			safe := []string{}
			for _, field := range fields {
				switch field {
				case "info.name", "info.bio", "info.backstory", "personality.traits", "personality.speaking_style", "personality.persona_prompt", "appearance.body_shape", "appearance.palette", "appearance.eyes", "appearance.mouth", "appearance.accessories", "voice_key":
					safe = append(safe, field)
				}
			}
			out["fields_changed"] = safe
		}
	case "character_deleted":
		ids("character_id")
	case "character_create_quoted":
		ids("pricing_version")
		numeric("credits")
		if operation := input["operation"]; operation == "create" || operation == "regenerate" {
			out["operation"] = operation
		}
	case "character_created", "character_regenerated":
		ids("character_id", "pricing_version", "rig_version", "voice_key")
		numeric("character_version", "credits")
	case "character_rejected", "character_create_failed":
		if operation := input["operation"]; operation == "manual_edit" || operation == "create" || operation == "regenerate" {
			out["operation"] = operation
		}
		boolean("fallback_used")
		if reason, ok := input["reason_code"].(string); ok {
			switch reason {
			case "real_person", "third_party_ip", "sexual_content", "age_inappropriate", "prompt_injection", "invalid_spec", "limit_exceeded", "screening_unavailable", "creation_disabled", "cancelled", "timeout", "version_conflict", "provider_unavailable":
				out["reason_code"] = reason
			}
		}
	case "course_cast_selected":
		numeric("supporting_count", "cast_version")
		if source := input["cast_source"]; source == "default" || source == "custom" || source == "mixed" {
			out["cast_source"] = source
		}
	case "slide_viewed", "slide_replayed", "slide_paused":
		ids("lesson_id", "slide_id")
		numeric("index")
		if event == "slide_viewed" {
			numeric("watch_ms")
			boolean("offline")
		}
	case "lesson_started":
		ids("lesson_id", "course_id")
	case "lesson_player_error":
		ids("lesson_id", "slide_id")
		if code, ok := input["error_code"].(string); ok {
			switch code {
			case "manifest_unavailable", "document_unavailable", "invalid_manifest", "document_timeout", "offline", "position_unavailable", "offline_uncached", "cache_unavailable", "checksum_mismatch", "asset_unavailable", "slide_failure", "subscription_required", "download_failed":
				out["error_code"] = code
			}
		}
	case "course_outline_generated", "course_enrolled":
		ids("course_id", "provider", "model")
		numeric("unit_count", "lesson_count", "latency_ms")
	case "outline_proposal_requested", "outline_proposal_delivered", "outline_proposal_failed", "outline_proposal_applied", "outline_proposal_discarded", "outline_proposal_undone":
		ids("proposal_id")
		numeric("base_revision")
	case "course_preview_requested", "course_preview_viewed", "course_generation_failed":
		numeric("topic_length", "duration_ms")
		boolean("has_profile")
		ids("stage", "error_code", "preview_id")
	case "plan_purchased", "subscription_grant_issued", "entitlement_changed", "subscription_renewed", "subscription_cancelled", "subscription_payment_failed":
		numeric("amount", "amount_minor")
		ids("plan_id", "interval", "channel", "currency", "price_table_version", "entitlement_version", "reason_code")
		if value, ok := input["period_key"].(string); ok && len(value) <= 500 && regexp.MustCompile(`^stripe:test:[A-Za-z0-9_-]+:[0-9]+:[0-9]+:(?:[0-9]|1[01])$`).MatchString(value) {
			out["period_key"] = value
		}
		for _, key := range []string{"billing_period_start", "billing_period_end"} {
			if value, ok := input[key].(string); ok {
				if _, err := time.Parse(time.RFC3339, value); err == nil {
					out[key] = value
				}
			}
		}
	case "checkout_started", "credits_purchased", "credits_purchase_failed":
		numeric("amount", "price", "amount_minor")
		ids("currency", "sku_id", "channel", "price_table_version")
		if reason, ok := input["reason"].(string); ok && (reason == "failed" || reason == "expired" || reason == "invalid_evidence") {
			out["reason"] = reason
		}
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
		ids("task", "dataset_revision", "dataset_source")
	case "ai_cost_budget_alert":
		ids("scope", "status", "pricing_version")
		numeric("actual_usd", "budget_usd", "threshold", "actual_percent", "cost_nanos", "budget_nanos", "cache_hit_rate", "paid_margin")
		boolean("ok")
	case "$ai_generation":
		numeric("$ai_input_tokens", "$ai_output_tokens", "$ai_latency")
		boolean("fallback_used", "$ai_is_error", "succeeded", "usage_available")
		ids("$ai_provider", "$ai_model", "feature", "error_code")
		numeric("input_tokens", "output_tokens", "latency_ms", "attempt", "cache_write_tokens", "cache_read_tokens", "narration_characters", "provider_cost_nanos")
		ids("provider", "model", "task", "pricing_version", "error_code")
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
		for _, key := range strings.Fields("goal topic background level course_level daily_minutes daily_xp locale time_zone reminder_enabled reminder_time appearance motion narration narration_speed captions text_size ui_sounds haptics analytics_opt_out") {
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
		ids("control_id", "screen", "surface", "course_id")
	case "ui_recovery_selected":
		ids("surface", "error_code", "action", "course_id", "screen_id")
	case "ui_navigation_selected":
		ids("destination")
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
	case "character_rendered":
		ids("speaker_key", "character_version", "rig_version", "expression")
		boolean("offline")
	case "character_render_fallback":
		ids("reason_code")
		boolean("offline")
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
