-- +goose Up
CREATE TABLE tutor_threads (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tutor_threads_owner_course ON tutor_threads(learner_id,course_id,created_at DESC);
CREATE TABLE tutor_messages (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 seq bigint GENERATED ALWAYS AS IDENTITY,
 thread_id uuid NOT NULL REFERENCES tutor_threads(id) ON DELETE CASCADE,
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 role text NOT NULL CHECK(role IN ('user','assistant')),
 content text NOT NULL CHECK(length(content)>0),
 status text NOT NULL DEFAULT 'complete' CHECK(status IN ('complete','interrupted')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX tutor_messages_thread ON tutor_messages(thread_id,seq);
CREATE INDEX tutor_messages_owner ON tutor_messages(learner_id);
-- Turn rows hold no text: they bind an idempotency key and a quote to one charge.
CREATE TABLE tutor_turns (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
 idempotency_key text NOT NULL CHECK(length(idempotency_key) BETWEEN 1 AND 128),
 quote_id uuid NOT NULL UNIQUE REFERENCES pricing_quotes(id),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','completed','interrupted')),
 thread_id uuid REFERENCES tutor_threads(id) ON DELETE SET NULL,
 user_message_id uuid,
 assistant_message_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(learner_id,idempotency_key)
);
-- +goose Down
DROP TABLE tutor_turns,tutor_messages,tutor_threads;
