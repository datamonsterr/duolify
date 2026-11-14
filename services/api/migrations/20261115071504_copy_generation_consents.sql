-- +goose Up
-- Evidence of the explicit consent behind a Lesson generated in a Course copy (E15-04): which quote,
-- at which Pricing config version, for which Lesson. Learner-linked, erased with the learner/copy.
CREATE TABLE copy_generation_consents (
  generation_id uuid PRIMARY KEY REFERENCES lesson_generations(id) ON DELETE CASCADE,
  learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  copy_course_id uuid NOT NULL REFERENCES course_copies(copy_course_id) ON DELETE CASCADE,
  lesson_id uuid NOT NULL REFERENCES course_lesson_stubs(id) ON DELETE CASCADE,
  quote_id uuid NOT NULL,
  pricing_version text NOT NULL CHECK (pricing_version <> ''),
  consented_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX copy_generation_consents_learner ON copy_generation_consents (learner_id, consented_at);
CREATE INDEX copy_generation_consents_copy ON copy_generation_consents (copy_course_id);

-- +goose Down
DROP TABLE copy_generation_consents;
