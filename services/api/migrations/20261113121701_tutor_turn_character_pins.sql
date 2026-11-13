-- +goose Up
-- E17-07: each tutor turn pins the effective Character reference before streaming. Ids and versions only:
-- no persona, name or message text. No foreign key: built-in Characters have no row, and a pin must not keep
-- a private Character alive. Thread deletion, tombstoning and account deletion clear these columns.
ALTER TABLE tutor_turns
 ADD COLUMN speaker_key text CHECK(speaker_key IS NULL OR speaker_key IN ('guide','supporter','supporter_2')),
 ADD COLUMN character_id text CHECK(character_id IS NULL OR length(character_id) BETWEEN 1 AND 64),
 ADD COLUMN character_version integer CHECK(character_version IS NULL OR character_version>0),
 ADD COLUMN cast_source text CHECK(cast_source IS NULL OR cast_source IN ('lesson','course','default')),
 ADD COLUMN cast_version integer CHECK(cast_version IS NULL OR cast_version>=0),
 ADD COLUMN persona_fallback text CHECK(persona_fallback IS NULL OR persona_fallback IN ('invalid_persona','persona_too_large','speaker_unavailable','persona_unavailable','suppressed'));

-- +goose Down
ALTER TABLE tutor_turns DROP COLUMN persona_fallback, DROP COLUMN cast_version, DROP COLUMN cast_source, DROP COLUMN character_version, DROP COLUMN character_id, DROP COLUMN speaker_key;
