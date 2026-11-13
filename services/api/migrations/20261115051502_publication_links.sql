-- +goose Up
-- Share-link visibility and revocable share tokens (E15-02).

-- course_visibility enum: private (default) | link (share-link visible) | public (marketplace, E15-03+).
CREATE TYPE course_visibility AS ENUM ('private', 'link', 'public');
ALTER TABLE courses ADD COLUMN visibility course_visibility NOT NULL DEFAULT 'private';

-- publication_links: one active share token per course, stored only as SHA-256 hex.
CREATE TABLE publication_links (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  course_id   UUID NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  token_hash  TEXT NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at  TIMESTAMPTZ
);
-- Index for token lookups on the preview path.
CREATE INDEX ON publication_links(token_hash) WHERE revoked_at IS NULL;
-- At most one active link per course (partial unique).
CREATE UNIQUE INDEX publication_links_course_active ON publication_links(course_id) WHERE revoked_at IS NULL;

-- Per-IP counters for the anonymous preview: keyed by a hash of the address, never the address,
-- and rows expire with their window. Not backed up as user data.
CREATE TABLE publication_preview_limits (
  key_hash   TEXT PRIMARY KEY CHECK (key_hash ~ '^[0-9a-f]{64}$'),
  count      INTEGER NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX ON publication_preview_limits(expires_at);

-- +goose Down
DROP TABLE publication_preview_limits;
DROP TABLE publication_links;
ALTER TABLE courses DROP COLUMN visibility;
DROP TYPE course_visibility;
