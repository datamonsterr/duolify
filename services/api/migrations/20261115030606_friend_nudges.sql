-- +goose Up
-- Friend nudges (E6-07). One row is both the sender's pair-day quota and the
-- recipient's in-app inbox entry. Delivery is in-app only (ADR 0012).
-- (sender, operation_key) makes a retry a replay; (sender, recipient, sender_local_date)
-- allows one nudge per pair and sender-local day. Rows go with either learner and with
-- the friendship (removeFriendshipTx); expired rows are deleted by nudge_retention_days.
ALTER TABLE username_config
  ADD COLUMN nudge_retention_days integer NOT NULL DEFAULT 14 CHECK (nudge_retention_days BETWEEN 2 AND 14),
  ADD COLUMN nudge_sends_per_day integer NOT NULL DEFAULT 20 CHECK (nudge_sends_per_day >= 1);

ALTER TABLE social_rate_limits DROP CONSTRAINT social_rate_limits_bucket_check;
ALTER TABLE social_rate_limits ADD CONSTRAINT social_rate_limits_bucket_check
  CHECK (bucket IN ('search','username_attempt','friend_send','friend_link','friend_nudge'));

CREATE TABLE friend_nudges (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  sender_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  recipient_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  operation_key uuid NOT NULL,
  sender_local_date date NOT NULL,
  delivered boolean NOT NULL DEFAULT true,
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK (sender_id <> recipient_id),
  UNIQUE (sender_id, operation_key),
  UNIQUE (sender_id, recipient_id, sender_local_date)
);
CREATE INDEX friend_nudges_inbox_idx ON friend_nudges (recipient_id, created_at DESC) WHERE delivered;
CREATE INDEX friend_nudges_created_idx ON friend_nudges (created_at);

-- +goose Down
DROP TABLE friend_nudges;
DELETE FROM social_rate_limits WHERE bucket = 'friend_nudge';
ALTER TABLE social_rate_limits DROP CONSTRAINT social_rate_limits_bucket_check;
ALTER TABLE social_rate_limits ADD CONSTRAINT social_rate_limits_bucket_check
  CHECK (bucket IN ('search','username_attempt','friend_send','friend_link'));
ALTER TABLE username_config DROP COLUMN nudge_retention_days, DROP COLUMN nudge_sends_per_day;
