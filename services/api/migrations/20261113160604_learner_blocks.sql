-- +goose Up
-- One row per ordered blocker->blocked pair. A block hides the pair from each other in
-- both directions; rows go with either learner (no hash of an erased learner is kept).
CREATE TABLE learner_blocks (
  blocker_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  blocked_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (blocker_id, blocked_id),
  CHECK (blocker_id <> blocked_id)
);
CREATE INDEX learner_blocks_blocked_idx ON learner_blocks (blocked_id);

-- Social limits stay in configuration next to the others.
ALTER TABLE username_config
  ADD COLUMN block_max integer NOT NULL DEFAULT 500 CHECK (block_max BETWEEN 1 AND 5000);

-- Who may send this learner a Course invite (E15 reads it through CanInvite).
ALTER TABLE learner_social_settings
  ADD COLUMN course_invites_from text NOT NULL DEFAULT 'friends' CHECK (course_invites_from IN ('friends','everyone','nobody'));

-- +goose Down
ALTER TABLE learner_social_settings DROP COLUMN course_invites_from;
ALTER TABLE username_config DROP COLUMN block_max;
DROP TABLE learner_blocks;
