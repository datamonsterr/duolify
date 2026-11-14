package backup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AnonymizedDomain is the reserved (RFC 2606) domain every replacement email uses.
const AnonymizedDomain = "anonymized.invalid"

var emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// AnonymizedEmail is deterministic per learner, so joins stay stable across runs.
func AnonymizedEmail(learnerID string) string {
	return fmt.Sprintf("learner-%s@%s", strings.ReplaceAll(learnerID, "-", ""), AnonymizedDomain)
}

// ContainsPII reports an email address that is not an anonymized replacement.
func ContainsPII(value string) bool {
	for _, match := range emailPattern.FindAllString(value, -1) {
		if !strings.HasSuffix(strings.ToLower(match), "@"+AnonymizedDomain) {
			return true
		}
	}
	return false
}

// piiRule replaces one column of every row. Expr is trusted SQL from this file.
type piiRule struct{ Table, Column, Expr string }

const (
	redacted = `'[redacted]'`
	emptyObj = `'{}'::jsonb`
	emptyArr = `'[]'::jsonb`
)

// piiRules is the registry of learner-identifying or learner-authored columns.
// Register new columns here when a migration adds them: the drill fails closed
// on any text-like column that is neither registered here nor reviewed as safe.
var piiRules = []piiRule{
	{"learners", "display_name", `'Learner ' || left(md5(id::text), 8)`},
	{"auth_identities", "email", `'learner-' || replace(learner_id::text, '-', '') || '@` + AnonymizedDomain + `'`},
	{"auth_provider_identities", "subject", `'anon-' || md5(provider || learner_id::text)`},
	{"auth_provider_identities", "verified_email", `'learner-' || replace(learner_id::text, '-', '') || '@` + AnonymizedDomain + `'`},
	{"learner_usernames", "username", `'learner' || left(md5(learner_id::text), 8)`},
	{"learner_profiles", "background", `''`},
	{"learner_profiles", "goal", `''`},
	{"learner_profiles", "topic", `''`},
	{"credit_goodwill_audit", "notes", redacted},
	{"bookmarks", "course_title", redacted},
	{"bookmarks", "lesson_title", redacted},
	{"bookmarks", "slide_title", redacted},
	{"character_create_jobs", "prompt", `NULL`},
	{"character_create_jobs", "result", `NULL`},
	{"character_versions", "spec", emptyObj},
	{"course_brief_revisions", "brief", emptyObj},
	{"course_casts", "selection", emptyObj},
	{"course_creations", "request", emptyObj},
	{"course_creations", "response", `NULL`},
	{"course_lesson_stubs", "goal", redacted},
	{"course_lesson_stubs", "title", redacted},
	{"course_units", "title", redacted},
	{"courses", "description", redacted},
	{"courses", "title", redacted},
	{"generated_lessons", "title", redacted},
	{"generated_lessons", "manifest", emptyObj},
	{"lesson_cast_snapshots", "snapshot", emptyObj},
	{"lesson_completion_summaries", "summary", redacted},
	{"lesson_completion_summaries", "weak_topics", emptyArr},
	{"lesson_generations", "result_checkpoint", `NULL`},
	{"lesson_questions", "choices", emptyArr},
	{"lesson_questions", "explanation", redacted},
	{"lesson_questions", "prompt", redacted},
	{"lesson_retry_pools", "variants", emptyArr},
	{"lesson_slides", "html", redacted},
	{"lesson_slides", "narration", redacted},
	{"lesson_slides", "segments", emptyArr},
	{"lesson_test_answer_operations", "response_payload", emptyObj},
	{"lesson_test_sessions", "question_queue", emptyArr},
	{"lesson_test_sessions", "resolved_topics", emptyArr},
	{"lesson_test_variant_snapshots", "choices", emptyArr},
	{"lesson_test_variant_snapshots", "explanation", redacted},
	{"lesson_test_variant_snapshots", "instruction_snapshot", emptyObj},
	{"lesson_test_variant_snapshots", "prompt", redacted},
	{"marketplace_listings", "description", redacted},
	{"marketplace_listings", "search", `''::tsvector`},
	{"marketplace_listings", "tags", `'{}'`},
	{"marketplace_listings", "title", redacted},
	{"outline_drafts", "confirmed", `NULL`},
	{"outline_drafts", "history", emptyArr},
	{"outline_drafts", "outline", emptyArr},
	{"outline_proposals", "attempts", `NULL`},
	{"outline_proposals", "base_outline", emptyArr},
	{"outline_proposals", "decision_draft", `NULL`},
	{"outline_proposals", "operations", `NULL`},
	{"outline_proposals", "request", redacted},
	{"outline_proposals", "result_outline", `NULL`},
	{"publication_drafts", "changelog", redacted},
	{"publication_drafts", "snapshot", emptyObj},
	{"quick_note_enrichments", "links", `'{}'`},
	{"quick_note_enrichments", "summary", redacted},
	{"quick_note_enrichments", "tags", `'{}'`},
	{"quick_notes", "raw_text", redacted},
	{"quick_notes", "slide_title", redacted},
	{"slide_narrations", "narration_text", redacted},
	{"tutor_messages", "content", redacted},
}

// purgedTables hold credentials, one-time secrets or raw provider payloads.
// They are emptied: nonproduction never needs a session, code or webhook body.
var purgedTables = []string{"auth_refresh_tokens", "auth_sessions", "auth_codes", "auth_rate_limits", "auth_oauth_attempts", "credit_payment_payloads", "publication_preview_limits"}

// safeSuffix matches columns that hold identifiers, hashes or enumerations.
var safeSuffix = regexp.MustCompile(`(^|_)(id|ids|hash|key|state|status|kind|role|source|provider|model|version|outcome|currency|locale|time_zone|code|type|reference|ref|sku|canonical|event_id)$`)

// reviewedSafe lists text-like columns reviewed as free of learner content:
// operator, configuration, pricing, billing-enumeration and evaluation data.
var reviewedSafe = map[string]bool{}

func init() {
	for _, column := range strings.Fields(`
character_create_jobs.operation character_screenings.verdict copy_generation_consents.pricing_version
course_invite_deliveries.channel course_invites.cancel_reason course_invites.method
course_copies.content_hash course_copies.source course_enrollments.level_override
course_lesson_stubs.reused_from_snapshot courses.visibility marketplace_listings.category marketplace_listings.language
credit_allocations.bucket credit_allocations.component credit_allocations.grant_date credit_artifacts.component
credit_checkout_orders.billing_period credit_checkout_orders.platform credit_checkout_orders.return_to
credit_cost_counters.scope credit_debt_history.bucket credit_dispute_keys.environment credit_entries.provenance credit_jobs.result credit_outbox.event credit_outbox.properties
credit_payment_attempts.event_id credit_payment_attempts.last_reason_code
credit_payment_inbox.environment credit_payment_sessions.environment credit_refund_allocations.source_id credit_reversals.environment credit_subscription_revocations.reason
credit_subscription_terms.benefits credit_subscription_terms.plan_name credit_subscription_transitions.terms
credit_subscriptions.environment credit_usage.usage eval_alert_configs.id eval_alert_events.properties eval_alert_events.scope
eval_operational_aggregates.task eval_reports.dataset_revision eval_reports.prompt_revision eval_reports.report_json eval_reports.task
friend_requests.method gamification_config.goal_options gamification_config.league_tiers
learner_profiles.appearance learner_profiles.haptics learner_profiles.level learner_profiles.motion learner_profiles.narration_voice narration_assets.settings_fingerprint slide_narrations.settings_fingerprint slide_narrations.speaker_key slide_narrations.segment_slide_id slide_narrations.actual_voice_key slide_narrations.voice_config_version slide_narrations.voice_fallback_reason learner_profiles.reminder_time
learner_social_settings.course_invites_from learner_social_settings.request_policy
lesson_test_variant_snapshots.reason lesson_test_variant_snapshots.variant_of lesson_questions.variant_of outline_proposals.decision
pricing_audit.action pricing_audit.change_class pricing_audit.diff_summary pricing_audit.override_reason pricing_audit.reason
pricing_drafts.change_class pricing_drafts.config pricing_drafts.created_by pricing_drafts.reviewed_by pricing_quote_policies.policy
pricing_quotes.snapshot pricing_verification_evidence.evidence pricing_version_cancellations.cancelled_by pricing_version_cancellations.reason
pricing_versions.config publication_drafts.content_hash publication_drafts.findings
publication_drafts.reason_codes publication_drafts.verdict publication_links.token_hash
publication_screenings.policy_version publication_screenings.reason_codes publication_screenings.verdict
social_rate_limits.bucket tutor_turns.end_reason tutor_turns.persona_fallback
username_config.profanity username_config.reserved`) {
		reviewedSafe[column] = true
	}
}

// ColumnClass says how the anonymizer treats a text-like column.
type ColumnClass int

// Column classes; Unclassified fails the drill until a migration author registers it.
const (
	Unclassified ColumnClass = iota
	ClassRedacted
	ClassPurged
	ClassSafe
)

// ClassifyColumn is exported so a new migration's author can test registration.
func ClassifyColumn(table, column string) ColumnClass {
	for _, rule := range piiRules {
		if rule.Table == table && rule.Column == column {
			return ClassRedacted
		}
	}
	for _, purged := range purgedTables {
		if purged == table {
			return ClassPurged
		}
	}
	if reviewedSafe[table+"."+column] || safeSuffix.MatchString(column) {
		return ClassSafe
	}
	return Unclassified
}

// Anonymize rewrites the throwaway database in place, in one transaction and
// before any other access. Triggers are bypassed (the restored copy is disposable)
// so immutable-ledger guards do not block redacting unrelated columns.
func Anonymize(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return errors.New("anonymization failed to begin")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SET LOCAL session_replication_role = replica"); err != nil {
		return errors.New("anonymization requires a superuser throwaway database")
	}
	for _, table := range purgedTables {
		if _, err = tx.Exec(ctx, "DELETE FROM "+pgx.Identifier{table}.Sanitize()); err != nil {
			return fmt.Errorf("anonymization purge %s failed", table)
		}
	}
	// Every rule runs in its own savepoint so one failure reports all of them.
	var failures []error
	for _, rule := range piiRules {
		sql := "UPDATE " + pgx.Identifier{rule.Table}.Sanitize() + " SET " + pgx.Identifier{rule.Column}.Sanitize() + " = " + rule.Expr
		nested, err := tx.Begin(ctx)
		if err != nil {
			return errors.New("anonymization savepoint failed")
		}
		if _, err = nested.Exec(ctx, sql); err != nil {
			_ = nested.Rollback(ctx)
			detail := "statement"
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				detail = pgErr.Code + " " + pgErr.ConstraintName // schema names only, never row data
			}
			failures = append(failures, fmt.Errorf("anonymization rule %s.%s failed (%s)", rule.Table, rule.Column, detail))
			continue
		}
		if err = nested.Commit(ctx); err != nil {
			return errors.New("anonymization savepoint failed")
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	return tx.Commit(ctx)
}
