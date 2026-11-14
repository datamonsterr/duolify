// Package analytics defines the server event boundary. Bootstrap emits no learner events.
package analytics

import (
	"io"
	"log"
	"os"
	"regexp"
	"slices"
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
	case "xp_earned", "daily_goal_met", "lesson_completed", "streak_extended", "streak_broken", "streak_freeze_earned", "streak_freeze_used", "league_promoted", "league_demoted", "friend_streak_started", "friend_streak_extended", "friend_streak_broken", "friend_nudge_sent":
		return true
	case "username_set", "username_rejected", "username_changed", "username_searched", "privacy_setting_changed":
		return true
	case "friend_request_sent", "friend_request_accepted", "friend_request_declined", "friend_removed", "learner_blocked":
		return true
	case "slide_viewed", "slide_replayed", "slide_paused":
		return true
	case "lesson_started", "lesson_player_error", "course_outline_generated", "course_enrolled", "outline_proposal_requested", "outline_proposal_delivered", "outline_proposal_failed", "outline_proposal_applied", "outline_proposal_discarded", "outline_proposal_undone", "course_preview_requested", "course_preview_viewed", "course_generation_failed", "ui_recovery_selected", "learner_profile_updated", "onboarding_completed", "analytics_opt_out_changed",
		"lesson_generation_requested", "lesson_generated", "lesson_test_started", "lesson_test_submitted", "lesson_test_passed",
		"lesson_test_answer_wrong", "retry_variant_queued", "retry_variant_served", "retry_variant_answered", "retry_variant_generation_failed", "retry_variant_pool_exhausted", "lesson_test_recap_shown", "ui_test_retry_presented",
		"checkout_started", "credits_purchased", "credits_purchase_failed",
		"plan_purchased", "subscription_grant_issued", "entitlement_changed", "subscription_renewed", "subscription_cancelled", "subscription_payment_failed",
		"backup_completed", "backup_failed", "restore_drill_completed", "eval_run_completed", "ai_cost_budget_alert",
		"$ai_generation", "tutor_message_sent", "tutor_reply_completed", "tutor_reply_failed", "tutor_reply_stopped", "ui_control_activated", "ui_navigation_selected", "ui_screen_viewed",
		"paywall_viewed", "credits_upsell_shown", "pricing_version_exposed", "ui_purchase_option_selected", "ui_checkout_requested",
		"pricing_version_created", "pricing_version_approved", "pricing_version_rejected",
		"pricing_version_activated", "pricing_version_rolled_back", "price_table_activated",
		"character_rendered", "character_render_fallback", "character_reaction_shown", "tutor_character_selected", "tutor_character_fallback",
		"character_create_quoted", "character_created", "character_regenerated", "character_create_failed",
		"character_updated", "character_deleted", "character_rejected", "course_cast_selected", "lesson_cast_applied",
		"daily_credits_granted", "daily_credits_expired", "credits_reserved", "credits_settled", "credits_released", "credits_insufficient", "credits_exhausted",
		"quick_note_created", "quick_note_enriched", "quick_note_enrichment_failed", "quick_note_reviewed", "quick_note_deleted", "ui_sheet_opened",
		"bookmark_added", "bookmark_removed", "bookmark_opened", "course_screening_completed",
		"course_visibility_changed", "course_published", "share_link_created", "share_link_revoked", "share_link_opened", "course_enrolled_published", "published_lesson_reused", "published_lesson_generated_on_demand",
		"marketplace_browsed", "marketplace_searched", "course_viewed",
		"course_invite_sent", "course_invite_accepted", "course_invite_declined",
		"course_republished", "course_update_applied",
		"narration_generated", "narration_failed", "narration_cache_hit", "narration_played", "narration_muted", "narration_speed_changed", "captions_toggled":
		return true
	default:
		return false
	}
}

// New disables analytics completely when the key is empty.
func New(key, host string, resolver ...ConsentResolver) (Events, error) {
	options := Options{OptOut: strings.EqualFold(os.Getenv("ANALYTICS_OPT_OUT"), "true")}
	if len(resolver) > 0 {
		options.Consent = resolver[0]
	}
	return NewWithOptions(key, host, options)
}

// Options can disable delivery without changing the caller's operational monitor.
type Options struct {
	OptOut  bool
	Consent ConsentResolver
}

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
	return WithConsent(c, options.Consent), nil
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
			if event == "quick_note_created" {
				if v == "slide" || v == "lesson" || v == "chat" {
					out[key] = v
				}
				continue
			}
			if event == "course_enrolled_published" {
				if v == "link" || v == "invite" || v == "marketplace" {
					out[key] = v
				}
				continue
			}
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
	case "xp_earned", "lesson_completed":
		ids("event_id", "course_id", "lesson_id", "local_date", "config_version")
		numeric("amount", "xp", "duration_ms")
		boolean("perfect")
	case "daily_goal_met":
		ids("event_id", "local_date")
		numeric("goal_xp")
	case "streak_extended":
		ids("event_id", "local_date")
		numeric("streak_length")
	case "streak_broken":
		ids("event_id", "broken_on")
		numeric("previous_length")
	case "streak_freeze_earned":
		ids("event_id", "reason", "local_date")
		numeric("streak_length")
	case "friend_nudge_sent":
		ids("event_id", "local_date")
	case "friend_streak_started", "friend_streak_extended":
		ids("event_id", "local_date")
		numeric("streak_length")
	case "friend_streak_broken":
		ids("event_id")
		numeric("previous_length")
		if v, _ := input["reason"].(string); v == "missed" || v == "friendship_ended" {
			out["reason"] = v
		}
	case "streak_freeze_used":
		ids("event_id", "protected_on")
		numeric("remaining", "streak_length")
	case "league_promoted", "league_demoted":
		ids("event_id", "week_start", "league")
		numeric("rank", "from_tier", "to_tier")
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
	case "course_screening_completed":
		// Closed verdict only; never Course text, findings or reasons.
		if v, ok := input["verdict"].(string); ok && (v == "pass" || v == "needs_review" || v == "block" || v == "pending_screening") {
			out["verdict"] = v
		}
		numeric("lessons_sampled", "finding_count")
		boolean("fallback_used")
	case "course_visibility_changed":
		ids("course_id")
		for _, key := range []string{"from", "to"} {
			if v, ok := input[key].(string); ok && (v == "private" || v == "link" || v == "public") {
				out[key] = v
			}
		}
	case "course_published":
		ids("course_id")
		numeric("version", "lesson_count")
		if v, ok := input["visibility"].(string); ok && (v == "private" || v == "link" || v == "public") {
			out["visibility"] = v
		}
	case "share_link_created", "share_link_revoked":
		ids("course_id")
	case "course_enrolled_published":
		ids("course_id", "source_course_id")
		numeric("version")
	case "published_lesson_reused", "published_lesson_generated_on_demand":
		ids("course_id", "lesson_id")
		numeric("version")
	case "marketplace_browsed", "marketplace_searched":
		// Closed vocabulary only: the search text never reaches analytics, only its length.
		if v, _ := input["category"].(string); marketCategories[v] {
			out["category"] = v
		}
		if v, _ := input["language"].(string); marketLanguage.MatchString(v) {
			out["language"] = v
		}
		if v, _ := input["sort"].(string); v == "popular" || v == "new" || v == "rating" {
			out["sort"] = v
		}
		numeric("result_count", "query_length")
		boolean("has_cursor")
	case "course_invite_sent", "course_invite_accepted", "course_invite_declined":
		// UUIDs and a closed enum only: never a Username, title or recipient identity.
		ids("course_id", "invite_id")
		if v, _ := input["method"].(string); v == "username" || v == "friend" {
			out["method"] = v
		}
	case "course_republished":
		ids("course_id")
		numeric("version", "lesson_count")
		if v, ok := input["verdict"].(string); ok && (v == "pass" || v == "needs_review" || v == "block" || v == "pending_screening") {
			out["verdict"] = v
		}
	case "course_update_applied":
		// Ids and counts only: never a title or changelog text.
		ids("course_id", "source_course_id")
		numeric("from_version", "to_version", "kept", "added", "archived")
	case "course_viewed":
		if v, _ := input["course_id"].(string); marketUUID.MatchString(v) {
			out["course_id"] = v
		}
		numeric("version")
		if input["surface"] == "marketplace" {
			out["surface"] = "marketplace"
		}
	case "share_link_opened":
		ids("course_id")
		boolean("has_account")
		if input["surface"] == "web" {
			out["surface"] = "web"
		}
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
	case "lesson_cast_applied":
		ids("course_id", "lesson_id")
		numeric("supporting_count", "cast_version")
		boolean("fallback_used")
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
	case "lesson_generation_requested":
		ids("course_id", "lesson_id")
	case "lesson_generated":
		ids("course_id", "lesson_id", "provider", "model")
		numeric("slide_count", "duration_ms", "latency_ms")
	case "lesson_test_started":
		ids("course_id", "lesson_id")
		numeric("question_count")
	case "lesson_test_submitted":
		ids("course_id", "lesson_id", "topic_id")
		boolean("correct", "is_variant")
		numeric("attempt", "duration_ms", "score")
	case "lesson_test_passed":
		ids("course_id", "lesson_id")
		numeric("first_try_correct", "variants_served", "duration_ms", "score", "question_count")
	case "lesson_test_answer_wrong":
		ids("course_id", "lesson_id", "topic_id")
		boolean("is_variant")
		numeric("attempt_for_topic")
	case "retry_variant_queued":
		ids("course_id", "lesson_id", "topic_id")
	case "retry_variant_served":
		ids("course_id", "lesson_id", "topic_id", "variant_source")
	case "retry_variant_answered":
		ids("course_id", "lesson_id", "topic_id")
		boolean("correct")
	case "retry_variant_generation_failed", "retry_variant_pool_exhausted":
		ids("course_id", "lesson_id", "topic_id", "error_code")
	case "lesson_test_recap_shown":
		ids("course_id", "lesson_id", "topic_id")
	case "ui_test_retry_presented":
		ids("course_id", "lesson_id", "topic_id", "question_id")
		numeric("queue_size")
	case "outline_proposal_requested", "outline_proposal_delivered", "outline_proposal_failed", "outline_proposal_applied", "outline_proposal_discarded", "outline_proposal_undone":
		ids("proposal_id")
		numeric("base_revision")
	case "course_preview_requested", "course_preview_viewed", "course_generation_failed":
		if event == "course_preview_viewed" && input["preview_kind"] == "shared_demo" {
			out["preview_kind"] = "shared_demo"
			ids("demo_version")
		}
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
		numeric("score", "threshold", "budget_usd", "budget")
		boolean("passed")
		ids("task", "dataset_revision", "dataset_source", "provider", "status")
	case "ai_cost_budget_alert":
		ids("scope", "status", "pricing_version")
		numeric("actual_usd", "budget_usd", "threshold", "actual_percent", "cost_nanos", "budget_nanos", "cache_hit_rate", "paid_margin")
		boolean("ok")
	case "narration_generated":
		ids("lesson_id", "slide_id", "voice_id", "model")
		numeric("characters", "latency_ms")
	case "narration_cache_hit":
		ids("lesson_id", "slide_id", "model")
	case "narration_failed":
		ids("lesson_id", "slide_id", "error_code")
	case "narration_played":
		ids("lesson_id", "slide_id")
		numeric("duration_ms")
	case "narration_muted":
		ids("lesson_id", "slide_id")
		boolean("muted")
	case "narration_speed_changed":
		ids("lesson_id", "slide_id")
		numeric("speed")
	case "captions_toggled":
		ids("lesson_id", "slide_id")
		boolean("captions")
	case "$ai_generation":
		numeric("$ai_input_tokens", "$ai_output_tokens", "$ai_latency")
		boolean("fallback_used", "$ai_is_error", "succeeded", "usage_available", "usage_estimated")
		ids("$ai_provider", "$ai_model", "feature", "error_code")
		numeric("input_tokens", "output_tokens", "latency_ms", "attempt", "cache_write_tokens", "cache_read_tokens", "narration_characters", "provider_cost_nanos")
		ids("provider", "model", "task", "pricing_version", "error_code", "envelope", "cache_class")
		for legacy, canonical := range map[string]string{"input_tokens": "$ai_input_tokens", "output_tokens": "$ai_output_tokens", "latency_ms": "$ai_latency", "provider": "$ai_provider", "model": "$ai_model", "task": "feature"} {
			if out[canonical] == nil && out[legacy] != nil {
				out[canonical] = out[legacy]
			}
		}
		if out["$ai_is_error"] == nil {
			succeeded, known := out["succeeded"].(bool)
			out["$ai_is_error"] = known && !succeeded
		}
	case "tutor_message_sent":
		ids("course_id", "class")
		numeric("message_length")
		boolean("has_lesson_context")
	case "tutor_reply_completed":
		ids("course_id", "class", "model", "envelope", "cache_class")
		numeric("latency_ms", "input_tokens", "output_tokens", "cache_read_tokens", "cache_write_tokens", "summary_count", "excerpt_count", "context_tokens")
	case "tutor_reply_failed":
		ids("course_id", "class", "error_code")
		numeric("latency_ms")
		boolean("interrupted", "retryable")
	case "tutor_reply_stopped":
		ids("course_id", "class", "reason")
		numeric("latency_ms", "delivered_chars")
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
	case "ui_screen_viewed":
		ids("screen_id")
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
	case "character_reaction_shown":
		if v, ok := input["reaction_trigger"].(string); ok && (v == "answer_correct" || v == "answer_incorrect" || v == "test_passed") {
			out["reaction_trigger"] = v
		}
		ids("speaker_key", "expression")
		boolean("fallback_used")
	case "tutor_character_selected":
		ids("speaker_key")
		numeric("character_version")
		if source := input["cast_source"]; source == "lesson" || source == "course" || source == "default" {
			out["cast_source"] = source
		}
	case "tutor_character_fallback":
		if reason, ok := input["reason_code"].(string); ok {
			switch reason {
			case "invalid_persona", "persona_too_large", "speaker_unavailable", "persona_unavailable", "suppressed":
				out["reason_code"] = reason
			}
		}
		if input["phase"] == "before_stream" {
			out["phase"] = "before_stream"
		}
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
	case "username_set", "username_changed":
		numeric("username_length")
	case "username_searched":
		numeric("query_length", "result_count")
	case "privacy_setting_changed":
		setting, _ := input["setting"].(string)
		value, _ := input["value"].(string)
		allowed := map[string][]string{"discoverable": {"on", "off"}, "show_streak_to_friends": {"on", "off"}, "request_policy": {"everyone", "friends_of_friends", "nobody"}, "course_invites_from": {"friends", "everyone", "nobody"}}
		if slices.Contains(allowed[setting], value) {
			out["setting"], out["value"] = setting, value
		}
	case "learner_blocked":
		boolean("was_friend")
	case "friend_request_sent":
		if method, ok := input["method"].(string); ok && (method == "username" || method == "invite_link") {
			out["method"] = method
		}
	case "username_rejected":
		if reason, ok := input["reason_code"].(string); ok {
			switch reason {
			case "username_invalid", "username_taken", "username_reserved", "username_not_allowed", "username_already_set":
				out["reason_code"] = reason
			}
		}
	case "quick_note_created":
		numeric("length")
		if src, ok := input["source"].(string); ok && (src == "slide" || src == "lesson" || src == "chat") {
			out["source"] = src
		}
	case "quick_note_enriched", "quick_note_enrichment_failed", "quick_note_reviewed", "quick_note_deleted":
		ids("note_id", "reason", "error_code")
	case "ui_sheet_opened":
		ids("sheet_id")
	case "bookmark_added", "bookmark_removed", "bookmark_opened":
		ids("course_id", "lesson_id")
		numeric("slide_id")
		if tt, ok := input["target_type"].(string); ok && (tt == "slide" || tt == "lesson") {
			out["target_type"] = tt
		}
	}
	return out
}

var (
	marketCategories = map[string]bool{"science": true, "tech": true, "languages": true, "life_skills": true, "arts": true}
	marketLanguage   = regexp.MustCompile(`^[a-z]{2,3}$`)
	marketUUID       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)
