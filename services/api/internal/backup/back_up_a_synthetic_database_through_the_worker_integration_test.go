//go:build integration

package backup_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
)

func TestPostgresRiverRustFSEncryptedBackupRetry(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.FailNextUpload()
	fixture.RunJob(t, "2026-11-08")
	ciphertext := fixture.Ciphertext(t, fixture.ArchiveKey("2026-11-08"))
	fixture.AssertRestored(t, fixture.Restore(t, ciphertext))
	fixture.AssertObjectCount(t, 1)
	fixture.AssertEvents(t, "backup_failed", "backup_completed")
	fixture.AssertWriterCannotDelete(t)
}

func TestRestorePreDeletionDumpKeepsLearnerDeleted(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.RunJob(t, "2026-11-09")
	fixture.AssertPreDeletionRestoreErased(t, fixture.Ciphertext(t, fixture.ArchiveKey("2026-11-09")))
}
