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
