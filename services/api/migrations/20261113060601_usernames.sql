-- +goose Up
-- Reserved and profanity lists are configuration. Operators extend them with UPDATE;
-- matching folds case, separators and common digit look-alikes (see internal/social).
CREATE TABLE username_config (
  id boolean PRIMARY KEY DEFAULT true CHECK (id),
  version text NOT NULL,
  hold_days integer NOT NULL CHECK (hold_days BETWEEN 1 AND 365),
  reserved text[] NOT NULL,
  profanity text[] NOT NULL
);
INSERT INTO username_config(version,hold_days,reserved,profanity) VALUES (
  'launch-v1',30,
  ARRAY['admin','administrator','support','duolify','moderator','mod','staff','system','root','official','security','billing','help','team','api','null','undefined'],
  ARRAY['fuck','shit','cunt','bitch','asshole']
);

-- Display case is kept in username; canonical is the unique lowercase form. The client
-- and API apply NFKC first, and the ASCII-only CHECK keeps lower() equal to the NFKC fold.
CREATE TABLE learner_usernames (
  learner_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
  username text NOT NULL CHECK (username ~ '^[A-Za-z][A-Za-z0-9_.]{2,19}$' AND username !~ '[_.]$' AND username !~ '[_.]{2}'),
  canonical text GENERATED ALWAYS AS (lower(username)) STORED NOT NULL,
  claimed_at timestamptz NOT NULL DEFAULT now(),
  changed_at timestamptz,
  CONSTRAINT learner_usernames_canonical_key UNIQUE (canonical)
);

-- Released names stay reserved for hold_days. previous_owner_ref is a SHA-256 of the
-- learner UUID so the previous owner (rename hold) can reclaim it while a deleted
-- account keeps no learner link. Shared by the deletion hold and the E6-02 rename hold.
CREATE TABLE username_holds (
  canonical text PRIMARY KEY CHECK (canonical = lower(canonical)),
  previous_owner_ref text NOT NULL CHECK (previous_owner_ref ~ '^[0-9a-f]{64}$'),
  released_at timestamptz NOT NULL DEFAULT now(),
  held_until timestamptz NOT NULL
);
CREATE INDEX username_holds_held_until_idx ON username_holds(held_until);

-- Created now with safe defaults so E6-02..E6-04 add no learner-linked tables.
CREATE TABLE learner_social_settings (
  learner_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
  discoverable boolean NOT NULL DEFAULT true,
  request_policy text NOT NULL DEFAULT 'everyone' CHECK (request_policy IN ('everyone','friends_of_friends','nobody')),
  show_streak_to_friends boolean NOT NULL DEFAULT true
);

-- One mechanism for account deletion (live and tombstone replay) and rename.
-- +goose StatementBegin
CREATE FUNCTION hold_released_username() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  INSERT INTO username_holds(canonical,previous_owner_ref,held_until)
  VALUES (OLD.canonical, encode(sha256(convert_to(OLD.learner_id::text,'UTF8')),'hex'),
          now() + make_interval(days => (SELECT hold_days FROM username_config WHERE id)))
  ON CONFLICT (canonical) DO UPDATE SET previous_owner_ref=EXCLUDED.previous_owner_ref,
    released_at=now(), held_until=EXCLUDED.held_until;
  RETURN NULL;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER learner_usernames_hold_on_delete AFTER DELETE ON learner_usernames
  FOR EACH ROW EXECUTE FUNCTION hold_released_username();
CREATE TRIGGER learner_usernames_hold_on_rename AFTER UPDATE OF username ON learner_usernames
  FOR EACH ROW WHEN (OLD.canonical IS DISTINCT FROM NEW.canonical) EXECUTE FUNCTION hold_released_username();

-- +goose Down
DROP TABLE learner_social_settings, username_holds, learner_usernames, username_config;
DROP FUNCTION hold_released_username();
