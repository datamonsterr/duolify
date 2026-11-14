package playerfixture

import (
	"bytes"
	"context"
	"errors"
	"sync"
)

// MemoryObjects is an isolated immutable store for tests that do not exercise S3.
type MemoryObjects struct {
	mu   sync.Mutex
	docs map[string][]byte
}

// Put follows the immutable S3 writer's idempotency contract.
func (m *MemoryObjects) Put(_ context.Context, key string, raw []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.docs == nil {
		m.docs = map[string][]byte{}
	}
	if old, ok := m.docs[key]; ok && !bytes.Equal(old, raw) {
		return errors.New("immutable object conflict")
	}
	m.docs[key] = bytes.Clone(raw)
	return nil
}

// Get reads fixture content only.
func (m *MemoryObjects) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.docs[key]
	if !ok {
		return nil, errors.New("missing fixture object")
	}
	return bytes.Clone(raw), nil
}
