-- name: CreateLearner :one
INSERT INTO learners (display_name) VALUES ($1) RETURNING *;

-- name: GetLearner :one
SELECT * FROM learners WHERE id = $1;
