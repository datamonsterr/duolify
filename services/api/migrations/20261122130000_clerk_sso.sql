-- +goose Up
-- ADR 0016: Clerk replaces first-party Google/Apple OAuth. Those providers were never
-- provisioned live, so no production binding exists; fixture rows are dropped.
DELETE FROM auth_provider_identities WHERE provider IN ('google','apple');
ALTER TABLE auth_provider_identities DROP CONSTRAINT auth_provider_identities_provider_check;
ALTER TABLE auth_provider_identities ADD CONSTRAINT auth_provider_identities_provider_check CHECK (provider = 'clerk');

-- SSO attempts carry the bridge hand-off. They are short-lived, hold no Clerk token,
-- and are purged from backups (internal/backup/anonymize.go).
DELETE FROM auth_oauth_attempts;
ALTER TABLE auth_oauth_attempts
 ALTER COLUMN nonce_hash DROP NOT NULL,
 ADD COLUMN learner_id uuid REFERENCES learners(id) ON DELETE CASCADE,
 ADD COLUMN clerk_user_id text,
 ADD COLUMN locale text NOT NULL DEFAULT 'en',
 ADD COLUMN time_zone text NOT NULL DEFAULT 'UTC',
 ADD COLUMN analytics_opt_out boolean NOT NULL DEFAULT false,
 ADD COLUMN handed_off_at timestamptz;
CREATE INDEX auth_oauth_attempt_learner_idx ON auth_oauth_attempts(learner_id) WHERE learner_id IS NOT NULL;

-- +goose Down
DROP INDEX auth_oauth_attempt_learner_idx;
DELETE FROM auth_oauth_attempts;
ALTER TABLE auth_oauth_attempts
 DROP COLUMN handed_off_at, DROP COLUMN analytics_opt_out, DROP COLUMN time_zone,
 DROP COLUMN locale, DROP COLUMN clerk_user_id, DROP COLUMN learner_id,
 ALTER COLUMN nonce_hash SET NOT NULL;
DELETE FROM auth_provider_identities WHERE provider = 'clerk';
ALTER TABLE auth_provider_identities DROP CONSTRAINT auth_provider_identities_provider_check;
ALTER TABLE auth_provider_identities ADD CONSTRAINT auth_provider_identities_provider_check CHECK (provider IN ('google','apple'));
