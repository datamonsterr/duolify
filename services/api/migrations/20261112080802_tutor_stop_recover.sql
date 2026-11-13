-- +goose Up
-- A Learner can stop a reply: the stored prefix is `stopped`; disconnects and failures stay `interrupted`.
ALTER TABLE tutor_messages DROP CONSTRAINT tutor_messages_status_check;
ALTER TABLE tutor_messages ADD CONSTRAINT tutor_messages_status_check CHECK(status IN ('complete','interrupted','stopped'));
ALTER TABLE tutor_turns DROP CONSTRAINT tutor_turns_state_check;
ALTER TABLE tutor_turns ADD CONSTRAINT tutor_turns_state_check CHECK(state IN ('pending','completed','interrupted','stopped'));
-- Text-free recovery facts: why the reply ended, the reservation that settled it (no FK: credit rows keep
-- their financial retention after the Learner is erased) and whether its usage was estimated.
ALTER TABLE tutor_turns
 ADD COLUMN end_reason text CHECK(end_reason IS NULL OR length(end_reason) BETWEEN 1 AND 64),
 ADD COLUMN job_id uuid,
 ADD COLUMN usage_estimated boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE tutor_turns DROP COLUMN usage_estimated, DROP COLUMN job_id, DROP COLUMN end_reason;
UPDATE tutor_turns SET state='interrupted' WHERE state='stopped';
ALTER TABLE tutor_turns DROP CONSTRAINT tutor_turns_state_check;
ALTER TABLE tutor_turns ADD CONSTRAINT tutor_turns_state_check CHECK(state IN ('pending','completed','interrupted'));
UPDATE tutor_messages SET status='interrupted' WHERE status='stopped';
ALTER TABLE tutor_messages DROP CONSTRAINT tutor_messages_status_check;
ALTER TABLE tutor_messages ADD CONSTRAINT tutor_messages_status_check CHECK(status IN ('complete','interrupted'));
