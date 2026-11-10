-- +goose Up
-- Legacy alias-based queues cannot safely be resumed. Preserve their history,
-- then start a fresh session with opaque, exact question references on next GET.
UPDATE lesson_test_sessions SET state='abandoned', updated_at=now()
WHERE state='in_progress' AND EXISTS (
  SELECT 1 FROM jsonb_array_elements(question_queue) item
  WHERE jsonb_typeof(item) <> 'object' OR NOT (item ? 'source_id' AND item ? 'original_id')
);
WITH duplicates AS (
 SELECT id, row_number() OVER (PARTITION BY learner_id, lesson_stub_id ORDER BY created_at DESC, id DESC) AS rank
 FROM lesson_test_sessions WHERE state='in_progress'
)
UPDATE lesson_test_sessions SET state='abandoned', updated_at=now()
WHERE id IN (SELECT id FROM duplicates WHERE rank>1);
CREATE UNIQUE INDEX lesson_test_sessions_one_in_progress
ON lesson_test_sessions (learner_id, lesson_stub_id) WHERE state='in_progress';

-- +goose Down
DROP INDEX lesson_test_sessions_one_in_progress;
