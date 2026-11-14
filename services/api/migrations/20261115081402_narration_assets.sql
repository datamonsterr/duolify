-- +goose Up
-- Deduplicated narration objects (E14-02). One row per content hash: identical normalized text,
-- voice, model and settings fingerprint. Rows hold hashes, keys and counts only, never text,
-- and no learner id: learners reach an asset through slide_narrations.
CREATE TABLE narration_assets (
 content_hash text PRIMARY KEY CHECK(content_hash ~ '^[a-f0-9]{64}$'),
 -- sha256 of the normalized text; links retained settings versions of the same narration.
 text_hash text NOT NULL CHECK(text_hash ~ '^[a-f0-9]{64}$'),
 voice_id text NOT NULL,
 model_id text NOT NULL,
 settings_fingerprint text NOT NULL,
 audio_key text NOT NULL CHECK(audio_key LIKE 'lessons/narration/%'),
 alignment_key text NOT NULL CHECK(alignment_key LIKE 'lessons/narration/%'),
 characters integer NOT NULL CHECK(characters >= 0),
 audio_bytes integer NOT NULL CHECK(audio_bytes >= 0),
 duration_ms integer NOT NULL CHECK(duration_ms >= 0),
 -- ready: objects were stored; missing: an object is gone and recovery is tracked.
 state text NOT NULL DEFAULT 'ready' CHECK(state IN ('ready','missing')),
 -- Maintained by trigger from ready slide_narrations rows; never written by the application.
 ref_count integer NOT NULL DEFAULT 0 CHECK(ref_count >= 0),
 -- Set when the last reference goes away; the object is deleted only after retention elapses.
 unreferenced_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX narration_assets_text ON narration_assets(text_hash);
CREATE INDEX narration_assets_unreferenced ON narration_assets(unreferenced_at) WHERE ref_count = 0;

-- Operator-visible record of audio that had to be generated again without charging the learner.
CREATE TABLE narration_regenerations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 content_hash text NOT NULL REFERENCES narration_assets(content_hash) ON DELETE CASCADE,
 status text NOT NULL DEFAULT 'requested' CHECK(status IN ('requested','completed','failed')),
 failure_code text NOT NULL DEFAULT '' CHECK(char_length(failure_code) <= 64),
 characters integer NOT NULL DEFAULT 0 CHECK(characters >= 0),
 requested_at timestamptz NOT NULL DEFAULT now(),
 completed_at timestamptz
);
CREATE UNIQUE INDEX narration_regenerations_open ON narration_regenerations(content_hash) WHERE status = 'requested';

INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms)
SELECT DISTINCT ON (content_hash) content_hash,
 encode(sha256(convert_to(regexp_replace(btrim(narration_text),'\s+',' ','g'),'UTF8')),'hex'),
 voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms
FROM slide_narrations WHERE status = 'ready' ORDER BY content_hash, updated_at;
UPDATE narration_assets a SET ref_count = (SELECT count(*) FROM slide_narrations n WHERE n.content_hash = a.content_hash AND n.status = 'ready');

-- +goose StatementBegin
CREATE FUNCTION narration_track_refs() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP IN ('UPDATE','DELETE') AND OLD.status = 'ready'
  AND (TG_OP = 'DELETE' OR NEW.status <> 'ready' OR NEW.content_hash <> OLD.content_hash) THEN
  UPDATE narration_assets SET ref_count = greatest(ref_count - 1, 0),
   unreferenced_at = CASE WHEN ref_count <= 1 THEN now() ELSE NULL END, updated_at = now()
  WHERE content_hash = OLD.content_hash;
 END IF;
 IF TG_OP IN ('INSERT','UPDATE') AND NEW.status = 'ready'
  AND (TG_OP = 'INSERT' OR OLD.status <> 'ready' OR OLD.content_hash <> NEW.content_hash) THEN
  UPDATE narration_assets SET ref_count = ref_count + 1, unreferenced_at = NULL, updated_at = now()
  WHERE content_hash = NEW.content_hash;
 END IF;
 RETURN NULL;
END $$;
-- +goose StatementEnd
CREATE TRIGGER slide_narrations_refs AFTER INSERT OR UPDATE OR DELETE ON slide_narrations
 FOR EACH ROW EXECUTE FUNCTION narration_track_refs();

-- +goose Down
DROP TRIGGER slide_narrations_refs ON slide_narrations;
DROP FUNCTION narration_track_refs();
DROP TABLE narration_regenerations;
DROP TABLE narration_assets;
