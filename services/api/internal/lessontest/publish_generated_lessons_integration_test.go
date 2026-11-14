//go:build integration

package lessontest_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
)

func TestSettlementPublishesGeneratedLessonIntegration(t *testing.T) {
	storage := backuptest.New(t)
	f := newReviewFixture(t)
	f.service = lessontest.New(lessontest.Options{Pool: f.db.Pool, Engine: f.engine, Financial: f.financial, Objects: storage.ApplicationObjects(), AssetOrigin: playerfixture.AssetOrigin})
	id := f.prepare(t)
	if err := f.service.RunLessonGeneration(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	var key string
	if err := f.db.Pool.QueryRow(t.Context(), `SELECT manifest_key FROM lesson_manifests WHERE lesson_id=$1`, f.lesson).Scan(&key); err != nil {
		t.Fatalf("generation completed without a player manifest: %v", err)
	}
	raw, err := storage.ApplicationObjects().Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	var manifest player.ObjectManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Slides) != len(f.engine.lesson.Slides) {
		t.Fatal("missing slides")
	}
	for i, slide := range manifest.Slides {
		raw, err = storage.ApplicationObjects().Get(t.Context(), slide.HTMLKey)
		if err != nil || string(raw) != f.engine.lesson.Slides[i].Html {
			t.Fatalf("slide mismatch: %s %v", raw, err)
		}
	}
	// Completed jobs are no-ops, including their paid provider and ledger effects.
	f.engine.err = errors.New("provider must not run")
	if err = f.service.RunLessonGeneration(t.Context(), id); err != nil {
		t.Fatal(err)
	}
}

type failingPublication struct {
	objects *player.S3
	fail    bool
	check   func() error
}

func (w *failingPublication) Put(ctx context.Context, key string, raw []byte) error {
	if w.check != nil {
		if err := w.check(); err != nil {
			return err
		}
	}
	if w.fail && strings.HasSuffix(key, "/manifest.json") {
		return errors.New("manifest storage unavailable")
	}
	return w.objects.Put(ctx, key, raw)
}

func TestPublicationFailuresResumeWithoutRegenerationIntegration(t *testing.T) {
	storage := backuptest.New(t)
	for _, mode := range []string{"object-write", "manifest-transaction"} {
		t.Run(mode, func(t *testing.T) {
			f := newReviewFixture(t)
			objects := &failingPublication{objects: storage.ApplicationObjects(), fail: mode == "object-write"}
			objects.check = func() error {
				var open bool
				if err := f.db.Pool.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND state='idle in transaction')`).Scan(&open); err != nil {
					return err
				}
				if open {
					return errors.New("object write inside a database transaction")
				}
				return nil
			}
			f.service = lessontest.New(lessontest.Options{Pool: f.db.Pool, Engine: f.engine, Financial: f.financial, Objects: objects, AssetOrigin: playerfixture.AssetOrigin})
			if mode == "manifest-transaction" {
				if _, err := f.db.Pool.Exec(t.Context(), `CREATE FUNCTION reject_publication() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'manifest database unavailable'; END $$; CREATE TRIGGER reject_publication BEFORE INSERT ON lesson_manifests FOR EACH ROW EXECUTE FUNCTION reject_publication()`); err != nil {
					t.Fatal(err)
				}
			}
			id := f.prepare(t)
			if err := f.service.RunLessonGeneration(t.Context(), id); err == nil {
				t.Fatal("expected publication failure")
			}
			var state, jobState, status string
			var checkpoint bool
			var manifests, generated, charges int
			if err := f.db.Pool.QueryRow(t.Context(), `SELECT g.state,g.result_checkpoint IS NOT NULL,j.state,l.status,
 (SELECT count(*) FROM lesson_manifests WHERE lesson_id=g.lesson_id),
 (SELECT count(*) FROM generated_lessons WHERE lesson_stub_id=g.lesson_id),
 (SELECT count(*) FROM credit_entries WHERE learner_id=g.learner_id AND kind='spend')
 FROM lesson_generations g JOIN credit_jobs j ON j.id=g.reservation_id JOIN course_lesson_stubs l ON l.id=g.lesson_id WHERE g.id=$1`, id).Scan(&state, &checkpoint, &jobState, &status, &manifests, &generated, &charges); err != nil {
				t.Fatal(err)
			}
			if state != "validated" || !checkpoint || jobState == "settled" || status != "generating" || manifests != 0 || generated != 0 || charges != 0 {
				t.Fatalf("partial settlement: %s %v %s %s manifests=%d generated=%d charges=%d", state, checkpoint, jobState, status, manifests, generated, charges)
			}
			if mode == "manifest-transaction" {
				if _, err := f.db.Pool.Exec(t.Context(), `DROP TRIGGER reject_publication ON lesson_manifests`); err != nil {
					t.Fatal(err)
				}
			}
			objects.fail = false
			f.engine.err = errors.New("provider must not run on retry")
			if err := f.service.RunLessonGeneration(t.Context(), id); err != nil {
				t.Fatalf("resume checkpoint: %v", err)
			}
			var key string
			if err := f.db.Pool.QueryRow(t.Context(), `SELECT manifest_key FROM lesson_manifests WHERE lesson_id=$1`, f.lesson).Scan(&key); err != nil {
				t.Fatal(err)
			}
			if _, err := storage.ApplicationObjects().Get(t.Context(), key); err != nil {
				t.Fatal(err)
			}
			versions, err := storage.AssetOperator().ListVersions(t.Context(), key)
			if err != nil || len(versions) != 1 {
				t.Fatalf("retry created manifest versions: %d %v", len(versions), err)
			}
			keys, err := storage.ApplicationObjects().List(t.Context(), "lessons/generated/"+id+"/")
			if err != nil || len(keys) != len(f.engine.lesson.Slides)+1 {
				t.Fatalf("retry leaked objects: %d %v", len(keys), err)
			}
		})
	}
}
