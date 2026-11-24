-- +goose Up
-- SHA-256 of the compiled idle avatar SVG, written with every v2 version (NULL for stored v1 versions).
ALTER TABLE character_versions ADD COLUMN IF NOT EXISTS compile_sha256 text CHECK(compile_sha256 ~ '^[0-9a-f]{64}$');
-- +goose Down
ALTER TABLE character_versions DROP COLUMN IF EXISTS compile_sha256;
