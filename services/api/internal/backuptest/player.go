package backuptest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/jackc/pgx/v5/pgxpool"
)

type protectedObject struct {
	Key     string `json:"key"`
	Version string `json:"version_id"`
	SHA256  string `json:"sha256"`
}

const protectedBucket = "protected-player-assets"
const protectedManifest = "fixture-snapshot.json"

func authoredObjects() map[string][]byte {
	objects := playerfixture.Documents()
	for key, raw := range courses.SharedDemoDocuments() {
		objects[key] = raw
	}
	return objects
}

// AssertPlaybackPositionRestored compares the complete learner cursor, including
// its UTC timestamp, from the encrypted dump rather than reconstructing it.
func (f *Fixture) AssertPlaybackPositionRestored(t *testing.T, restored *pgxpool.Pool) {
	t.Helper()
	const query = "SELECT coalesce(jsonb_agg(to_jsonb(p)), '[]'::jsonb)::text FROM lesson_playback_positions p"
	var before, after string
	if err := f.DB.Pool.QueryRow(t.Context(), query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRow(t.Context(), query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || !bytes.Contains([]byte(after), []byte(`"time_ms": 1800`)) {
		t.Fatal("cursor restore mismatch", before, after)
	}
	const syncQuery = "SELECT coalesce(jsonb_agg(to_jsonb(s)), '[]'::jsonb)::text FROM lesson_playback_sync_events s"
	var syncBefore, syncAfter string
	if err := f.DB.Pool.QueryRow(t.Context(), syncQuery).Scan(&syncBefore); err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRow(t.Context(), syncQuery).Scan(&syncAfter); err != nil {
		t.Fatal(err)
	}
	if syncBefore != syncAfter || !bytes.Contains([]byte(syncAfter), []byte(`act-11111111`)) {
		t.Fatal("sync event restore mismatch", syncBefore, syncAfter)
	}
}

// AssertNarrationRestored compares narration text and status rows from the encrypted dump.
func (f *Fixture) AssertNarrationRestored(t *testing.T, restored *pgxpool.Pool) {
	t.Helper()
	const query = "SELECT coalesce(jsonb_agg(to_jsonb(n) ORDER BY n.slide_id), '[]'::jsonb)::text FROM slide_narrations n"
	var before, after string
	if err := f.DB.Pool.QueryRow(t.Context(), query).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRow(t.Context(), query).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after || !bytes.Contains([]byte(after), []byte(`"narration_text": "Seeded narration text."`)) || !bytes.Contains([]byte(after), []byte(`"status": "ready"`)) {
		t.Fatal("narration text/status restore mismatch", before, after)
	}
	// E14-02: content hashes, object keys, reference counts and regeneration tracking survive restore.
	const assets = `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.content_hash), '[]'::jsonb)::text FROM narration_assets a`
	var assetsBefore, assetsAfter string
	if err := f.DB.Pool.QueryRow(t.Context(), assets).Scan(&assetsBefore); err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRow(t.Context(), assets).Scan(&assetsAfter); err != nil {
		t.Fatal(err)
	}
	if assetsBefore != assetsAfter || !bytes.Contains([]byte(assetsAfter), []byte(`"ref_count": 1`)) || !bytes.Contains([]byte(assetsAfter), []byte("lessons/narration/v1/")) {
		t.Fatal("narration asset hash/reference restore mismatch", assetsBefore, assetsAfter)
	}
	// E14-06: the voice preference and both voices' manifest references (old asset retained) survive restore.
	const voices = `SELECT (SELECT narration_voice FROM learner_profiles WHERE learner_id='a1111111-1111-4111-8111-111111111111') || ':' ||
		(SELECT string_agg(voice_key||'='||content_hash, ',' ORDER BY voice_key) FROM slide_narrations WHERE slide_id='seed-ready') || ':' ||
		(SELECT string_agg(ref_count::text, ',' ORDER BY content_hash) FROM narration_assets WHERE content_hash IN (repeat('ab',32),repeat('ef',32)))`
	var voicesBefore, voicesAfter string
	if err := f.DB.Pool.QueryRow(t.Context(), voices).Scan(&voicesBefore); err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRow(t.Context(), voices).Scan(&voicesAfter); err != nil {
		t.Fatal(err)
	}
	if voicesBefore != voicesAfter || !bytes.HasPrefix([]byte(voicesAfter), []byte("fixture-warm:default="+"abababab")) || !bytes.HasSuffix([]byte(voicesAfter), []byte(":1,1")) {
		t.Fatal("voice preference or retained voice references restore mismatch", voicesBefore, voicesAfter)
	}
	// E17-08: speaker order, the actual voice/config and the alignment reference restore without any synthesis.
	const segment = `SELECT speaker_key||':'||segment_slide_id||':'||segment_index||':'||voice_key||'>'||actual_voice_key||'@'||voice_config_version||'/'||voice_fallback_reason||':'||alignment_key
		FROM slide_narrations WHERE slide_id='seed-seg__s1'`
	var segmentBefore, segmentAfter string
	if err := f.DB.Pool.QueryRow(t.Context(), segment).Scan(&segmentBefore); err != nil {
		t.Fatal(err)
	}
	if err := restored.QueryRow(t.Context(), segment).Scan(&segmentAfter); err != nil {
		t.Fatal(err)
	}
	if segmentBefore != segmentAfter || !bytes.HasPrefix([]byte(segmentAfter), []byte("guide:seed-seg:1:voice-provisional-guide>default@2026-11-13.1/voice_retired:lessons/narration/v1/")) {
		t.Fatal("segment voice, config or alignment reference restore mismatch", segmentBefore, segmentAfter)
	}
	// The restored trigger keeps counting: deleting the only reference starts retention.
	var unreferenced bool
	tx, err := restored.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()
	if _, err = tx.Exec(t.Context(), `DELETE FROM slide_narrations WHERE content_hash=repeat('ab',32)`); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(t.Context(), `SELECT ref_count=0 AND unreferenced_at IS NOT NULL FROM narration_assets WHERE content_hash=repeat('ab',32)`).Scan(&unreferenced); err != nil || !unreferenced {
		t.Fatal("restored database must keep reference counting", err)
	}
}

// BackupPlayerObjects exercises the protected-copy/key-version-checksum contract
// without adding application objects to the database archive bucket.
func (f *Fixture) BackupPlayerObjects(t *testing.T) {
	t.Helper()
	snapshot := []protectedObject{}
	for key, raw := range authoredObjects() {
		if err := f.ApplicationObjects().Put(t.Context(), key, raw); err != nil {
			t.Fatal(err)
		}
		out, err := f.root.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String(protectedBucket), Key: aws.String(key), Body: bytes.NewReader(raw)})
		if err != nil || aws.ToString(out.VersionId) == "" {
			t.Fatal("protected version", err)
		}
		sum := sha256.Sum256(raw)
		snapshot = append(snapshot, protectedObject{Key: key, Version: aws.ToString(out.VersionId), SHA256: hex.EncodeToString(sum[:])})
	}
	raw, _ := json.Marshal(snapshot)
	if _, err := f.root.Client.PutObject(t.Context(), &s3.PutObjectInput{Bucket: aws.String(protectedBucket), Key: aws.String(protectedManifest), Body: bytes.NewReader(raw)}); err != nil {
		t.Fatal(err)
	}
}

// AssertPlayerObjectsRestored loses current application objects and restores only
// the protected versions, checking each checksum. No LLM/TTS or Git read supplies
// recovery bytes; Git fixture bytes are used solely as comparison assertions.
func (f *Fixture) AssertPlayerObjectsRestored(t *testing.T) {
	t.Helper()
	manifest, err := f.root.Client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(protectedBucket), Key: aws.String(protectedManifest)})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot []protectedObject
	err = json.NewDecoder(manifest.Body).Decode(&snapshot)
	_ = manifest.Body.Close()
	if err != nil || len(snapshot) != len(authoredObjects()) {
		t.Fatal("protected manifest", err)
	}
	for _, entry := range snapshot {
		if _, err := f.root.Client.DeleteObject(t.Context(), &s3.DeleteObjectInput{Bucket: aws.String("application-assets"), Key: aws.String(entry.Key)}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.ApplicationObjects().Get(t.Context(), entry.Key); err == nil {
			t.Fatal("object loss did not occur")
		}
		object, err := f.root.Client.GetObject(t.Context(), &s3.GetObjectInput{Bucket: aws.String(protectedBucket), Key: aws.String(entry.Key), VersionId: aws.String(entry.Version)})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(object.Body)
		_ = object.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != entry.SHA256 {
			t.Fatal("protected checksum mismatch")
		}
		if err := f.ApplicationObjects().Put(t.Context(), entry.Key, raw); err != nil {
			t.Fatal(err)
		}
		restored, err := f.ApplicationObjects().Get(t.Context(), entry.Key)
		if err != nil || !bytes.Equal(restored, authoredObjects()[entry.Key]) {
			t.Fatal("asset restore mismatch", err)
		}
	}
}
