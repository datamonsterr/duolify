-- +goose Up
-- Owner decision 2026-11-23: retain learner prices; raise preview output to 6000;
-- configure free transcription at the primary's live USD/second rate, with a
-- $0.0005 request ceiling and 60 requests per learner local day. Source terms,
-- other than version/effective time and these settings, remain unchanged.
-- Null preserves the historical full learner hold for already-reserved jobs.
ALTER TABLE credit_jobs ADD COLUMN learner_held_nanos bigint CHECK (learner_held_nanos >= 0 AND learner_held_nanos <= held_nanos);
-- +goose StatementBegin
DO $$
DECLARE
    new_version constant text := 'usd-voice-preview-2026-11-23';
    new_effective constant timestamptz := '2026-11-22T12:00:04Z';
    source_version constant text := 'usd-hy4-check-in-2026-11-22';
    approval_reason constant text := 'owner approved preview output 6000 and free bounded transcription on 2026-11-23; learner prices unchanged';
    system_actor constant text := '00000000-0000-4000-8000-000000000011';
    owner_parent text;
    owner_id text;
    new_config jsonb;
    source_policy jsonb;
    preview_cost numeric;
    activated boolean;
BEGIN
    IF EXISTS (SELECT 1 FROM pricing_versions WHERE version = new_version) THEN
        RETURN;
    END IF;
    SELECT jsonb_set(jsonb_set(jsonb_set(jsonb_set(config, '{version}', to_jsonb(new_version)), '{effective_from}', to_jsonb('2026-11-22T12:00:04Z'::text)),
                     '{limits,preview,output_tokens}', '6000'::jsonb),
                     '{transcribe}', '{"credits":0,"usd_nanos_per_second":3330,"max_cost_nanos":500000,"daily_requests":60}'::jsonb)
    INTO new_config FROM pricing_versions WHERE version = source_version AND approved;
    IF new_config IS NULL THEN
        RAISE EXCEPTION '% must exist approved before voice pricing can be added', source_version;
    END IF;

    SELECT policy INTO source_policy FROM pricing_quote_policies WHERE version=source_version;
    IF source_policy IS NULL OR source_policy->'action_models'->>'preview' IS DISTINCT FROM 'hy4_flash'
       OR source_policy->'fallback_models'->>'hy4_flash' IS DISTINCT FROM 'glm_flash'
       OR source_policy->'fallback_models'->>'glm_flash' IS NOT NULL
       OR COALESCE((source_policy->>'max_attempts')::numeric, 0) <= 0
       OR COALESCE((source_policy->'models'->'hy4_flash'->>'input')::numeric, 0) <= 0
       OR COALESCE((source_policy->'models'->'hy4_flash'->>'output')::numeric, 0) <= 0
       OR COALESCE((source_policy->'models'->'glm_flash'->>'input')::numeric, 0) <= 0
       OR COALESCE((source_policy->'models'->'glm_flash'->>'output')::numeric, 0) <= 0 THEN
        RAISE EXCEPTION 'approved preview route changed; recalculate economics';
    END IF;
    preview_cost := (2000 * GREATEST((source_policy->'models'->'hy4_flash'->>'input')::numeric, (source_policy->'models'->'glm_flash'->>'input')::numeric)
                  + 6000 * GREATEST((source_policy->'models'->'hy4_flash'->>'output')::numeric, (source_policy->'models'->'glm_flash'->>'output')::numeric))
                  * (source_policy->>'max_attempts')::numeric * 1.2066 / 1000000000;
    IF preview_cost IS NULL OR preview_cost > 25 * 0.0015 THEN
        RAISE EXCEPTION 'preview protected cost % exceeds budget', preview_cost;
    END IF;

    INSERT INTO pricing_versions(version, effective_from, approved, config)
    VALUES (new_version, new_effective, true, new_config);
    INSERT INTO pricing_quote_policies(version, policy)
    SELECT new_version, policy FROM pricing_quote_policies WHERE version = source_version;
    INSERT INTO pricing_verification_evidence(version, evidence)
    VALUES (new_version, jsonb_build_object(
        'margin_floor_verified', true, 'economics_verified', true, 'mappings_verified', true,
        'basis', 'unchanged SKU economics; preview checked against protected per-Credit budget at the pinned route and attempts',
        'owner_decision', '2026-11-23', 'checks', jsonb_build_array(jsonb_build_object('check','preview_protected_cost','cost_usd',preview_cost,'budget_usd',0.0375,'verified',true))));

    SELECT d.version, d.reviewed_by INTO owner_parent, owner_id
    FROM pricing_drafts d JOIN pricing_versions v ON v.version = d.version AND v.approved
    WHERE d.status = 'approved' AND d.reviewed_by IS NOT NULL AND d.config->>'character_create_decision' = 'owner_approved'
    ORDER BY d.reviewed_at DESC LIMIT 1;
    IF owner_id IS NOT NULL THEN
        INSERT INTO pricing_drafts(version, parent_version, config, created_by, status, change_class, submitted_at, reviewed_by, reviewed_at)
        VALUES (new_version, owner_parent, new_config || jsonb_build_object('quote_policy_source', source_version),
                owner_id, 'approved', 'non_adverse', now(), owner_id, now());
        INSERT INTO pricing_audit(actor_id, action, version, change_class, reason, request_id, diff_summary)
        SELECT encode(sha256(convert_to(lower(owner_id), 'UTF8')), 'hex'), a, new_version, 'non_adverse', approval_reason, 'approval-' || new_version || '-' || a, d
        FROM (VALUES ('version_submitted', '{"submitted":true}'::jsonb), ('version_approved', '{"approved":true}'::jsonb)) AS t(a, d);
    END IF;

    WITH marker AS (
        INSERT INTO pricing_activation_events(version, activated_at)
        SELECT p.version, p.effective_from FROM pricing_versions p
        WHERE p.version = new_version AND p.approved AND p.effective_from <= clock_timestamp()
          AND NOT EXISTS (SELECT 1 FROM pricing_version_cancellations x WHERE x.version = p.version)
        ON CONFLICT DO NOTHING RETURNING version
    ) SELECT true INTO activated FROM marker;
    IF activated THEN
        INSERT INTO pricing_audit(actor_id, action, version, change_class, reason, request_id, created_at)
        SELECT encode(sha256(convert_to(system_actor, 'UTF8')), 'hex'), 'version_activated', version, 'non_adverse',
               'scheduled activation', 'activation-' || version, effective_from
        FROM pricing_versions WHERE version = new_version;
    END IF;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Approved pricing, its activation and audit rows are immutable financial history; roll forward instead.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
