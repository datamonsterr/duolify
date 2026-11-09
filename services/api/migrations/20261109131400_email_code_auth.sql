-- +goose Up
CREATE TABLE learner_profiles (
 learner_id uuid PRIMARY KEY REFERENCES learners(id) ON DELETE CASCADE,
 locale text NOT NULL DEFAULT 'en',
 time_zone text NOT NULL DEFAULT 'UTC',
 background text NOT NULL DEFAULT '',
 analytics_opt_out boolean NOT NULL DEFAULT false,
 age_confirmed_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_identities (
 email text PRIMARY KEY,
 learner_id uuid NOT NULL UNIQUE REFERENCES learners(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE auth_codes (
 email text PRIMARY KEY,
 code_hash text NOT NULL CHECK (length(code_hash)=64),
 expires_at timestamptz NOT NULL,
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts BETWEEN 0 AND 5),
 consumed_at timestamptz
);
CREATE TABLE auth_sessions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '30 days',
 revoked_at timestamptz
);
CREATE TABLE auth_refresh_tokens (
 token_hash text PRIMARY KEY CHECK (length(token_hash)=64),
 session_id uuid NOT NULL REFERENCES auth_sessions(id) ON DELETE CASCADE,
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz
);
CREATE INDEX auth_refresh_session_idx ON auth_refresh_tokens(session_id);
CREATE TABLE auth_rate_limits (
 key_hash text PRIMARY KEY,
 count integer NOT NULL,
 expires_at timestamptz NOT NULL
);
-- Remove pending challenges too when a known identity is deleted.
-- +goose StatementBegin
CREATE FUNCTION delete_identity_codes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM auth_codes WHERE email=OLD.email;
 RETURN OLD;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER auth_identity_deleted AFTER DELETE ON auth_identities
 FOR EACH ROW EXECUTE FUNCTION delete_identity_codes();

-- +goose Down
DROP TABLE auth_rate_limits, auth_refresh_tokens, auth_sessions;
DROP TABLE auth_identities;
DROP FUNCTION delete_identity_codes();
DROP TABLE auth_codes, learner_profiles;
