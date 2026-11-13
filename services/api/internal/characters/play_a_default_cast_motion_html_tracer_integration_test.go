//go:build integration

package characters_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

func TestPlayDefaultCastMotionHtmlTracerIntegration(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	learnerID := "a1111111-1111-4111-8111-111111111111"
	courseID := "f1111111-1111-4111-8111-111111111111"
	lessonID := "fixture-lesson"

	svc := characters.NewService(fixture.DB.Pool, fixture.ApplicationStorage(), fixture.Events())

	// 1. Authorized manifest projection without saved Cast
	manifest, err := svc.ProjectManifest(ctx, learnerID, courseID, lessonID)
	if err != nil {
		t.Fatalf("failed to project manifest: %v", err)
	}

	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}

	// Assert pinned versions
	if manifest.Cast.SchemaVersion != 1 {
		t.Errorf("schema_version=%d want=1", manifest.Cast.SchemaVersion)
	}
	if manifest.Cast.RigVersion != "original-v1" {
		t.Errorf("rig_version=%q want=original-v1", manifest.Cast.RigVersion)
	}
	if manifest.Cast.VoiceConfigVersion != "v1" {
		t.Errorf("voice_config_version=%q want=v1", manifest.Cast.VoiceConfigVersion)
	}
	if manifest.Cast.CastSource != "default" {
		t.Errorf("cast_source=%q want=default", manifest.Cast.CastSource)
	}

	// Two stable speaker keys: guide and supporter
	guide, hasGuide := manifest.Cast.Speakers["guide"]
	if !hasGuide {
		t.Fatal("manifest missing guide speaker")
	}
	if guide.Role != "guide" || guide.SpeakerKey != "guide" {
		t.Errorf("invalid guide speaker: %+v", guide)
	}
	if !guide.IsProvisional {
		t.Error("expected guide to be marked provisional")
	}

	supporter, hasSupporter := manifest.Cast.Speakers["supporter"]
	if !hasSupporter {
		t.Fatal("manifest missing supporter speaker")
	}
	if supporter.Role != "supporter" || supporter.SpeakerKey != "supporter" {
		t.Errorf("invalid supporter speaker: %+v", supporter)
	}
	if !supporter.IsProvisional {
		t.Error("expected supporter to be marked provisional")
	}

	// Verify spend is zero: no credit_entries created for character_create
	var createSpendCount int64
	err = fixture.DB.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND reference LIKE '%character_create%'", learnerID).Scan(&createSpendCount)
	if err != nil {
		t.Fatal(err)
	}
	if createSpendCount != 0 {
		t.Errorf("expected 0 character_create credit entries, got %d", createSpendCount)
	}

	// 2. Immutable fixture references: re-projecting returns the exact same snapshot
	manifest2, err := svc.ProjectManifest(ctx, learnerID, courseID, lessonID)
	if err != nil {
		t.Fatalf("re-project failed: %v", err)
	}
	if manifest2.Cast.Speakers["guide"].CharacterID != guide.CharacterID {
		t.Errorf("guide character ID mutated: %s vs %s", manifest2.Cast.Speakers["guide"].CharacterID, guide.CharacterID)
	}

	// 3. Object / Checksum restore from RustFS S3
	testRigData := []byte("<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 160 160'><circle cx='80' cy='80' r='60'/></svg>")
	h := sha256.Sum256(testRigData)
	expectedChecksum := hex.EncodeToString(h[:])

	checksum, err := svc.StoreRigAsset(ctx, "original-v1", testRigData)
	if err != nil {
		t.Fatalf("StoreRigAsset failed: %v", err)
	}
	if checksum != expectedChecksum {
		t.Errorf("checksum=%s want=%s", checksum, expectedChecksum)
	}

	restoredData, err := svc.RestoreRigAsset(ctx, "original-v1", expectedChecksum)
	if err != nil {
		t.Fatalf("RestoreRigAsset failed: %v", err)
	}
	if string(restoredData) != string(testRigData) {
		t.Errorf("restoredData mismatch")
	}

	// Checksum mismatch must fail
	_, err = svc.RestoreRigAsset(ctx, "original-v1", "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected checksum verification failure on corrupted hash")
	}

	// 4. Missing-version fallback
	fallbackData, err := svc.RestoreRigAsset(ctx, "missing-rig-v99", "")
	if err != nil {
		t.Fatalf("RestoreRigAsset should gracefully fall back on missing rig, got err: %v", err)
	}
	if len(fallbackData) == 0 {
		t.Fatal("expected non-empty fallback rig data")
	}
}
