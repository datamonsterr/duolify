-- +goose Up
CREATE TABLE course_casts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    guide_character_id text NOT NULL,
    supporting_character_ids jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(learner_id, course_id)
);

CREATE TABLE lesson_cast_snapshots (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
    lesson_id text NOT NULL,
    cast_source text NOT NULL DEFAULT 'default' CHECK(cast_source IN ('default', 'custom', 'mixed', 'published')),
    schema_version integer NOT NULL DEFAULT 1,
    rig_version text NOT NULL DEFAULT 'original-v1',
    voice_config_version text NOT NULL DEFAULT 'v1',
    speakers jsonb NOT NULL,
    fallback_used boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(course_id, lesson_id)
);

CREATE INDEX course_casts_learner ON course_casts(learner_id);
CREATE INDEX lesson_cast_snapshots_learner ON lesson_cast_snapshots(learner_id);

-- +goose Down
DROP TABLE lesson_cast_snapshots, course_casts;
