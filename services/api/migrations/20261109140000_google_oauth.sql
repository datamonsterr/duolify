-- +goose Up
CREATE TABLE auth_provider_identities (
 provider text NOT NULL CHECK (provider IN ('google','apple')),
 subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 255),
 learner_id uuid NOT NULL REFERENCES learners(id) ON DELETE CASCADE,
 verified_email text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(provider, subject),
 UNIQUE(provider, learner_id)
);
CREATE INDEX auth_provider_learner_idx ON auth_provider_identities(learner_id);

CREATE TABLE auth_oauth_attempts (
 state_hash text PRIMARY KEY CHECK (length(state_hash)=64),
 nonce_hash text NOT NULL CHECK (length(nonce_hash)=64),
 code_challenge text NOT NULL CHECK (length(code_challenge)=43),
 redirect_uri text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz
);
CREATE INDEX auth_oauth_attempt_expiry_idx ON auth_oauth_attempts(expires_at);

-- +goose Down
DROP TABLE auth_oauth_attempts;
DROP TABLE auth_provider_identities;
