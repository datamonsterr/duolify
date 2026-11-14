-- +goose Up
-- E17-08: per-speaker dialogue segments reuse slide_narrations. A segment row has its own stable
-- slide_id (<slide>__s<index>), so the content hash, asset ref counts, retention and recovery are the
-- E14 ones. The columns keep speaker order, the actual voice/config and any fallback reason in dumps.
ALTER TABLE slide_narrations
 ADD COLUMN speaker_key text NOT NULL DEFAULT '' CHECK (speaker_key ~ '^[a-z0-9_-]{0,24}$'),
 ADD COLUMN segment_slide_id text NOT NULL DEFAULT '' CHECK (segment_slide_id ~ '^[A-Za-z0-9_-]{0,100}$'),
 ADD COLUMN segment_index integer NOT NULL DEFAULT 0 CHECK (segment_index >= 0 AND segment_index < 12),
 ADD COLUMN actual_voice_key text NOT NULL DEFAULT '' CHECK (actual_voice_key ~ '^[a-z0-9-]{0,40}$'),
 ADD COLUMN voice_config_version text NOT NULL DEFAULT '' CHECK (char_length(voice_config_version) <= 40),
 ADD COLUMN voice_fallback_reason text NOT NULL DEFAULT '' CHECK (voice_fallback_reason IN ('', 'voice_retired', 'voice_unavailable', 'voice_not_entitled')),
 ADD CONSTRAINT slide_narrations_segment_shape CHECK ((speaker_key = '') = (segment_slide_id = ''));
CREATE INDEX slide_narrations_segments ON slide_narrations(lesson_id, segment_slide_id, segment_index) WHERE speaker_key <> '';

-- +goose Down
DROP INDEX slide_narrations_segments;
ALTER TABLE slide_narrations
 DROP CONSTRAINT slide_narrations_segment_shape,
 DROP COLUMN voice_fallback_reason, DROP COLUMN voice_config_version, DROP COLUMN actual_voice_key,
 DROP COLUMN segment_index, DROP COLUMN segment_slide_id, DROP COLUMN speaker_key;
