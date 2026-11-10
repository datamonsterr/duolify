-- +goose Up
CREATE TABLE characters (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 current_version integer NOT NULL DEFAULT 1 CHECK(current_version>0),
 deleted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX characters_owner ON characters(learner_id) WHERE deleted_at IS NULL;
-- +goose StatementBegin
CREATE FUNCTION bound_character_count() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM id FROM learners WHERE id=NEW.learner_id FOR UPDATE;
 IF (SELECT count(*) FROM characters WHERE learner_id=NEW.learner_id AND deleted_at IS NULL)>=10 THEN RAISE EXCEPTION 'limit_exceeded'; END IF;
 RETURN NEW;
END; $$;
-- +goose StatementEnd
CREATE TRIGGER characters_count BEFORE INSERT ON characters FOR EACH ROW EXECUTE FUNCTION bound_character_count();
CREATE TABLE character_versions (
 character_id uuid NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
 version integer NOT NULL CHECK(version>0),
 spec jsonb NOT NULL CHECK(octet_length(spec::text)<=8192),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(character_id,version)
);
CREATE TABLE character_screenings (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 character_id uuid REFERENCES characters(id) ON DELETE CASCADE,
 verdict text NOT NULL DEFAULT 'pending' CHECK(verdict IN ('pending','approved','rejected')),
 reason_code text NOT NULL DEFAULT '',
 policy_version text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now()
);
-- +goose StatementBegin
CREATE FUNCTION immutable_character_payload() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'immutable character payload'; END; $$;
-- +goose StatementEnd
CREATE TRIGGER character_versions_immutable BEFORE UPDATE ON character_versions FOR EACH ROW EXECUTE FUNCTION immutable_character_payload();
CREATE INDEX character_edit_quota ON character_screenings(learner_id,created_at);
CREATE TABLE course_casts (
 course_id uuid PRIMARY KEY REFERENCES courses(id) ON DELETE CASCADE,
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 selection jsonb NOT NULL,
 version integer NOT NULL DEFAULT 1 CHECK(version>0)
);
-- Immutable independent payloads survive private Character deletion/purge.
CREATE TABLE lesson_cast_snapshots (
 course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 lesson_key text NOT NULL CHECK(length(lesson_key) BETWEEN 1 AND 100),
 snapshot jsonb NOT NULL,
 PRIMARY KEY(course_id,lesson_key)
);
CREATE TRIGGER lesson_cast_snapshots_immutable BEFORE UPDATE ON lesson_cast_snapshots FOR EACH ROW EXECUTE FUNCTION immutable_character_payload();
-- +goose Down
DROP TABLE lesson_cast_snapshots,course_casts,character_screenings,character_versions,characters;
DROP FUNCTION immutable_character_payload(),bound_character_count();
