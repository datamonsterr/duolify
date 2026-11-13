-- +goose Up
-- A turn that delivered nothing keeps a text-free `released` tombstone, so a repeated or late Stop by its
-- owner stays idempotent and the same key can never start a second charge.
ALTER TABLE tutor_turns DROP CONSTRAINT tutor_turns_state_check;
ALTER TABLE tutor_turns ADD CONSTRAINT tutor_turns_state_check CHECK(state IN ('pending','completed','interrupted','stopped','released'));

-- +goose Down
DELETE FROM tutor_turns WHERE state='released';
ALTER TABLE tutor_turns DROP CONSTRAINT tutor_turns_state_check;
ALTER TABLE tutor_turns ADD CONSTRAINT tutor_turns_state_check CHECK(state IN ('pending','completed','interrupted','stopped'));
