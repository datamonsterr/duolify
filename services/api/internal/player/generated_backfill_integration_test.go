//go:build integration

package player_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

type revokingObjects struct {
	*player.S3
	revoke func(context.Context) error
}

func (o *revokingObjects) Put(ctx context.Context, key string, raw []byte) error {
	if o.revoke != nil {
		revoke := o.revoke
		o.revoke = nil
		if err := revoke(ctx); err != nil {
			return err
		}
	}
	return o.S3.Put(ctx, key, raw)
}

func TestLegacyMaterializationRechecksAccessAfterWritesIntegration(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	const owner = "a1111111-1111-4111-8111-111111111111"
	const lesson = "f3333333-3333-4333-8333-333333333333"
	if _, err := f.DB.Pool.Exec(t.Context(), `DELETE FROM lesson_manifests WHERE lesson_id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	objects := &revokingObjects{S3: f.ApplicationObjects(), revoke: func(ctx context.Context) error {
		tx, err := f.DB.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='100ms'`); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs SET status='locked' WHERE id=$1`, lesson); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}}
	service := player.New(player.Options{Pool: f.DB.Pool, Objects: objects, AssetOrigin: playerfixture.AssetOrigin})
	if err := service.MaterializeGenerated(t.Context(), owner, lesson); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("expected authorization refusal after access was revoked, got %v", err)
	}
	var exists bool
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM lesson_manifests WHERE lesson_id=$1)`, lesson).Scan(&exists); err != nil || exists {
		t.Fatalf("revoked lesson has manifest: %v %v", exists, err)
	}
}

func TestWorkerBackfillsLegacyGeneratedLessonsIntegration(t *testing.T) {
	f := backuptest.New(t)
	f.Seed(t)
	const lesson = "f3333333-3333-4333-8333-333333333333"
	if _, err := f.DB.Pool.Exec(t.Context(), `DELETE FROM lesson_manifests WHERE lesson_id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	service := player.New(player.Options{Pool: f.DB.Pool, Objects: f.ApplicationObjects(), AssetOrigin: playerfixture.AssetOrigin})
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE course_lesson_stubs SET status='locked' WHERE id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	if err := service.BackfillGenerated(t.Context()); err != nil {
		t.Fatal(err)
	}
	var hidden bool
	if err := f.DB.Pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM lesson_manifests WHERE lesson_id=$1)`, lesson).Scan(&hidden); err != nil || hidden {
		t.Fatalf("backfill published locked lesson: %v %v", hidden, err)
	}
	if _, err := f.DB.Pool.Exec(t.Context(), `UPDATE course_lesson_stubs SET status='current' WHERE id=$1`, lesson); err != nil {
		t.Fatal(err)
	}
	queue, err := service.GeneratedBackfillQueue(f.Logger)
	if err != nil {
		t.Fatal(err)
	}
	if err = queue.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = queue.Stop(ctx)
	})
	job, err := queue.Insert(t.Context(), player.GeneratedBackfillArgs{}, &river.InsertOpts{Queue: "generated-publication"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var state string
		if err = f.DB.Pool.QueryRow(t.Context(), `SELECT state FROM river_job WHERE id=$1`, job.Job.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("backfill did not complete: %s", state)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var key string
	if err = f.DB.Pool.QueryRow(t.Context(), `SELECT manifest_key FROM lesson_manifests WHERE lesson_id=$1`, lesson).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err = f.ApplicationObjects().Get(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if err = service.BackfillGenerated(t.Context()); err != nil {
		t.Fatal(err)
	}
	versions, err := f.AssetOperator().ListVersions(t.Context(), key)
	if err != nil || len(versions) != 1 {
		t.Fatalf("backfill rewrote immutable manifest: %d %v", len(versions), err)
	}
}
