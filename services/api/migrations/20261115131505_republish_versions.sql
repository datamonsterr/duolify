-- +goose Up
-- Republished versions, explicit copy updates and stable lesson keys (E15-05).
-- Changelog is owner text bounded to 1000 characters; republish marks versions that count toward the daily cap.
ALTER TABLE publication_drafts
  ADD COLUMN changelog text NOT NULL DEFAULT '' CHECK (length(changelog) <= 1000),
  ADD COLUMN republish boolean NOT NULL DEFAULT false;
CREATE INDEX publication_drafts_republish_idx ON publication_drafts (course_id, created_at) WHERE republish;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION publication_drafts_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.snapshot IS DISTINCT FROM OLD.snapshot OR NEW.content_hash <> OLD.content_hash
     OR NEW.number <> OLD.number OR NEW.course_id <> OLD.course_id OR NEW.owner_id <> OLD.owner_id
     OR NEW.findings IS DISTINCT FROM OLD.findings OR NEW.lesson_count <> OLD.lesson_count
     OR NEW.changelog <> OLD.changelog OR NEW.republish <> OLD.republish THEN
    RAISE EXCEPTION 'publication draft is immutable';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd

-- Stable lesson key: set on the owner's stubs when a draft is built and copied to learner copies.
ALTER TABLE course_lesson_stubs
  ADD COLUMN lesson_key text CHECK (lesson_key IS NULL OR length(lesson_key) BETWEEN 1 AND 64),
  ADD COLUMN archived_at timestamptz,
  ADD COLUMN archived_from_version integer CHECK (archived_from_version IS NULL OR archived_from_version > 0);
CREATE INDEX course_lesson_stubs_key_idx ON course_lesson_stubs (lesson_key) WHERE lesson_key IS NOT NULL;
-- Archived lessons of a copy live in one hidden unit so unlock and outline queries never reach them.
ALTER TABLE course_units ADD COLUMN archived_at timestamptz;

-- lock_version is the optimistic lock for pin/update; forked_at marks a steered copy (no updates, ADR 0012).
ALTER TABLE course_copies
  ADD COLUMN lock_version integer NOT NULL DEFAULT 1 CHECK (lock_version > 0),
  ADD COLUMN forked_at timestamptz,
  ADD COLUMN updated_at timestamptz;

-- +goose Down
ALTER TABLE course_copies DROP COLUMN updated_at, DROP COLUMN forked_at, DROP COLUMN lock_version;
ALTER TABLE course_units DROP COLUMN archived_at;
DROP INDEX course_lesson_stubs_key_idx;
ALTER TABLE course_lesson_stubs DROP COLUMN archived_from_version, DROP COLUMN archived_at, DROP COLUMN lesson_key;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION publication_drafts_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.snapshot IS DISTINCT FROM OLD.snapshot OR NEW.content_hash <> OLD.content_hash
     OR NEW.number <> OLD.number OR NEW.course_id <> OLD.course_id OR NEW.owner_id <> OLD.owner_id
     OR NEW.findings IS DISTINCT FROM OLD.findings OR NEW.lesson_count <> OLD.lesson_count THEN
    RAISE EXCEPTION 'publication draft is immutable';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
DROP INDEX publication_drafts_republish_idx;
ALTER TABLE publication_drafts DROP COLUMN republish, DROP COLUMN changelog;
