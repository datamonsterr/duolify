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
