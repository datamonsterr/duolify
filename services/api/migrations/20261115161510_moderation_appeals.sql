-- +goose Up
-- Moderation, audited enforcement and one-time appeals (E15-10).

-- Same grant pattern as pricing_admin_grants. Grants are made by the project owner in the database (ADR 0012).
CREATE TABLE moderator_grants (
  account_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
  granted_at timestamptz NOT NULL DEFAULT now()
);

-- A moderated course cannot be relisted or republished until an appeal is overturned.
ALTER TABLE courses
  ADD COLUMN moderation_state text NOT NULL DEFAULT 'none' CHECK (moderation_state IN ('none','delisted','taken_down')),
  ADD COLUMN moderation_action_id uuid;
-- A severe takedown blocks existing copies. Normal actions leave them untouched.
ALTER TABLE course_copies ADD COLUMN blocked_at timestamptz, ADD COLUMN blocked_action_id uuid;
CREATE INDEX course_copies_blocked ON course_copies (blocked_action_id) WHERE blocked_at IS NOT NULL;

CREATE TABLE publishing_bans (
  learner_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
  action_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- The queue now also holds held publications (target id = course id, draft id kept), reviews and users.
ALTER TABLE moderation_queue_items DROP CONSTRAINT moderation_queue_items_target_type_check;
ALTER TABLE moderation_queue_items ADD CONSTRAINT moderation_queue_items_target_type_check CHECK (target_type IN ('course','review','user','publication'));
ALTER TABLE moderation_queue_items ADD COLUMN draft_id uuid REFERENCES publication_drafts(id) ON DELETE SET NULL, ADD COLUMN resolved_at timestamptz;
CREATE INDEX moderation_queue_retention ON moderation_queue_items (opened_at);

-- Append-only audit log. Ids of people are pseudonymized (set NULL) on account deletion, nothing else changes.
CREATE TABLE moderation_actions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  queue_item_id uuid,
  target_type text NOT NULL CHECK (target_type IN ('course','review','user','publication')),
  target_id uuid NOT NULL,
  course_id uuid,
  draft_id uuid,
  author_id uuid REFERENCES learners(id) ON DELETE SET NULL,
  moderator_id uuid REFERENCES learners(id) ON DELETE SET NULL,
  action text NOT NULL CHECK (action IN ('dismiss','warn','delist','takedown','ban_publishing')),
  reason_code text NOT NULL CHECK (reason_code IN ('unsafe','legal','inappropriate','spam','copyright','misleading','other','no_violation')),
  reason text NOT NULL CHECK (char_length(btrim(reason)) > 0 AND char_length(reason) <= 1000),
  severe boolean NOT NULL DEFAULT false,
  listing_was_active boolean NOT NULL DEFAULT false,
  prior_verdict text CHECK (prior_verdict IS NULL OR prior_verdict IN ('pass','needs_review','block','pending_screening')),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX moderation_actions_author ON moderation_actions (author_id, created_at DESC);
CREATE INDEX moderation_actions_target ON moderation_actions (target_type, target_id, created_at);
CREATE INDEX moderation_actions_retention ON moderation_actions (created_at);

-- +goose StatementBegin
CREATE FUNCTION moderation_actions_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    -- Only the retention purge may delete, and it declares itself for its own transaction.
    IF current_setting('duolify.audit_purge', true) = 'on' THEN
      RETURN OLD;
    END IF;
    RAISE EXCEPTION 'moderation audit is append-only';
  END IF;
  -- Account deletion may only null the person references.
  IF (NEW.author_id IS NOT NULL AND NEW.author_id IS DISTINCT FROM OLD.author_id)
     OR (NEW.moderator_id IS NOT NULL AND NEW.moderator_id IS DISTINCT FROM OLD.moderator_id)
     OR (to_jsonb(NEW) - 'author_id' - 'moderator_id') IS DISTINCT FROM (to_jsonb(OLD) - 'author_id' - 'moderator_id') THEN
    RAISE EXCEPTION 'moderation audit is append-only';
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER moderation_actions_append_only BEFORE UPDATE OR DELETE ON moderation_actions
FOR EACH ROW EXECUTE FUNCTION moderation_actions_append_only();

CREATE TABLE moderation_appeals (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  -- One appeal per action: the unique key is the one-appeal rule.
  action_id uuid NOT NULL UNIQUE REFERENCES moderation_actions(id) ON DELETE CASCADE,
  author_id uuid REFERENCES learners(id) ON DELETE SET NULL,
  text text NOT NULL CHECK (char_length(text) <= 1000),
  status text NOT NULL DEFAULT 'open' CHECK (status IN ('open','upheld','overturned')),
  decided_by uuid REFERENCES learners(id) ON DELETE SET NULL,
  decision_reason text NOT NULL DEFAULT '' CHECK (char_length(decision_reason) <= 1000),
  decided_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX moderation_appeals_open ON moderation_appeals (created_at) WHERE status='open';

-- Erasing the author clears the appeal text, including restored rows and direct FK deletion.
-- The reviewer must differ from the moderator who acted, and an appeal is decided once.
-- +goose StatementBegin
CREATE FUNCTION moderation_appeals_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE acted uuid;
BEGIN
  IF NEW.author_id IS NULL THEN
    NEW.text := '';
  END IF;
  IF OLD.status <> 'open' AND (NEW.status <> OLD.status OR NEW.decision_reason IS DISTINCT FROM OLD.decision_reason) THEN
    RAISE EXCEPTION 'appeal already decided';
  END IF;
  IF NEW.status <> 'open' AND OLD.status = 'open' THEN
    SELECT moderator_id INTO acted FROM moderation_actions WHERE id = NEW.action_id;
    IF NEW.decided_by IS NULL OR NEW.decided_by = acted OR NEW.decided_by = NEW.author_id THEN
      RAISE EXCEPTION 'appeal must be decided by another moderator';
    END IF;
  END IF;
  RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER moderation_appeals_guard BEFORE UPDATE ON moderation_appeals
FOR EACH ROW EXECUTE FUNCTION moderation_appeals_guard();

-- +goose Down
DROP TABLE moderation_appeals;
DROP FUNCTION moderation_appeals_guard();
DROP TABLE moderation_actions;
DROP FUNCTION moderation_actions_append_only();
ALTER TABLE moderation_queue_items DROP COLUMN resolved_at, DROP COLUMN draft_id;
DELETE FROM moderation_queue_items WHERE target_type = 'publication';
ALTER TABLE moderation_queue_items DROP CONSTRAINT moderation_queue_items_target_type_check;
ALTER TABLE moderation_queue_items ADD CONSTRAINT moderation_queue_items_target_type_check CHECK (target_type IN ('course'));
DROP TABLE publishing_bans;
ALTER TABLE course_copies DROP COLUMN blocked_action_id, DROP COLUMN blocked_at;
ALTER TABLE courses DROP COLUMN moderation_action_id, DROP COLUMN moderation_state;
DROP TABLE moderator_grants;
