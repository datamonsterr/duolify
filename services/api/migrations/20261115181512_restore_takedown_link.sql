-- +goose Up
-- Track which share links were revoked by a moderation takedown so an overturned
-- appeal can re-activate exactly those links (issue #207 / fix for E15-10).
-- Only takedown actions set this column; owner-revoked or expired links are NULL
-- and are therefore never re-activated by an overturn.
ALTER TABLE publication_links
  ADD COLUMN revoked_by_action_id uuid REFERENCES moderation_actions(id) ON DELETE SET NULL;

CREATE INDEX publication_links_revoked_by_action ON publication_links (revoked_by_action_id)
  WHERE revoked_by_action_id IS NOT NULL;

-- Extend the action enum to include the restore_link audit action written when an
-- overturned takedown re-activates the share link it had revoked.
ALTER TABLE moderation_actions DROP CONSTRAINT moderation_actions_action_check;
ALTER TABLE moderation_actions ADD CONSTRAINT moderation_actions_action_check
  CHECK (action IN ('dismiss','warn','delist','takedown','ban_publishing','restore_link'));

-- +goose Down
ALTER TABLE moderation_actions DROP CONSTRAINT moderation_actions_action_check;
ALTER TABLE moderation_actions ADD CONSTRAINT moderation_actions_action_check
  CHECK (action IN ('dismiss','warn','delist','takedown','ban_publishing'));
DROP INDEX publication_links_revoked_by_action;
ALTER TABLE publication_links DROP COLUMN revoked_by_action_id;
