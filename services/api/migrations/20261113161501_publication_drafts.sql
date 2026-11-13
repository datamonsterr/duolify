-- +goose Up
-- Immutable, numbered publication snapshots (E15-01). Content only: no profile, notes, chat,
-- answers, progress or instructions; narration audio excluded by default (ADR 0012).
CREATE TABLE publication_drafts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  owner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  number integer NOT NULL CHECK (number >= 1),
  content_hash text NOT NULL CHECK (content_hash ~ '^[0-9a-f]{64}$'),
  snapshot jsonb NOT NULL,
  lesson_count integer NOT NULL CHECK (lesson_count >= 0),
  findings jsonb NOT NULL DEFAULT '[]'::jsonb,
  -- Mutable only through screening: pending_screening until a validated verdict arrives.
  verdict text NOT NULL DEFAULT 'pending_screening' CHECK (verdict IN ('pass','needs_review','block','pending_screening')),
  reason_codes text[] NOT NULL DEFAULT '{}',
  sampled_lessons integer NOT NULL DEFAULT 0 CHECK (sampled_lessons BETWEEN 0 AND 12),
  screened_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (course_id, number)
);
CREATE INDEX publication_drafts_owner_idx ON publication_drafts (owner_id, course_id);

-- snapshot, hash, number and ownership never change after creation.
-- +goose StatementBegin
CREATE FUNCTION publication_drafts_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.snapshot IS DISTINCT FROM OLD.snapshot OR NEW.content_hash <> OLD.content_hash
     OR NEW.number <> OLD.number OR NEW.course_id <> OLD.course_id OR NEW.owner_id <> OLD.owner_id
     OR NEW.findings IS DISTINCT FROM OLD.findings OR NEW.lesson_count <> OLD.lesson_count THEN
    RAISE EXCEPTION 'publication draft is immutable';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER publication_drafts_immutable BEFORE UPDATE ON publication_drafts
  FOR EACH ROW EXECUTE FUNCTION publication_drafts_immutable();

-- One row per screening attempt set: verdict, closed codes and cost only. Kept one year.
CREATE TABLE publication_screenings (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  draft_id uuid NOT NULL REFERENCES publication_drafts(id) ON DELETE CASCADE,
  owner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  verdict text NOT NULL CHECK (verdict IN ('pass','needs_review','block','pending_screening')),
  reason_codes text[] NOT NULL DEFAULT '{}',
  policy_version text NOT NULL DEFAULT '',
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 2),
  fallback_used boolean NOT NULL DEFAULT false,
  cost_nanos bigint NOT NULL DEFAULT 0 CHECK (cost_nanos >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX publication_screenings_draft_idx ON publication_screenings (draft_id);
CREATE INDEX publication_screenings_created_idx ON publication_screenings (created_at);

-- +goose Down
DROP TABLE publication_screenings;
DROP TRIGGER publication_drafts_immutable ON publication_drafts;
DROP FUNCTION publication_drafts_immutable();
DROP TABLE publication_drafts;
