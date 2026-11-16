-- name: CreateLearner :one
INSERT INTO learners (display_name) VALUES ($1) RETURNING *;

-- name: GetLearner :one
SELECT * FROM learners WHERE id = $1;

-- name: SetLearnerBirthDate :execrows
UPDATE learners SET birth_date = $1 WHERE id = sqlc.arg(learner_id)::uuid AND birth_date IS NULL;

-- name: GetLearnerBirthDate :one
SELECT birth_date FROM learners WHERE id = sqlc.arg(learner_id)::uuid;
