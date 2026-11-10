-- name: UpsertBookmark :one
INSERT INTO bookmarks (
    learner_id,
    action_key,
    course_id,
    lesson_id,
    slide_id,
    target_type,
    course_title,
    lesson_title,
    slide_title
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (learner_id, lesson_id, slide_id) DO UPDATE
SET updated_at = bookmarks.updated_at
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, target_type, course_title, lesson_title, slide_title, created_at, updated_at;

-- name: GetBookmarkByActionKey :one
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, target_type, course_title, lesson_title, slide_title, created_at, updated_at
FROM bookmarks
WHERE learner_id = $1 AND action_key = $2;

-- name: GetBookmarkByTarget :one
SELECT id, learner_id, action_key, course_id, lesson_id, slide_id, target_type, course_title, lesson_title, slide_title, created_at, updated_at
FROM bookmarks
WHERE learner_id = $1 AND lesson_id IS NOT DISTINCT FROM $2 AND slide_id IS NOT DISTINCT FROM $3;

-- name: GetBookmarkByID :one
SELECT b.id, b.learner_id, b.action_key, b.course_id, b.lesson_id, b.slide_id, b.target_type, b.course_title, b.lesson_title, b.slide_title, b.created_at, b.updated_at,
       (b.lesson_id IS NULL OR (l.id IS NOT NULL AND (c.id IS NULL OR c.deleted_at IS NULL))) AS available
FROM bookmarks b
LEFT JOIN course_lesson_stubs l ON l.id = b.lesson_id
LEFT JOIN courses c ON c.id = b.course_id
WHERE b.id = $1 AND b.learner_id = $2;

-- name: ListBookmarksByLearner :many
SELECT b.id, b.learner_id, b.action_key, b.course_id, b.lesson_id, b.slide_id, b.target_type, b.course_title, b.lesson_title, b.slide_title, b.created_at, b.updated_at,
       (b.lesson_id IS NULL OR (l.id IS NOT NULL AND (c.id IS NULL OR c.deleted_at IS NULL))) AS available
FROM bookmarks b
LEFT JOIN course_lesson_stubs l ON l.id = b.lesson_id
LEFT JOIN courses c ON c.id = b.course_id
WHERE b.learner_id = $1
ORDER BY b.created_at DESC;

-- name: ListBookmarksByLearnerAndCourse :many
SELECT b.id, b.learner_id, b.action_key, b.course_id, b.lesson_id, b.slide_id, b.target_type, b.course_title, b.lesson_title, b.slide_title, b.created_at, b.updated_at,
       (b.lesson_id IS NULL OR (l.id IS NOT NULL AND (c.id IS NULL OR c.deleted_at IS NULL))) AS available
FROM bookmarks b
LEFT JOIN course_lesson_stubs l ON l.id = b.lesson_id
LEFT JOIN courses c ON c.id = b.course_id
WHERE b.learner_id = $1 AND b.course_id = $2
ORDER BY b.created_at DESC;

-- name: DeleteBookmarkByID :one
DELETE FROM bookmarks
WHERE id = $1 AND learner_id = $2
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, target_type, course_title, lesson_title, slide_title, created_at, updated_at;

-- name: DeleteBookmarkByTarget :one
DELETE FROM bookmarks
WHERE learner_id = $1 AND lesson_id IS NOT DISTINCT FROM $2 AND slide_id IS NOT DISTINCT FROM $3
RETURNING id, learner_id, action_key, course_id, lesson_id, slide_id, target_type, course_title, lesson_title, slide_title, created_at, updated_at;

-- name: GetLessonContext :one
SELECT l.id AS lesson_id, l.title AS lesson_title, c.id AS course_id, c.title AS course_title
FROM course_lesson_stubs l
JOIN course_units u ON u.id = l.unit_id
JOIN courses c ON c.id = u.course_id
WHERE l.id = $1;
