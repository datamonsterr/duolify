-- +goose Up
-- Course invites (E15-06). Learner-id based, so a rename never breaks one. An invite refused by
-- the recipient's settings or a block is stored with delivered=false: the sender sees it as sent,
-- it counts toward the daily cap, and nothing is delivered. In-app delivery is the invite row
-- itself; course_invite_deliveries is the outbox (in_app is written with the invite, push is
-- drained by the adapter) and its unique (invite, channel) pair dedupes every channel.
CREATE TABLE course_invites (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  course_id uuid NOT NULL REFERENCES courses(id) ON DELETE CASCADE,
  sender_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  recipient_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
  operation_key uuid NOT NULL,
  method text NOT NULL CHECK (method IN ('username','friend')),
  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','accepted','declined','expired','revoked')),
  cancel_reason text CHECK (cancel_reason IN ('block')),
  delivered boolean NOT NULL DEFAULT true,
  read_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  resolved_at timestamptz,
  copy_course_id uuid REFERENCES courses(id) ON DELETE SET NULL,
  CHECK (sender_id <> recipient_id),
  CHECK ((status = 'pending') = (resolved_at IS NULL)),
  UNIQUE (sender_id, operation_key)
);
-- One pending invite per recipient and Course.
CREATE UNIQUE INDEX course_invites_pending_idx ON course_invites (recipient_id, course_id) WHERE status = 'pending';
CREATE INDEX course_invites_inbox_idx ON course_invites (recipient_id, created_at DESC) WHERE status = 'pending' AND delivered;
CREATE INDEX course_invites_sender_idx ON course_invites (sender_id, created_at DESC);
CREATE INDEX course_invites_course_idx ON course_invites (course_id, created_at DESC);
CREATE INDEX course_invites_expiry_idx ON course_invites (expires_at) WHERE status = 'pending';
CREATE INDEX course_invites_resolved_idx ON course_invites (resolved_at) WHERE resolved_at IS NOT NULL;

CREATE TABLE course_invite_deliveries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  invite_id uuid NOT NULL REFERENCES course_invites(id) ON DELETE CASCADE,
  channel text NOT NULL CHECK (channel IN ('in_app','push')),
  status text NOT NULL CHECK (status IN ('pending','sent','suppressed','failed')),
  attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  created_at timestamptz NOT NULL DEFAULT now(),
  sent_at timestamptz,
  UNIQUE (invite_id, channel)
);
CREATE INDEX course_invite_deliveries_pending_idx ON course_invite_deliveries (created_at, id) WHERE channel = 'push' AND status = 'pending';

-- +goose Down
DROP TABLE course_invite_deliveries;
DROP TABLE course_invites;
