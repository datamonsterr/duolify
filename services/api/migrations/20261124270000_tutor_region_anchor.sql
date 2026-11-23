-- +goose Up
-- A tutor question about a selected part of a Slide stores where it was asked: the Lesson and Slide,
-- the playback time, the stage-normalised rectangle and the ids of the declared regions the answer
-- was grounded on. Every column is null for ordinary questions. Ids and numbers only; no Slide text.
ALTER TABLE tutor_messages
  ADD COLUMN anchor_lesson_id uuid,
  ADD COLUMN anchor_slide_index integer,
  ADD COLUMN anchor_slide_id text,
  ADD COLUMN anchor_at_ms integer,
  ADD COLUMN anchor_x double precision,
  ADD COLUMN anchor_y double precision,
  ADD COLUMN anchor_w double precision,
  ADD COLUMN anchor_h double precision,
  ADD COLUMN anchor_region_ids text[] NOT NULL DEFAULT '{}',
  ADD COLUMN anchor_grounding text;

-- Constraints are added NOT VALID (no scan under the ACCESS EXCLUSIVE lock) and validated afterwards,
-- which only takes SHARE UPDATE EXCLUSIVE. Names are the ones Postgres generated for the inline form.
ALTER TABLE tutor_messages
  ADD CONSTRAINT tutor_messages_anchor_lesson_id_fkey FOREIGN KEY (anchor_lesson_id) REFERENCES course_lesson_stubs(id) ON DELETE SET NULL NOT VALID,
  ADD CONSTRAINT tutor_messages_anchor_slide_index_check CHECK (anchor_slide_index >= 0) NOT VALID,
  ADD CONSTRAINT tutor_messages_anchor_slide_id_check CHECK (anchor_slide_id IS NULL OR length(anchor_slide_id) BETWEEN 1 AND 100) NOT VALID,
  ADD CONSTRAINT tutor_messages_anchor_at_ms_check CHECK (anchor_at_ms >= 0) NOT VALID,
  ADD CONSTRAINT tutor_messages_anchor_grounding_check CHECK (anchor_grounding IN ('regions','whole_slide')) NOT VALID,
  ADD CONSTRAINT tutor_messages_anchor_rect CHECK (
    (anchor_grounding IS NULL AND anchor_slide_index IS NULL AND anchor_at_ms IS NULL AND anchor_x IS NULL AND anchor_y IS NULL AND anchor_w IS NULL AND anchor_h IS NULL AND cardinality(anchor_region_ids) = 0)
    OR (anchor_grounding IS NOT NULL AND anchor_slide_index IS NOT NULL AND anchor_at_ms IS NOT NULL
        AND anchor_x >= 0 AND anchor_y >= 0 AND anchor_w > 0 AND anchor_h > 0 AND anchor_x + anchor_w <= 1.000000001 AND anchor_y + anchor_h <= 1.000000001
        AND cardinality(anchor_region_ids) <= 6)
  ) NOT VALID;

ALTER TABLE tutor_messages VALIDATE CONSTRAINT tutor_messages_anchor_lesson_id_fkey;
ALTER TABLE tutor_messages VALIDATE CONSTRAINT tutor_messages_anchor_slide_index_check;
ALTER TABLE tutor_messages VALIDATE CONSTRAINT tutor_messages_anchor_slide_id_check;
ALTER TABLE tutor_messages VALIDATE CONSTRAINT tutor_messages_anchor_at_ms_check;
ALTER TABLE tutor_messages VALIDATE CONSTRAINT tutor_messages_anchor_grounding_check;
ALTER TABLE tutor_messages VALIDATE CONSTRAINT tutor_messages_anchor_rect;

-- +goose Down
ALTER TABLE tutor_messages
  DROP CONSTRAINT tutor_messages_anchor_rect,
  DROP COLUMN anchor_lesson_id, DROP COLUMN anchor_slide_index, DROP COLUMN anchor_slide_id, DROP COLUMN anchor_at_ms,
  DROP COLUMN anchor_x, DROP COLUMN anchor_y, DROP COLUMN anchor_w, DROP COLUMN anchor_h,
  DROP COLUMN anchor_region_ids, DROP COLUMN anchor_grounding;
