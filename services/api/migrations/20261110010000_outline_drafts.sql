-- +goose Up
ALTER TABLE course_brief_revisions ADD CONSTRAINT course_brief_owner UNIQUE(id,learner_id);
CREATE TABLE outline_drafts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 brief_revision uuid NOT NULL,
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 outline jsonb NOT NULL CHECK(jsonb_typeof(outline)='array'),
 history jsonb NOT NULL DEFAULT '[]' CHECK(jsonb_typeof(history)='array' AND jsonb_array_length(history)<=50),
 confirmed jsonb CHECK(confirmed IS NULL OR jsonb_typeof(confirmed)='object'),
 is_recovery boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(brief_revision,learner_id) REFERENCES course_brief_revisions(id,learner_id) ON DELETE CASCADE
);
CREATE INDEX outline_drafts_owner ON outline_drafts(learner_id,brief_revision,updated_at DESC);

-- +goose Down
DROP TABLE outline_drafts;
ALTER TABLE course_brief_revisions DROP CONSTRAINT course_brief_owner;
