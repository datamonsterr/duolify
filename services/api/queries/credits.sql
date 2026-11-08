-- name: CreateCreditEntry :one
INSERT INTO credit_entries (learner_id, amount, kind, reference)
VALUES ($1, $2, $3, $4) RETURNING *;

-- name: ListCreditEntries :many
SELECT * FROM credit_entries WHERE learner_id = $1 ORDER BY id;
