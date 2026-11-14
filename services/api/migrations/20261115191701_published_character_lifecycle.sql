-- +goose Up
ALTER TABLE course_copies ADD COLUMN character_cast jsonb;
ALTER TABLE publication_drafts ADD COLUMN character_event_emitted boolean NOT NULL DEFAULT false;

-- Content addressed, ownership-free shared outputs. References are counted under this row's lock.
CREATE TABLE shared_character_assets (
 content_hash text PRIMARY KEY CHECK (content_hash ~ '^[0-9a-f]{64}$'),
 object_key text NOT NULL UNIQUE,
 ref_count bigint NOT NULL DEFAULT 0 CHECK (ref_count >= 0),
 released_at timestamptz,
 purge_after timestamptz,
 purged_at timestamptz
);
-- Separate immutable moderation overlay; no persona text, owner or private library links.
CREATE TABLE character_suppressions (
 snapshot_id text PRIMARY KEY CHECK (snapshot_id ~ '^snapshot-[0-9a-f]{64}$'),
 reason_code text NOT NULL CHECK (reason_code IN ('unsafe','legal')),
 created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE character_suppressions;
DROP TABLE shared_character_assets;
ALTER TABLE publication_drafts DROP COLUMN character_event_emitted;
ALTER TABLE course_copies DROP COLUMN character_cast;
