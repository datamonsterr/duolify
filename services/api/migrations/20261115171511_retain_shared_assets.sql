-- +goose Up
-- Retention of published lesson objects and creator attribution of copies (E15-11).

-- Who made the course a copy came from. Null after the owner deletes their account: the copy then shows
-- "deleted user" and keeps working.
ALTER TABLE course_copies ADD COLUMN source_owner_id uuid REFERENCES learners(id) ON DELETE SET NULL;
UPDATE course_copies cc SET source_owner_id = c.learner_id FROM courses c WHERE c.id = cc.source_course_id;

-- One row per published version (an object prefix). ref_count is the live-copy count at the last
-- retention pass; the pass recounts under the row lock, so the stored value is never trusted for a purge.
CREATE TABLE shared_asset_retention (
  prefix      text PRIMARY KEY CHECK (prefix ~ '^lessons/published/[0-9a-f-]{36}/$'),
  ref_count   integer NOT NULL DEFAULT 0 CHECK (ref_count >= 0),
  created_at  timestamptz NOT NULL DEFAULT now(),
  released_at timestamptz,
  purge_after timestamptz,
  purged_at   timestamptz,
  CHECK ((released_at IS NULL) = (purge_after IS NULL))
);
CREATE INDEX shared_asset_retention_due ON shared_asset_retention (purge_after) WHERE purged_at IS NULL AND purge_after IS NOT NULL;

-- Every pinned version and every version a copy lesson manifest points into is tracked.
INSERT INTO shared_asset_retention(prefix)
SELECT DISTINCT p FROM (
  SELECT 'lessons/published/'||draft_id::text||'/' AS p FROM course_copies WHERE draft_id IS NOT NULL
  UNION ALL
  SELECT substring(manifest_key from '^lessons/published/[0-9a-f-]{36}/') FROM lesson_manifests WHERE manifest_key LIKE 'lessons/published/%'
) x WHERE p IS NOT NULL
ON CONFLICT DO NOTHING;

-- A pin always has a retention row and cancels a pending release, however the copy was written.
-- +goose StatementBegin
CREATE FUNCTION register_copy_pin() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO shared_asset_retention(prefix) VALUES ('lessons/published/'||NEW.draft_id::text||'/')
  ON CONFLICT (prefix) DO UPDATE SET released_at=NULL, purge_after=NULL, purged_at=NULL;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER course_copies_register_pin AFTER INSERT OR UPDATE OF draft_id ON course_copies
  FOR EACH ROW WHEN (NEW.draft_id IS NOT NULL) EXECUTE FUNCTION register_copy_pin();

-- +goose Down
DROP TRIGGER course_copies_register_pin ON course_copies;
DROP FUNCTION register_copy_pin();
DROP TABLE shared_asset_retention;
ALTER TABLE course_copies DROP COLUMN source_owner_id;
