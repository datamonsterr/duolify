-- +goose Up
-- Replace the global key without rewriting or discarding existing event rows.
ALTER TABLE lesson_playback_sync_events DROP CONSTRAINT lesson_playback_sync_events_pkey;
ALTER TABLE lesson_playback_sync_events ADD PRIMARY KEY (learner_id, action_key);

-- +goose Down
-- Keep all rows if independent learners have since reused a key. PostgreSQL
-- refuses the global constraint in that case and rolls this migration back.
ALTER TABLE lesson_playback_sync_events DROP CONSTRAINT lesson_playback_sync_events_pkey;
ALTER TABLE lesson_playback_sync_events ADD PRIMARY KEY (action_key);
