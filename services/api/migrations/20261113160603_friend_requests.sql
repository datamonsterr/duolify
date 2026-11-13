-- +goose Up
-- Friend limits are configuration next to the other social limits.
ALTER TABLE username_config
  ADD COLUMN friend_pending_max integer NOT NULL DEFAULT 20 CHECK (friend_pending_max BETWEEN 1 AND 200),
  ADD COLUMN friend_sends_per_day integer NOT NULL DEFAULT 50 CHECK (friend_sends_per_day BETWEEN 1 AND 1000),
  ADD COLUMN friend_max integer NOT NULL DEFAULT 500 CHECK (friend_max BETWEEN 1 AND 5000),
  ADD COLUMN friend_decline_cooldown_days integer NOT NULL DEFAULT 7 CHECK (friend_decline_cooldown_days BETWEEN 1 AND 90),
  ADD COLUMN friend_link_valid_days integer NOT NULL DEFAULT 30 CHECK (friend_link_valid_days BETWEEN 1 AND 365),
  ADD COLUMN friend_link_per_hour integer NOT NULL DEFAULT 60 CHECK (friend_link_per_hour BETWEEN 1 AND 1000);

ALTER TABLE social_rate_limits DROP CONSTRAINT social_rate_limits_bucket_check;
ALTER TABLE social_rate_limits ADD CONSTRAINT social_rate_limits_bucket_check
  CHECK (bucket IN ('search','username_attempt','friend_send','friend_link'));

-- One row per ordered sender->recipient pair; a re-send after the cooldown reuses it.
-- Accepted rows stay so accept is idempotent; removing the friendship deletes the pair's rows.
CREATE TABLE friend_requests (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sender_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  recipient_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined')),
  method text NOT NULL CHECK (method IN ('username','invite_link')),
  created_at timestamptz NOT NULL DEFAULT now(),
  decided_at timestamptz,
  CHECK (sender_id <> recipient_id),
  UNIQUE (sender_id, recipient_id)
);
-- At most one pending request per canonical pair, whichever learner sent it.
CREATE UNIQUE INDEX friend_requests_pending_pair_idx ON friend_requests (LEAST(sender_id, recipient_id), GREATEST(sender_id, recipient_id)) WHERE status = 'pending';
CREATE INDEX friend_requests_recipient_idx ON friend_requests (recipient_id, status);

-- Canonical ordered pair: the same two learners can only ever have one row.
CREATE TABLE friendships (
  id uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
  learner_low uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  learner_high uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (learner_low, learner_high),
  CHECK (learner_low < learner_high)
);
CREATE INDEX friendships_high_idx ON friendships (learner_high);

-- Only a SHA-256 of the token is stored; the token is shown once on creation.
CREATE TABLE friend_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  token_hash text NOT NULL UNIQUE CHECK (token_hash ~ '^[0-9a-f]{64}$'),
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz
);
CREATE UNIQUE INDEX friend_links_one_active_idx ON friend_links (owner_id) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE friend_links, friendships, friend_requests;
DELETE FROM social_rate_limits WHERE bucket IN ('friend_send','friend_link');
ALTER TABLE social_rate_limits DROP CONSTRAINT social_rate_limits_bucket_check;
ALTER TABLE social_rate_limits ADD CONSTRAINT social_rate_limits_bucket_check CHECK (bucket IN ('search','username_attempt'));
ALTER TABLE username_config DROP COLUMN friend_pending_max, DROP COLUMN friend_sends_per_day, DROP COLUMN friend_max, DROP COLUMN friend_decline_cooldown_days, DROP COLUMN friend_link_valid_days, DROP COLUMN friend_link_per_hour;
