-- name: GetAuthenticatedLearner :one
SELECT l.id::text AS id, l.display_name, p.locale, p.time_zone, p.background, p.analytics_opt_out
FROM learners l JOIN learner_profiles p ON p.learner_id=l.id
WHERE l.id::text=sqlc.arg(learner_id)::text;
