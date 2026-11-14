package backuptest

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

// ProtectCharacterAssets uses the real isolated encrypted backup store and versioned app objects.
func (f *Fixture) ProtectCharacterAssets(t *testing.T, date string) {
	t.Helper()
	p := backup.CharacterAssetProtection{Pool: f.DB.Pool, Application: &backup.S3{Client: f.objects.Client, Bucket: f.writerOptions.ApplicationBucket}, Protected: f.Store("writer"), Recipient: f.identity.Recipient().String()}
	if err := p.Protect(t.Context(), date); err != nil {
		t.Fatal(err)
	}
}

// RestoreCharacterAssets recovers only protected bytes; neither a rig compiler nor a provider supplies them.
func (f *Fixture) RestoreCharacterAssets(t *testing.T, date string) {
	t.Helper()
	if err := backup.RestoreCharacterAssets(t.Context(), date, f.Store("restorer"), f.identity, f.ApplicationObjects().Put); err != nil {
		t.Fatal(err)
	}
}
