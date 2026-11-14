-- +goose Up
CREATE TABLE slide_narrations (
 lesson_id uuid NOT NULL REFERENCES lesson_manifests(lesson_id) ON DELETE CASCADE,
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 slide_id text NOT NULL CHECK(slide_id ~ '^[A-Za-z0-9_-]{1,100}$'),
 status text NOT NULL CHECK(status IN ('pending','ready','failed','disabled')),
 -- Plain narration text copied from the stored manifest so Postgres dumps hold text and status.
 narration_text text NOT NULL DEFAULT '' CHECK(char_length(narration_text) <= 20000),
 -- sha256 over text, voice id, model and settings fingerprint; names the shared objects.
 content_hash text NOT NULL DEFAULT '' CHECK(content_hash = '' OR content_hash ~ '^[a-f0-9]{64}$'),
 voice_key text NOT NULL DEFAULT 'default',
 voice_id text NOT NULL DEFAULT '',
 model_id text NOT NULL DEFAULT '',
 settings_fingerprint text NOT NULL DEFAULT '',
 audio_key text NOT NULL DEFAULT '' CHECK(audio_key = '' OR audio_key LIKE 'lessons/narration/%'),
 alignment_key text NOT NULL DEFAULT '' CHECK(alignment_key = '' OR alignment_key LIKE 'lessons/narration/%'),
 characters integer NOT NULL DEFAULT 0 CHECK(characters >= 0),
 audio_bytes integer NOT NULL DEFAULT 0 CHECK(audio_bytes >= 0),
 duration_ms integer NOT NULL DEFAULT 0 CHECK(duration_ms >= 0),
 version integer NOT NULL DEFAULT 1 CHECK(version > 0),
 reservation_id uuid,
 failure_code text NOT NULL DEFAULT '' CHECK(char_length(failure_code) <= 64),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (lesson_id, slide_id),
 CHECK(status <> 'ready' OR (content_hash <> '' AND audio_key <> '' AND alignment_key <> '' AND duration_ms > 0))
);
CREATE INDEX slide_narrations_owner ON slide_narrations(learner_id);
CREATE INDEX slide_narrations_hash_ready ON slide_narrations(content_hash) WHERE status = 'ready';
-- +goose Down
DROP TABLE slide_narrations;
