-- +goose Up
ALTER TABLE auth_sessions ADD COLUMN reauthenticated_at timestamptz;
UPDATE auth_sessions SET reauthenticated_at=created_at;
ALTER TABLE auth_sessions ALTER COLUMN reauthenticated_at SET DEFAULT now();
ALTER TABLE auth_sessions ALTER COLUMN reauthenticated_at SET NOT NULL;

CREATE TABLE pricing_admin_grants (
    account_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
    granted_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE pricing_drafts (
    version text PRIMARY KEY CHECK (length(version) BETWEEN 1 AND 100),
    parent_version text NOT NULL REFERENCES pricing_versions(version),
    config jsonb NOT NULL CHECK (jsonb_typeof(config)='object'),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','submitted','rejected','approved')),
    change_class text NOT NULL CHECK (change_class IN ('adverse','non_adverse')),
    submitted_at timestamptz,
    reviewed_by text,
    reviewed_at timestamptz,
    CHECK (config->>'version'=version)
);
CREATE INDEX pricing_drafts_status_created_idx ON pricing_drafts(status,created_at DESC);

CREATE TABLE pricing_audit (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id text NOT NULL CHECK (length(actor_id)=64),
    action text NOT NULL CHECK (length(action) BETWEEN 1 AND 40),
    version text,
    change_class text CHECK (change_class IS NULL OR change_class IN ('adverse','non_adverse')),
    diff_summary jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(diff_summary)='object'),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000),
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 200),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX pricing_audit_version_created_idx ON pricing_audit(version,created_at);

-- Actor IDs are one-way hashes at insertion, so account erasure never needs to
-- rewrite an audit row.
-- +goose StatementBegin
CREATE FUNCTION protect_pricing_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='INSERT' THEN RETURN NEW; END IF;
    RAISE EXCEPTION 'pricing_audit is append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER pricing_audit_immutable BEFORE UPDATE OR DELETE ON pricing_audit
    FOR EACH ROW EXECUTE FUNCTION protect_pricing_audit();
CREATE TRIGGER pricing_audit_no_truncate BEFORE TRUNCATE ON pricing_audit
    FOR EACH STATEMENT EXECUTE FUNCTION protect_pricing_audit();

-- Drafts remain reviewable after their author leaves; identity becomes opaque.
-- +goose StatementBegin
CREATE FUNCTION pseudonymize_pricing_draft_actor() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    UPDATE pricing_drafts SET created_by='deleted:' || md5(OLD.id::text) WHERE created_by=OLD.id::text;
    UPDATE pricing_drafts SET reviewed_by='deleted:' || md5(OLD.id::text) WHERE reviewed_by=OLD.id::text;
    RETURN OLD;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER pricing_draft_actor_deleted BEFORE DELETE ON learners
    FOR EACH ROW EXECUTE FUNCTION pseudonymize_pricing_draft_actor();

-- +goose Down
-- Financial audit history is retained for at least seven years.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing audit history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
