-- name: CreateCreditEntry :one
INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance)
VALUES ($1, $2, $3, $4, $5, COALESCE($6, '{}'::jsonb)) RETURNING *;

-- name: ListCreditEntries :many
SELECT * FROM credit_entries WHERE learner_id = $1 ORDER BY id ASC;

-- name: ListCreditEntriesCursor :many
SELECT * FROM credit_entries
WHERE learner_id = $1 AND ($2::bigint = 0 OR id < $2::bigint)
ORDER BY id DESC
LIMIT $3;
