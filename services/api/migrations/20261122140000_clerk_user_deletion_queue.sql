-- +goose Up
-- Durable, retryable request to delete a Clerk user after the Duolify account is erased
-- (ADR 0016 follow-up). The row is written in the same transaction that deletes the Learner,
-- because the Clerk user id lives in auth_provider_identities and cascades away with it. It
-- holds no Learner id: the worker only needs the Clerk user id, and removes the row once
-- Clerk confirms the user is gone (or reports it as already gone). Purged from nonproduction
-- backups (internal/backup/anonymize.go).
CREATE TABLE clerk_user_deletion_requests (
 clerk_user_id text PRIMARY KEY CHECK (length(clerk_user_id) BETWEEN 1 AND 255),
 requested_at timestamptz NOT NULL DEFAULT now(),
 attempts integer NOT NULL DEFAULT 0,
 last_attempt_at timestamptz
);

-- +goose Down
DROP TABLE clerk_user_deletion_requests;
