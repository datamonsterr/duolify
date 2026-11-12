-- +goose Up
ALTER TABLE quick_notes
    ADD COLUMN enrichment_claim_id uuid,
    ADD COLUMN enrichment_lease_until timestamptz;

CREATE INDEX quick_notes_enrichment_leases
ON quick_notes(learner_id, enrichment_lease_until)
WHERE status = 'enriching';

-- +goose Down
DROP INDEX quick_notes_enrichment_leases;
ALTER TABLE quick_notes
    DROP COLUMN enrichment_claim_id,
    DROP COLUMN enrichment_lease_until;
