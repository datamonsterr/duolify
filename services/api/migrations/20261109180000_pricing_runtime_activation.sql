-- +goose Up
-- Cancellation and activation bookkeeping are append-only controls around
-- immutable catalogue versions. A failed process cannot rewrite financial history.
CREATE TABLE pricing_version_cancellations (
    version text PRIMARY KEY REFERENCES pricing_versions(version),
    cancelled_by text NOT NULL,
    cancelled_at timestamptz NOT NULL DEFAULT now(),
    reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 1000)
);

ALTER TABLE pricing_drafts ADD COLUMN rollback_source_version text REFERENCES pricing_versions(version);

CREATE TABLE pricing_activation_events (
    version text PRIMARY KEY REFERENCES pricing_versions(version),
    activated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementBegin
CREATE FUNCTION prevent_pricing_control_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'pricing runtime controls are append-only';
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER pricing_cancellations_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON pricing_version_cancellations
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_control_mutation();
CREATE TRIGGER pricing_activation_events_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON pricing_activation_events
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_control_mutation();

-- +goose Down
-- Runtime activation markers are financial audit history; retain them.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing runtime history requires retention; use a forward migration'; END $$;
-- +goose StatementEnd
