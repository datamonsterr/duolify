//go:build integration

package backup_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
)

// E17-08: Speaker order, the actual voice/config and the alignment reference of a pinned-speaker
// segment come back from the encrypted dump; restoring never synthesizes.
func TestRestoreKeepsPinnedSpeakerSegmentVoices(t *testing.T) {
	fixture := backuptest.New(t)
	fixture.Seed(t)
	fixture.SeedSegmentNarration(t)
	fixture.RunJob(t, "2026-11-10")
	fixture.AssertRestored(t, fixture.Restore(t, fixture.Ciphertext(t, fixture.ArchiveKey("2026-11-10"))))
}
