package backup

import (
	"bytes"
	"context"
	"errors"
	"regexp"
)

var deletionReference = regexp.MustCompile(`^[0-9a-f]{64}$`)

// DeletionLedger stores immutable hashed references outside database snapshots.
// Each record is a conditional, atomic Put to a deterministic key. A retry or
// concurrent writer preserves the first successful write. There is no rewrite,
// delete, expiry or database restore operation on this prefix. A successful Put
// MUST be durable before committing account erasure; an ambiguous failure is
// retried with the same key. A committed intent is replayed even if SQL failed.
type DeletionLedger struct{ Objects Objects }

// Append writes only the SHA-256 learner reference, never identity or timestamps.
func (l DeletionLedger) Append(ctx context.Context, reference string) error {
	if l.Objects == nil || !deletionReference.MatchString(reference) {
		return errors.New("deletion ledger unavailable or invalid reference")
	}
	body := []byte(`{"learner_reference":"` + reference + `"}`)
	return l.Objects.Put(ctx, "account-deletions/v1/"+reference+".json", bytes.NewReader(body), int64(len(body)))
}

// Contains uses the immutable object key as the record index. Replay enumerates
// restored learners and checks each hash without listing or reading records,
// so no independently mutable ledger index can go stale.
func (l DeletionLedger) Contains(ctx context.Context, reference string) (bool, error) {
	if l.Objects == nil || !deletionReference.MatchString(reference) {
		return false, errors.New("deletion ledger unavailable or invalid reference")
	}
	return l.Objects.Exists(ctx, "account-deletions/v1/"+reference+".json")
}

// ReadOnlyLedger is the drill's view of the deletion ledger. The restorer
// identity cannot write, and a drill must never mutate backup storage: legacy
// SQL tombstones found in a restored copy are applied locally and not appended.
type ReadOnlyLedger struct {
	Objects interface {
		Exists(context.Context, string) (bool, error)
	}
}

// Append is intentionally a no-op.
func (ReadOnlyLedger) Append(context.Context, string) error { return nil }

// Contains checks the immutable per-learner object key.
func (l ReadOnlyLedger) Contains(ctx context.Context, reference string) (bool, error) {
	if l.Objects == nil || !deletionReference.MatchString(reference) {
		return false, errors.New("deletion ledger unavailable or invalid reference")
	}
	return l.Objects.Exists(ctx, "account-deletions/v1/"+reference+".json")
}
