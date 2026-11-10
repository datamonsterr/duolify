// Package accountstest provides explicit disposable privacy boundaries for tests.
package accountstest

import (
	"context"
	"io"
	"sync"

	"github.com/datamonsterr/duolify/services/api/internal/backup"
)

type objects struct {
	mu      sync.Mutex
	records map[string][]byte
}

func (o *objects) Exists(_ context.Context, key string) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.records[key]
	return ok, nil
}
func (o *objects) Put(_ context.Context, key string, body io.ReadSeeker, _ int64) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.records[key]; ok {
		return nil
	}
	b, err := io.ReadAll(body)
	if err == nil {
		o.records[key] = b
	}
	return err
}

// NewDeletionLedger models independent, immutable object storage, never SQL.
func NewDeletionLedger() backup.DeletionLedger {
	return backup.DeletionLedger{Objects: &objects{records: map[string][]byte{}}}
}
