//go:build integration

package narration_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type deletableObjects struct {
	mu      sync.Mutex
	deleted []string
}

func (*deletableObjects) Get(context.Context, string) ([]byte, error) { return nil, nil }
func (*deletableObjects) Put(context.Context, string, []byte) error   { return nil }
func (o *deletableObjects) Delete(_ context.Context, key string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.deleted = append(o.deleted, key)
	return nil
}
func (o *deletableObjects) keys() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.deleted...)
}

// Audio of erased learners must leave storage even when synthesis is switched off: the purge needs
// only the database and an object store that can delete, never the speech provider. A referenced
// asset stays whatever its age.
func TestOrphanedAudioPurgeRunsWithoutSynthesisIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("orphanaudio"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)
	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}
	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	orphanHash, sharedHash := strings.Repeat("a", 64), strings.Repeat("c", 64)
	for _, row := range []struct {
		hash, audio string
		refs        int
	}{{orphanHash, "lessons/narration/orphan", 0}, {sharedHash, "lessons/narration/shared", 1}} {
		if _, err = db.Pool.Exec(ctx, `INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms,state,ref_count,unreferenced_at)
			VALUES ($1,repeat('b',64),'v','m','f',$2,$2||'.json',1,1,1,'ready',$3,now()-interval '90 days')`, row.hash, row.audio, row.refs); err != nil {
			t.Fatal(err)
		}
	}
	objects := new(deletableObjects)
	svc := narration.New(narration.Options{Pool: db.Pool, Objects: objects, Retention: 30 * 24 * time.Hour})
	go svc.MaintainOrphanedAudio(ctx, time.Hour)
	deadline := time.Now().Add(30 * time.Second)
	for len(objects.keys()) < 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := objects.keys(); len(got) != 2 || got[0] != "lessons/narration/orphan" || got[1] != "lessons/narration/orphan.json" {
		t.Fatalf("deleted objects %v, want the orphan audio and alignment only", got)
	}
	var left int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM narration_assets WHERE content_hash=$1`, sharedHash).Scan(&left); err != nil || left != 1 {
		t.Fatalf("referenced asset must stay: %d %v", left, err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM narration_assets WHERE content_hash=$1`, orphanHash).Scan(&left); err != nil || left != 0 {
		t.Fatalf("orphan row must be deleted with its objects: %d %v", left, err)
	}
}
