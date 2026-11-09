-- name: UpsertQuickNote :one
INSERT INTO quick_notes (
    learner_id,
    action_key,
    course_id,
    lesson_id,
    slide_id,
    slide_title,
    source,
    raw_text,
    status
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (learner_id, action_key) DO UPDATE
SET updated_at = quick_notes.updated_at
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, created_at, updated_at;

-- name: GetQuickNoteByID :one
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, created_at, updated_at
FROM quick_notes
WHERE id = $1 AND learner_id = $2;

-- name: ListQuickNotesByLearner :many
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, slide_title, source, raw_text, status, reviewed, created_at, updated_at
FROM quick_notes
WHERE learner_id = $1
ORDER BY created_at DESC;

-- name: CheckLessonAuthorization :one
SELECT EXISTS (
    SELECT 1
    FROM course_lesson_stubs l
    JOIN course_units u ON u.id = l.unit_id
    JOIN courses c ON c.id = u.course_id
    LEFT JOIN course_enrollments e ON e.course_id = c.id AND e.learner_id = $2
    WHERE l.id = $1 AND (l.learner_id = $2 OR c.learner_id = $2 OR e.learner_id = $2)
) AS authorized;
