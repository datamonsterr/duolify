-- name: LearnerXPTotal :one
SELECT COALESCE(sum(amount),0)::bigint AS total_xp FROM xp_events WHERE learner_id=$1;
