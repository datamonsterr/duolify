-- +goose Up
-- Deleted instructions are purged 30 days after soft deletion; keep that sweep an index scan.
CREATE INDEX course_instructions_deleted_purge ON course_instructions(deleted_at) WHERE status='deleted';
-- +goose Down
DROP INDEX course_instructions_deleted_purge;
