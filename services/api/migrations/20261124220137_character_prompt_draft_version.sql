-- +goose Up
ALTER TABLE character_create_jobs ADD COLUMN draft_schema_version smallint NOT NULL DEFAULT 0
    CHECK (draft_schema_version IN (0, 2));

-- +goose Down
ALTER TABLE character_create_jobs DROP COLUMN draft_schema_version;
