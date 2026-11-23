-- +goose Up
-- Guided chat drafts (change conversational-flows-and-mascot): one row per Learner and script,
-- holding bounded answers so a chat resumes after a restart or on another device. Secrets are
-- never stored (the client leaves secure steps out). Rows expire after 30 days idle and are
-- purged by the retention worker. Cascades on account erasure.
CREATE TABLE guided_chat_drafts (
    learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
    script_id text NOT NULL CHECK (script_id ~ '^[a-z]+(\.[a-z_]+)+$' AND char_length(script_id) <= 64),
    script_version integer NOT NULL CHECK (script_version >= 1),
    step_id text NOT NULL CHECK (char_length(step_id) <= 64),
    answers jsonb NOT NULL CHECK (jsonb_typeof(answers) = 'object' AND octet_length(answers::text) <= 16384),
    updated_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL DEFAULT now() + interval '30 days',
    PRIMARY KEY (learner_id, script_id)
);
CREATE INDEX guided_chat_drafts_expires ON guided_chat_drafts(expires_at);

-- +goose Down
DROP TABLE guided_chat_drafts;
