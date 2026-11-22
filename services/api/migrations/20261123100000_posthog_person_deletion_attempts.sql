-- +goose Up
-- The queue is now drained by a periodic worker job; failed attempts are counted (no learner data).
ALTER TABLE posthog_person_deletion_requests
 ADD COLUMN attempts integer NOT NULL DEFAULT 0,
 ADD COLUMN last_attempt_at timestamptz;

-- +goose Down
ALTER TABLE posthog_person_deletion_requests DROP COLUMN last_attempt_at, DROP COLUMN attempts;
