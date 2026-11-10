package backup_test

import (
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

func TestDeletionLedgerOnlyAcceptsHashedReferencesAndPropagatesStorageFailure(t *testing.T) {
	store := new(objects)
	ledger := backup.DeletionLedger{Objects: store}
	for _, invalid := range []string{"", "private@example.test", "11111111-1111-4111-8111-111111111111", "../" + strings.Repeat("a", 64)} {
		if err := ledger.Append(t.Context(), invalid); err == nil {
			t.Fatal("accepted non-hash", invalid)
		}
		if _, err := ledger.Contains(t.Context(), invalid); err == nil {
			t.Fatal("looked up non-hash", invalid)
		}
	}
	ref := strings.Repeat("a", 64)
	store.fail = true
	if err := ledger.Append(t.Context(), ref); err == nil {
		t.Fatal("storage failure accepted")
	}
	store.fail = false
	if err := ledger.Append(t.Context(), ref); err != nil {
		t.Fatal(err)
	}
	if store.key != "account-deletions/v1/"+ref+".json" || string(store.data) != `{"learner_reference":"`+ref+`"}` {
		t.Fatalf("unexpected ledger record: %s %s", store.key, store.data)
	}
	if found, err := ledger.Contains(t.Context(), ref); err != nil || !found {
		t.Fatal("record not durable", err)
	}
	if err := (backup.DeletionLedger{}).Append(t.Context(), ref); err == nil {
		t.Fatal("missing storage accepted")
	}
}
