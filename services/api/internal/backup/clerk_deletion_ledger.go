package backup

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const clerkLedgerPrefix = "account-deletions/clerk-v1/"

type clerkLedgerObjects interface {
	Objects
	List(context.Context, string) ([]Object, error)
	GetCurrent(context.Context, string) ([]byte, error)
}

// Clerk intents are encrypted separately from database snapshots. Keep JWT_SECRET
// in the recovery key escrow: deleting or replacing it loses these erasure intents.
func (l DeletionLedger) clerkCipher() (cipher.AEAD, error) {
	if len(l.ClerkKey) < 32 {
		return nil, errors.New("clerk deletion ledger encryption key required")
	}
	key, err := hkdf.Key(sha256.New, []byte(l.ClerkKey), nil, "duolify/clerk-erasure/v1", 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func clerkReference(id string) string {
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])
}

// AppendClerk durably preserves the recoverable subject before SQL erasure.
func (l DeletionLedger) AppendClerk(ctx context.Context, id string) error {
	if l.Objects == nil || len(id) < 1 || len(id) > 255 {
		return errors.New("invalid Clerk erasure intent")
	}
	aead, err := l.clerkCipher()
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	key := clerkLedgerPrefix + clerkReference(id) + ".bin"
	body := aead.Seal(nonce, nonce, []byte(id), []byte(key))
	return l.Objects.Put(ctx, key, bytes.NewReader(body), int64(len(body)))
}

// ContainsClerk also blocks a previously verified identity racing a completed deletion.
// Clerk subjects are immutable and never reused; a fresh sign-up has a fresh subject.
func (l DeletionLedger) ContainsClerk(ctx context.Context, id string) (bool, error) {
	if l.Objects == nil {
		return false, errors.New("clerk deletion ledger unavailable")
	}
	return l.Objects.Exists(ctx, clerkLedgerPrefix+clerkReference(id)+".bin")
}

// ClerkDeletions enumerates independently of restored learners and bindings. Never
// expire these objects: replay may repeat an already completed, idempotent erasure.
func (l DeletionLedger) ClerkDeletions(ctx context.Context) ([]string, error) {
	objects, ok := l.Objects.(clerkLedgerObjects)
	if !ok {
		return nil, errors.New("recoverable Clerk deletion ledger required")
	}
	records, err := objects.List(ctx, clerkLedgerPrefix)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, nil
	}
	aead, err := l.clerkCipher()
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, record := range records {
		raw, err := objects.GetCurrent(ctx, record.Key)
		if err != nil {
			return nil, err
		}
		if len(raw) < aead.NonceSize() {
			return nil, errors.New("invalid Clerk erasure ciphertext")
		}
		id, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(record.Key))
		if err != nil || len(id) < 1 || len(id) > 255 || record.Key != clerkLedgerPrefix+clerkReference(string(id))+".bin" {
			return nil, errors.New("invalid Clerk erasure intent")
		}
		ids = append(ids, string(id))
	}
	return ids, nil
}
