-- +goose Up
ALTER TABLE auth_codes ADD COLUMN code_ciphertext bytea, ADD COLUMN last_sent_at timestamptz;
ALTER TABLE auth_refresh_tokens ADD COLUMN successor_ciphertext bytea;
ALTER TABLE auth_rate_limits ADD COLUMN email_hash text;
CREATE INDEX auth_rate_limits_email_hash_idx ON auth_rate_limits(email_hash) WHERE email_hash IS NOT NULL;
-- Existing codes and OAuth transactions used the undifferentiated root key.
-- Invalidate them at rollout instead of retaining an unsafe legacy verifier.
DELETE FROM auth_codes;
DELETE FROM auth_oauth_attempts;

-- +goose Down
DROP INDEX auth_rate_limits_email_hash_idx;
ALTER TABLE auth_rate_limits DROP COLUMN email_hash;
ALTER TABLE auth_refresh_tokens DROP COLUMN successor_ciphertext;
ALTER TABLE auth_codes DROP COLUMN code_ciphertext, DROP COLUMN last_sent_at;
