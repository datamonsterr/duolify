-- +goose Up
ALTER TABLE courses ADD COLUMN outline_version bigint NOT NULL DEFAULT 1 CHECK(outline_version>0);
CREATE TABLE course_replan_proposals (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
 quote_id uuid NOT NULL UNIQUE REFERENCES pricing_quotes(id),
 job_id uuid REFERENCES credit_jobs(id),
 outline_version bigint NOT NULL CHECK(outline_version>0),
 input jsonb NOT NULL,
 result_outline jsonb,
 diff jsonb NOT NULL DEFAULT '[]',
 attempts jsonb NOT NULL DEFAULT '[]',
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','proposed','failed')),
 reason text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '24 hours',
 decided_at timestamptz,
 decision text CHECK(decision IN ('confirmed','discarded'))
);
CREATE INDEX course_replan_proposals_owner ON course_replan_proposals(learner_id,course_id,created_at);
CREATE INDEX course_replan_proposals_expiry ON course_replan_proposals(expires_at);
-- +goose Down
DROP TABLE course_replan_proposals;
ALTER TABLE courses DROP COLUMN outline_version;
