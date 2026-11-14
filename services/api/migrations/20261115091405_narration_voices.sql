-- +goose Up
-- E14-06: the Learner's chosen narration voice and one retained narration row per voice.
-- narration_voice is a logical voice key from the approved voice config (never a vendor voice id).
ALTER TABLE learner_profiles
 ADD COLUMN narration_voice text NOT NULL DEFAULT 'default' CHECK (narration_voice ~ '^[a-z0-9-]{1,40}$');
-- A voice change adds a row for the new voice and keeps the old row, so replaying the old
-- narration stays free and its asset keeps a reference (and so its retention) until deletion.
ALTER TABLE slide_narrations DROP CONSTRAINT slide_narrations_pkey;
ALTER TABLE slide_narrations ADD CONSTRAINT slide_narrations_voice_key_format CHECK (voice_key ~ '^[a-z0-9-]{1,40}$');
ALTER TABLE slide_narrations ADD PRIMARY KEY (lesson_id, slide_id, voice_key);

-- +goose Down
DELETE FROM slide_narrations a USING slide_narrations b
 WHERE a.lesson_id = b.lesson_id AND a.slide_id = b.slide_id AND a.voice_key <> 'default' AND b.voice_key = 'default';
DELETE FROM slide_narrations WHERE voice_key <> 'default';
ALTER TABLE slide_narrations DROP CONSTRAINT slide_narrations_pkey;
ALTER TABLE slide_narrations DROP CONSTRAINT slide_narrations_voice_key_format;
ALTER TABLE slide_narrations ADD PRIMARY KEY (lesson_id, slide_id);
ALTER TABLE learner_profiles DROP COLUMN narration_voice;
