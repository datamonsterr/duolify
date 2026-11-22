-- +goose Up
-- Ledger of private generated-lesson object prefixes whose Course was hard-deleted. Rows are written
-- in the same transaction as the Course delete, so a crash cannot lose them. Objects go after
-- purge_after (past the nightly dump window), so a restorable dump never points at purged objects.
-- Published snapshots and narration audio have their own reference-counted retention and are not here.
CREATE TABLE course_object_purges (
  prefix       text PRIMARY KEY CHECK (prefix ~ '^lessons/generated/[0-9a-f-]{36}/$'),
  enqueued_at  timestamptz NOT NULL DEFAULT now(),
  purge_after  timestamptz NOT NULL,
  purged_at    timestamptz
);
CREATE INDEX course_object_purges_due ON course_object_purges (purge_after) WHERE purged_at IS NULL;

-- +goose Down
DROP TABLE course_object_purges;
