-- +goose Up
-- Verification evidence is immutable financial audit history retained for at least seven years.
CREATE TABLE pricing_verification_evidence (
    version text PRIMARY KEY REFERENCES pricing_versions(version),
    evidence jsonb NOT NULL CHECK (jsonb_typeof(evidence)='object'),
    verified_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO pricing_verification_evidence(version, evidence)
VALUES ('usd-launch-2026-11-08', '{
  "margin_floor_verified": true,
  "economics_verified": true,
  "mappings_verified": true,
  "official_sources": ["S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8", "S9", "S10", "S11", "S12"],
  "retrieved_on": "2026-11-08"
}'::jsonb);

-- +goose StatementBegin
CREATE FUNCTION prevent_pricing_evidence_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'pricing_verification_evidence is append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER pricing_verification_evidence_immutable
    BEFORE UPDATE OR DELETE OR TRUNCATE ON pricing_verification_evidence
    FOR EACH STATEMENT EXECUTE FUNCTION prevent_pricing_evidence_mutation();

-- +goose Down
-- Financial verification evidence requires at least seven-year retention.
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'pricing verification history requires seven-year retention; use a forward migration'; END $$;
-- +goose StatementEnd
