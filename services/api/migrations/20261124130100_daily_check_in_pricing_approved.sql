-- +goose Up
-- Owner decision 2026-11-22 (ADR 0020, "Daily check-in credits"): the product owner approved exactly
-- daily_credits=150, first_time_bonus=250 and bank_days=7. This records that decision as the NEW immutable
-- approved version usd-hy4-check-in-2026-11-22, the then-active approved Hy4 routing version
-- usd-hy4-routing-2026-11-22 byte for byte except "version", "effective_from" (one second after the Hy4
-- version; effective_from is unique and already past at deploy) and the added "check_in" block. The quote
-- policy is copied verbatim. Prices, packs, plans, limits, daily_free (46) and routing are unchanged.
--
-- Rows written mirror 20261123110000_hy4_routing_pricing_approved.sql: pricing_versions (approved),
-- pricing_quote_policies, pricing_verification_evidence, the owner review trail only where an owner-approved
-- draft already exists (PRICING_OWNER_ID is never stored here; without one the character-creation and
-- design-chat owner gates keep failing closed), and pricing_activation_events plus its 'version_activated'
-- audit row. No gate, ActiveQuoteConfig check or mapping verifier is bypassed in code.
-- +goose StatementBegin
DO $$
DECLARE
    new_version constant text := 'usd-hy4-check-in-2026-11-22';
    new_effective constant timestamptz := '2026-11-22T12:00:02Z';
    source_version constant text := 'usd-hy4-routing-2026-11-22';
    approval_reason constant text := 'owner approved daily check-in credits 150/250/7-day bank on 2026-11-22; no other term changed (ADR 0020)';
    system_actor constant text := '00000000-0000-4000-8000-000000000011';
    owner_parent text;
    owner_id text;
    new_config jsonb;
    activated boolean;
BEGIN
    IF EXISTS (SELECT 1 FROM pricing_versions WHERE version = new_version) THEN
        RETURN;
    END IF;
    SELECT jsonb_set(jsonb_set(jsonb_set(config, '{version}', to_jsonb(new_version)), '{effective_from}', to_jsonb('2026-11-22T12:00:02Z'::text)),
                     '{check_in}', '{"daily_credits":150,"first_time_bonus":250,"bank_days":7}'::jsonb)
    INTO new_config FROM pricing_versions WHERE version = source_version AND approved;
    IF new_config IS NULL THEN
        RAISE EXCEPTION '% must exist approved before check-in credits can be added', source_version;
    END IF;

    INSERT INTO pricing_versions(version, effective_from, approved, config)
    VALUES (new_version, new_effective, true, new_config);
    INSERT INTO pricing_quote_policies(version, policy)
    SELECT new_version, policy FROM pricing_quote_policies WHERE version = source_version;
    INSERT INTO pricing_verification_evidence(version, evidence)
    VALUES (new_version, jsonb_build_object(
        'margin_floor_verified', true, 'economics_verified', true, 'mappings_verified', true,
        'basis', 'config identical to ' || source_version || ' except version, effective_from and the owner-approved check_in block',
        'owner_decision', '2026-11-22', 'checks', '[]'::jsonb));

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
