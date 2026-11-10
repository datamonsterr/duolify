package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ClassifiedLearnerTables lists every table holding learner data and its retention policy.
var ClassifiedLearnerTables = map[string]string{
	"character_create_jobs":             "erased_prompt_terminal_or_24h",
	"characters":                        "erased",
	"character_versions":                "erased_via_character",
	"character_screenings":              "erased_30_days",
	"course_casts":                      "erased",
	"lesson_cast_snapshots":             "erased_independent_payload",
	"lesson_playback_sync_events":       "erased",
	"lesson_playback_positions":         "erased",
	"lesson_manifests":                  "erased",
	"course_creations":                  "erased",
	"courses":                           "erased",
	"course_units":                      "erased",
	"course_lesson_stubs":               "erased",
	"course_enrollments":                "erased",
	"lesson_generations":                "erased",
	"generated_lessons":                 "erased",
	"lesson_slides":                     "erased",
	"lesson_questions":                  "erased",
	"lesson_retry_pools":                "erased",
	"lesson_test_sessions":              "erased",
	"lesson_test_answers":               "erased",
	"lesson_test_answer_operations":     "erased",
	"lesson_completion_outbox":          "erased",
	"lesson_completion_summaries":       "erased",
	"lesson_test_variant_snapshots":     "erased",
	"credit_subscription_lifecycle":     "financial_retained_via_subscription",
	"credit_subscription_transitions":   "financial_retained_via_subscription",
	"credit_subscription_schedule":      "financial_retained_via_subscription",
	"credit_subscription_revocations":   "financial_retained_via_subscription",
	"credit_subscription_cancellations": "financial_retained_via_subscription",
	"credit_subscriptions":              "financial_retained_via_order",
	"credit_subscription_terms":         "financial_retained_via_subscription",
	"credit_payment_sessions":           "financial_retained_via_order",
	"credit_payment_inbox":              "financial_retained_via_session",
	"credit_payment_results":            "financial_retained_via_order",
	"credit_payment_payloads":           "redacted_expire_30_days",
	"credit_checkout_orders":            "financial_retained_anonymized",
	"credit_allocations":                "financial_retained_via_job",
	"credit_usage":                      "financial_retained_via_job",
	"credit_cost_counters":              "financial_retained_pseudonymous",
	"credit_artifacts":                  "financial_retained_pseudonymous",
	"credit_jobs":                       "financial_retained_anonymized",
	"credit_outbox":                     "financial_retained_anonymized",
	"course_brief_revisions":            "erased",
	"outline_drafts":                    "erased",
	"outline_proposals":                 "erased",
	"learner_profiles":                  "erased",
	"auth_identities":                   "erased",
	"auth_provider_identities":          "erased",
	"auth_sessions":                     "revoked_and_erased",
	"credit_entries":                    "financial_retained_pseudonymous",
	"pricing_quotes":                    "financial_retained_anonymized",
	"pricing_admin_grants":              "revoked_and_erased",
	"pricing_drafts":                    "financial_retained_pseudonymous",
	"pricing_audit":                     "financial_retained_pseudonymous",
	"pricing_version_cancellations":     "financial_retained_anonymized",
	"credit_timezones":                  "financial_retained_pseudonymous",
}

// LearnerReference is a stable pseudonym used only for legally retained records.
func LearnerReference(learnerID string) string {
	sum := sha256.Sum256([]byte(learnerID))
	return hex.EncodeToString(sum[:])
}

func (s *Service) registerPrivacy() {
	s.mux.Handle("GET /v1/me/data-export", s.Protect(s.privacyAccess("export", s.exportAccountData)))
	s.mux.Handle("POST /v1/me/deletion-request", s.Protect(s.privacyAccess("delete", s.deleteAccount)))
}

func (s *Service) exportAccountData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := contextID(r)
	var learner string
	if err := s.pool.QueryRow(ctx, "SELECT id::text FROM learners WHERE id=$1", id).Scan(&learner); err != nil {
		respond(w, 404, map[string]string{"error": "Account data is unavailable."})
		return
	}
	tables := map[string]json.RawMessage{}
	type exportQuery struct {
		table string
		query string
	}
	queries := []exportQuery{
		{table: "character_versions", query: `SELECT COALESCE(jsonb_agg(to_jsonb(v)),'[]'::jsonb) FROM character_versions v JOIN characters c ON c.id=v.character_id WHERE c.learner_id=$1`},
		{table: "credit_subscription_lifecycle", query: `SELECT COALESCE(jsonb_agg(to_jsonb(x)),'[]'::jsonb) FROM credit_subscription_lifecycle x JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id WHERE o.learner_id=$1`},
		{table: "credit_subscription_transitions", query: `SELECT COALESCE(jsonb_agg(to_jsonb(x)),'[]'::jsonb) FROM credit_subscription_transitions x JOIN credit_subscriptions b USING(subscription_id) JOIN credit_checkout_orders o ON o.id=b.order_id WHERE o.learner_id=$1`},
		{table: "credit_subscriptions", query: `SELECT COALESCE(jsonb_agg(to_jsonb(s)),'[]'::jsonb) FROM credit_subscriptions s JOIN credit_checkout_orders o ON o.id=s.order_id WHERE o.learner_id=$1`},
		{table: "credit_subscription_terms", query: `SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM credit_subscription_terms t JOIN credit_subscriptions s USING(subscription_id) JOIN credit_checkout_orders o ON o.id=s.order_id WHERE o.learner_id=$1`},
		{table: "credit_payment_sessions", query: `SELECT COALESCE(jsonb_agg(to_jsonb(p)),'[]'::jsonb) FROM credit_payment_sessions p JOIN credit_checkout_orders o ON o.id=p.order_id WHERE o.learner_id=$1`},
		{table: "credit_payment_inbox", query: `SELECT COALESCE(jsonb_agg(to_jsonb(i)),'[]'::jsonb) FROM credit_payment_inbox i WHERE EXISTS(SELECT 1 FROM credit_checkout_orders o LEFT JOIN credit_payment_sessions s ON s.order_id=o.id LEFT JOIN credit_subscriptions b ON b.order_id=o.id LEFT JOIN credit_subscription_terms t ON t.subscription_id=b.subscription_id LEFT JOIN credit_payment_results r ON r.order_id=o.id WHERE o.learner_id=$1 AND (i.session_id=s.session_id OR i.session_id=b.subscription_id OR i.session_id=t.invoice_id OR i.event_id=r.event_id))`},
		{table: "credit_payment_results", query: `SELECT COALESCE(jsonb_agg(to_jsonb(p)),'[]'::jsonb) FROM credit_payment_results p JOIN credit_checkout_orders o ON o.id=p.order_id WHERE o.learner_id=$1`},
		{table: "credit_payment_payloads", query: `SELECT COALESCE(jsonb_agg(to_jsonb(p)),'[]'::jsonb) FROM credit_payment_payloads p JOIN credit_payment_inbox i ON i.event_id=p.event_id WHERE p.expires_at>now() AND EXISTS(SELECT 1 FROM credit_checkout_orders o LEFT JOIN credit_payment_sessions s ON s.order_id=o.id LEFT JOIN credit_subscriptions b ON b.order_id=o.id LEFT JOIN credit_subscription_terms t ON t.subscription_id=b.subscription_id LEFT JOIN credit_payment_results r ON r.order_id=o.id WHERE o.learner_id=$1 AND (i.session_id=s.session_id OR i.session_id=b.subscription_id OR i.session_id=t.invoice_id OR i.event_id=r.event_id))`},
		{table: "course_brief_revisions", query: `SELECT COALESCE(jsonb_agg(to_jsonb(b)),'[]'::jsonb) FROM course_brief_revisions b WHERE learner_id=$1 AND expires_at>now()`},
		{table: "outline_proposals", query: `SELECT COALESCE(jsonb_agg(to_jsonb(p)),'[]'::jsonb) FROM outline_proposals p JOIN outline_drafts d ON d.id=p.draft_id JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE p.learner_id=$1 AND b.expires_at>now()`},
		{table: "outline_drafts", query: `SELECT COALESCE(jsonb_agg(to_jsonb(d)),'[]'::jsonb) FROM outline_drafts d JOIN course_brief_revisions b ON b.id=d.brief_revision WHERE d.learner_id=$1 AND b.expires_at>now()`},
		{table: "learners", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'display_name',display_name,'created_at',created_at)),'[]'::jsonb) FROM learners WHERE id=$1`},
		{table: "learner_profiles", query: `SELECT COALESCE(jsonb_agg(to_jsonb(p)),'[]'::jsonb) FROM learner_profiles p WHERE learner_id=$1`},
		{table: "auth_identities", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('email',email,'created_at',created_at)),'[]'::jsonb) FROM auth_identities WHERE learner_id=$1`},
		{table: "auth_provider_identities", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('provider',provider,'verified_email',verified_email,'created_at',created_at)),'[]'::jsonb) FROM auth_provider_identities WHERE learner_id=$1`},
		{table: "auth_sessions", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'created_at',created_at,'expires_at',expires_at,'revoked_at',revoked_at)),'[]'::jsonb) FROM auth_sessions WHERE learner_id=$1`},
		{table: "credit_entries", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'amount',amount,'kind',kind,'reference',reference,'created_at',created_at)),'[]'::jsonb) FROM credit_entries WHERE learner_id=$1`},
		{table: "pricing_quotes", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',q.id,'pricing_version',q.pricing_version,'snapshot',q.snapshot,'created_at',q.created_at,'expires_at',q.expires_at,'accepted_at',c.accepted_at)),'[]'::jsonb) FROM pricing_quotes q LEFT JOIN pricing_quote_consents c ON c.quote_id=q.id WHERE q.learner_id=$1`},
		{table: "pricing_admin_grants", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('account_id',account_id,'granted_at',granted_at)),'[]'::jsonb) FROM pricing_admin_grants WHERE account_id=$1`},
		{table: "credit_timezones", query: `SELECT COALESCE(jsonb_agg(jsonb_build_object('id',id,'time_zone',time_zone,'effective_at',effective_at,'created_at',created_at)),'[]'::jsonb) FROM credit_timezones WHERE learner_id=$1`},
	}
	for _, table := range []string{"credit_subscription_schedule", "credit_subscription_revocations", "credit_subscription_cancellations"} {
		queries = append(queries, exportQuery{table: table, query: "SELECT COALESCE(jsonb_agg(to_jsonb(g)),'[]'::jsonb) FROM " + table + " g JOIN credit_subscription_terms t USING(period_key) JOIN credit_subscriptions s USING(subscription_id) JOIN credit_checkout_orders o ON o.id=s.order_id WHERE o.learner_id=$1"})
	}
	for _, table := range []string{"character_create_jobs", "characters", "character_screenings", "course_casts", "lesson_cast_snapshots", "lesson_playback_sync_events", "lesson_playback_positions", "lesson_manifests", "course_creations", "courses", "course_units", "course_lesson_stubs", "course_enrollments", "lesson_generations", "generated_lessons", "lesson_slides", "lesson_questions", "lesson_retry_pools", "lesson_test_sessions", "lesson_test_answers", "lesson_test_answer_operations", "lesson_completion_outbox", "lesson_completion_summaries", "lesson_test_variant_snapshots", "credit_checkout_orders", "credit_jobs", "credit_outbox"} {
		queries = append(queries, exportQuery{table: table, query: "SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM " + table + " t WHERE learner_id=$1"})
	}
	for _, table := range []string{"credit_allocations", "credit_usage"} {
		queries = append(queries, exportQuery{table: table, query: "SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM " + table + " t JOIN credit_jobs j ON j.id=t.job_id WHERE j.learner_id=$1"})
	}
	for _, table := range []string{"credit_cost_counters", "credit_artifacts"} {
		queries = append(queries, exportQuery{table: table, query: "SELECT COALESCE(jsonb_agg(to_jsonb(t)),'[]'::jsonb) FROM " + table + " t WHERE owner_reference=encode(sha256(convert_to($1::text,'UTF8')),'hex')"})
	}
	for _, item := range queries {
		var raw []byte
		if err := s.pool.QueryRow(ctx, item.query, id).Scan(&raw); err != nil {
			respond(w, 503, map[string]string{"error": "Could not prepare account export. Retry."})
			return
		}
		tables[item.table] = json.RawMessage(raw)
	}
	now := time.Now().UTC()
	w.Header().Set("Content-Disposition", `attachment; filename="duolify-account-export.json"`)
	respond(w, 200, map[string]any{"schema_version": 1, "exported_at": now, "expires_at": now.Add(7 * 24 * time.Hour), "tables": tables})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmation string `json:"confirmation"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	err := d.Decode(&in)
	var extra any
	if err != nil || !errors.Is(d.Decode(&extra), io.EOF) || in.Confirmation != "DELETE" {
		respond(w, 400, map[string]string{"error": "Type DELETE to confirm account removal."})
		return
	}
	ctx := r.Context()
	id := contextID(r)
	var learner string
	if err := s.pool.QueryRow(ctx, "SELECT id::text FROM learners WHERE id=$1", id).Scan(&learner); err != nil {
		respond(w, 404, map[string]string{"error": "Account data is unavailable."})
		return
	}
	ref := LearnerReference(learner)
	// The external intent is durable first: a failed SQL commit cannot resurrect
	// this learner on restore. Retrying writes the same immutable object.
	if s.deletionLedger == nil || s.deletionLedger.Append(ctx, ref) != nil {
		respond(w, 503, map[string]string{"error": "Could not request account removal. Retry."})
		return
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not request account removal. Retry."})
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, "SELECT release_deleted_credit_jobs($1)", ref); err == nil {
		_, err = tx.Exec(ctx, "UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE learner_id=$1", id)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE credit_entries SET learner_id=NULL,owner_reference=$2 WHERE learner_id=$1", id, ref)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE credit_timezones SET learner_id=NULL,owner_reference=$2 WHERE learner_id=$1", id, ref)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "UPDATE pricing_quotes SET learner_id=NULL WHERE learner_id=$1", id)
	}
	if err == nil {
		// Payment Sessions, inbox, outcomes and redacted payloads link through this
		// immutable order only. Severing it erases their learner identity together.
		_, err = tx.Exec(ctx, "UPDATE credit_checkout_orders SET learner_id=NULL WHERE learner_id=$1", id)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "DELETE FROM pricing_admin_grants WHERE account_id=$1", id)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO account_deletion_tombstones(learner_reference) VALUES($1) ON CONFLICT DO NOTHING", ref)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "INSERT INTO posthog_person_deletion_requests(distinct_id) VALUES($1::uuid) ON CONFLICT DO NOTHING", learner)
	}
	if err == nil {
		_, err = tx.Exec(ctx, "DELETE FROM learners WHERE id=$1", id)
	}
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		respond(w, 503, map[string]string{"error": "Could not request account removal. Retry."})
		return
	}
	posthogAccepted := false
	if s.personDeleter != nil {
		requestCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		if s.personDeleter.DeletePerson(requestCtx, learner) == nil {
			posthogAccepted = true
			_, _ = s.pool.Exec(ctx, "DELETE FROM posthog_person_deletion_requests WHERE distinct_id=$1::uuid", learner)
		}
		cancel()
	}
	respond(w, 200, map[string]any{"deleted": true, "deleted_at": time.Now().UTC(), "posthog_deletion_accepted": posthogAccepted, "retained_financial_reference": ref})
}

// ApplyDeletionTombstones is called after restoring a database and before its
// HTTP listener opens. It deletes any personal account whose pseudonym matches
// a durable tombstone, while leaving null-linked financial rows intact.
func ApplyDeletionTombstones(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, "SELECT id::text FROM learners")
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, id := range ids {
		ref := LearnerReference(id)
		var exists bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM account_deletion_tombstones WHERE learner_reference=$1)", ref).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err = tx.Exec(ctx, "SELECT release_deleted_credit_jobs($1)", ref); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE auth_sessions SET revoked_at=COALESCE(revoked_at,now()) WHERE learner_id=$1::uuid", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE credit_entries SET learner_id=NULL,owner_reference=$2 WHERE learner_id=$1::uuid", id, ref); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE credit_timezones SET learner_id=NULL,owner_reference=$2 WHERE learner_id=$1::uuid", id, ref); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE pricing_quotes SET learner_id=NULL WHERE learner_id=$1::uuid", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE credit_checkout_orders SET learner_id=NULL WHERE learner_id=$1::uuid", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM pricing_admin_grants WHERE account_id=$1::uuid", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "INSERT INTO posthog_person_deletion_requests(distinct_id) VALUES($1::uuid) ON CONFLICT DO NOTHING", id); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "DELETE FROM learners WHERE id=$1::uuid", id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
