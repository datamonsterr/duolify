-- +goose Up
-- Provenance for a Quick note saved from a tutor reply. No text: the note keeps its own raw_text.
-- Ids are cleared (not cascaded) when the source thread is deleted so the note stays readable.
CREATE TABLE quick_note_chat_sources (
 note_id uuid PRIMARY KEY REFERENCES quick_notes(id) ON DELETE CASCADE,
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 thread_id uuid,
 message_id uuid,
 lesson_id uuid,
 unavailable boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((unavailable AND thread_id IS NULL AND message_id IS NULL) OR (NOT unavailable AND thread_id IS NOT NULL AND message_id IS NOT NULL))
);
CREATE INDEX quick_note_chat_sources_thread ON quick_note_chat_sources(thread_id) WHERE thread_id IS NOT NULL;
CREATE INDEX quick_note_chat_sources_owner ON quick_note_chat_sources(learner_id);
-- Text-free record that a thread was deleted. Replayed after a restore so backups cannot resurrect it.
CREATE TABLE tutor_thread_deletion_tombstones (
 thread_id uuid PRIMARY KEY,
 deleted_at timestamptz NOT NULL DEFAULT now()
);
-- +goose Down
DROP TABLE tutor_thread_deletion_tombstones, quick_note_chat_sources;
