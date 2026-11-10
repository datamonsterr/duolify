-- name: GetLessonStub :one
SELECT * FROM course_lesson_stubs WHERE id = $1;

-- name: GetLessonGeneration :one
SELECT * FROM lesson_generations WHERE id = $1;

-- name: GetLessonCompletion :one
SELECT * FROM lesson_completion_outbox WHERE learner_id = $1 AND lesson_id = $2;

-- name: GetLessonRetryPool :one
SELECT * FROM lesson_retry_pools WHERE lesson_stub_id = $1 AND topic_id = $2;

-- name: ListLessonRetryPools :many
SELECT * FROM lesson_retry_pools WHERE lesson_stub_id = $1;

-- name: InsertLessonCompletionSummary :one
INSERT INTO lesson_completion_summaries (learner_id, course_id, lesson_id, summary, weak_topics)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (learner_id, lesson_id) DO UPDATE SET summary = EXCLUDED.summary, weak_topics = EXCLUDED.weak_topics
RETURNING *;

-- name: ListCourseLessonCompletionSummaries :many
SELECT * FROM lesson_completion_summaries
WHERE learner_id = $1 AND course_id = $2
ORDER BY completed_at ASC;
