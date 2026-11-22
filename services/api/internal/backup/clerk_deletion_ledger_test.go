package backup_test

import (
	"bytes"
	"context"
	"errors"
	"github.com/datamonsterr/duolify/services/api/internal/backup"
	"io"
	"strings"
	"testing"
)

type clerkObjects struct {
	records map[string][]byte
	fail    bool
}

func (o *clerkObjects) Exists(_ context.Context, key string) (bool, error) {
	_, ok := o.records[key]
	return ok, nil
}
func (o *clerkObjects) Put(_ context.Context, key string, r io.ReadSeeker, _ int64) error {
	if o.fail {
		return errors.New("unavailable")
	}
	if _, ok := o.records[key]; ok {
		return nil
	}
	raw, err := io.ReadAll(r)
	o.records[key] = raw
	return err
}
func (o *clerkObjects) List(_ context.Context, prefix string) ([]backup.Object, error) {
	var out []backup.Object
	for key := range o.records {
		if strings.HasPrefix(key, prefix) {
			out = append(out, backup.Object{Key: key})
		}
	}
	return out, nil
}
func (o *clerkObjects) GetCurrent(_ context.Context, key string) ([]byte, error) {
	return o.records[key], nil
}

func TestClerkErasureLedgerEncryptedDurableAndAuthenticated(t *testing.T) {
	store := &clerkObjects{records: map[string][]byte{}}
	ledger := backup.DeletionLedger{Objects: store, ClerkKey: strings.Repeat("test-secret", 4)}
	store.fail = true
	if err := ledger.AppendClerk(t.Context(), "user_private"); err == nil {
		t.Fatal("accepted failed durable write")
	}
	store.fail = false
	if err := ledger.AppendClerk(t.Context(), "user_private"); err != nil {
		t.Fatal(err)
	}
	var key string
	var original []byte
	for k, raw := range store.records {
		key = k
		original = append([]byte(nil), raw...)
		if strings.Contains(k, "user_private") || bytes.Contains(raw, []byte("user_private")) {
			t.Fatal("plaintext provider identity leaked")
		}
	}
	if err := ledger.AppendClerk(t.Context(), "user_private"); err != nil || !bytes.Equal(store.records[key], original) {
		t.Fatal("immutable retry failed", err)
	}
	ids, err := ledger.ClerkDeletions(t.Context())
	if err != nil || len(ids) != 1 || ids[0] != "user_private" {
		t.Fatal("cannot recover intent", ids, err)
	}
	if exists, err := ledger.ContainsClerk(t.Context(), "user_private"); err != nil || !exists {
		t.Fatal("missing intent", err)
	}
	wrong := ledger
	wrong.ClerkKey = strings.Repeat("wrong-key", 5)
	if _, err := wrong.ClerkDeletions(t.Context()); err == nil {
		t.Fatal("wrong recovery key accepted")
	}
	store.records[key][len(original)-1] ^= 1
	if _, err := ledger.ClerkDeletions(t.Context()); err == nil {
		t.Fatal("corrupt ledger accepted")
	}
	store.records[key] = []byte("short")
	if _, err := ledger.ClerkDeletions(t.Context()); err == nil {
		t.Fatal("truncated ledger accepted")
	}
	if err := (backup.DeletionLedger{Objects: store}).AppendClerk(t.Context(), "user_private"); err == nil {
		t.Fatal("missing encryption key accepted")
	}
}
