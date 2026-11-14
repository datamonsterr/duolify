//go:build !integration

package backup_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

type recordingObjects struct{ puts map[string]string }

func (r *recordingObjects) Exists(_ context.Context, key string) (bool, error) {
	_, ok := r.puts[key]
	return ok, nil
}

func (r *recordingObjects) Put(_ context.Context, key string, body io.ReadSeeker, _ int64) error {
	var b bytes.Buffer
	_, _ = b.ReadFrom(body)
	if r.puts == nil {
		r.puts = map[string]string{}
	}
	if _, ok := r.puts[key]; !ok {
		r.puts[key] = b.String()
	}
	return nil
}

func TestModerationLedgerWritesOnlyHashedReferencesToImmutableKeys(t *testing.T) {
	course, action := strings.Repeat("a", 64), strings.Repeat("b", 64)
	objects := &recordingObjects{}
	ledger := backup.ModerationLedger{Objects: objects}
	for i := 0; i < 2; i++ { // a retry keeps the first record
		if err := ledger.RecordBlock(t.Context(), course, action); err != nil {
			t.Fatal(err)
		}
	}
	if err := ledger.LiftBlock(t.Context(), action); err != nil {
		t.Fatal(err)
	}
	if got := objects.puts[backup.BlockKey(action)]; !strings.Contains(got, course) || !strings.Contains(got, action) {
		t.Fatalf("block record = %q", got)
	}
	if backup.BlockKey(action) == backup.LiftKey(action) || !strings.HasPrefix(backup.BlockKey(action), "moderation-blocks/v1/") || !strings.HasPrefix(backup.LiftKey(action), "moderation-blocks/v1/") {
		t.Fatalf("keys %q %q", backup.BlockKey(action), backup.LiftKey(action))
	}
	if len(objects.puts) != 2 {
		t.Fatalf("records = %v", objects.puts)
	}
}

func TestModerationLedgerRefusesReferencesThatAreNotHashes(t *testing.T) {
	ledger := backup.ModerationLedger{Objects: &recordingObjects{}}
	for _, bad := range []string{"", "not-a-hash", "11111111-2222-4333-8444-555555555555", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if err := ledger.RecordBlock(t.Context(), bad, strings.Repeat("b", 64)); err == nil {
			t.Errorf("accepted course reference %q", bad)
		}
		if err := ledger.RecordBlock(t.Context(), strings.Repeat("a", 64), bad); err == nil {
			t.Errorf("accepted action reference %q", bad)
		}
		if err := ledger.LiftBlock(t.Context(), bad); err == nil {
			t.Errorf("accepted lift reference %q", bad)
		}
	}
	if err := (backup.ModerationLedger{}).RecordBlock(t.Context(), strings.Repeat("a", 64), strings.Repeat("b", 64)); err == nil {
		t.Error("a ledger without storage must fail closed")
	}
}
