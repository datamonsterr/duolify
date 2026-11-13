-- +goose Up
-- Plain-text dialogue segments for a generated Slide; speaker keys resolve only
-- through the job's immutable lesson_cast_snapshots row.
ALTER TABLE lesson_slides ADD COLUMN segments jsonb NOT NULL DEFAULT '[]'::jsonb CHECK(jsonb_typeof(segments)='array' AND jsonb_array_length(segments)<=12);

-- +goose Down
ALTER TABLE lesson_slides DROP COLUMN segments;
