//go:build integration

package courses_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type fakeObjects struct {
	keys       []string
	deleted    []string
	failDelete bool
}

func (f *fakeObjects) List(_ context.Context, prefix string) ([]string, error) {
	var out []string
	for _, k := range f.keys {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	return out, nil
}

func (f *fakeObjects) Delete(_ context.Context, key string) error {
	if f.failDelete {
		return errors.New("storage down")
	}
	f.deleted = append(f.deleted, key)
	return nil
}

func objectPurgeDB(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("objpurge"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies())
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
	t.Cleanup(db.Close)
	var learner string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('objpurge') RETURNING id::text`).Scan(&learner); err != nil {
		t.Fatal(err)
	}
	return db.Pool, learner
}

// seedGeneratedLesson creates a course soft-deleted deletedAgo ago with one generated lesson whose
// manifest lives under a generated prefix. It returns the course id, the lesson stub id and the prefix.
func seedGeneratedLesson(t *testing.T, pool *pgxpool.Pool, learner string, deletedAgo time.Duration) (course, stub, prefix string) {
	t.Helper()
	ctx := t.Context()
	var quote, creation, unit, generated string
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid,'seed-'||gen_random_uuid(),(SELECT max(version) FROM pricing_versions),'{}',now(),now()+interval '10 minutes') RETURNING id::text`, learner).Scan(&quote))
	must(pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learner, quote).Scan(&creation))
	must(pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title, deleted_at) VALUES($1::uuid,$2::uuid,'c', now()-make_interval(secs => $3)) RETURNING id::text`, learner, creation, deletedAgo.Seconds()).Scan(&course))
	must(pool.QueryRow(ctx, `INSERT INTO course_units(course_id, learner_id, authored_id, position, title) VALUES($1::uuid,$2::uuid,'u',0,'u') RETURNING id::text`, course, learner).Scan(&unit))
	must(pool.QueryRow(ctx, `INSERT INTO course_lesson_stubs(unit_id, learner_id, authored_id, position, title, goal, status) VALUES($1::uuid,$2::uuid,'l',0,'l','g','locked') RETURNING id::text`, unit, learner).Scan(&stub))
	must(pool.QueryRow(ctx, `INSERT INTO generated_lessons(lesson_stub_id, course_id, learner_id, title, manifest) VALUES($1::uuid,$2::uuid,$3::uuid,'l','{}') RETURNING id::text`, stub, course, learner).Scan(&generated))
	prefix = "lessons/generated/" + generated + "/"
	_, err := pool.Exec(ctx, `INSERT INTO lesson_manifests(lesson_id, learner_id, version, manifest_key) VALUES($1::uuid,$2::uuid,1,$3)`, stub, learner, prefix+"manifest.json")
	must(err)
	return course, stub, prefix
}

func TestPurgedCourseGeneratedLessonObjectsAreDeletedAfterRetentionIntegration(t *testing.T) {
	ctx := t.Context()
	pool, learner := objectPurgeDB(t)
	_, _, oldPrefix := seedGeneratedLesson(t, pool, learner, 31*24*time.Hour)
	_, _, recentPrefix := seedGeneratedLesson(t, pool, learner, 29*24*time.Hour)
	published := "lessons/published/11111111-1111-4111-8111-111111111111/manifest.json"
	objects := &fakeObjects{keys: []string{
		oldPrefix + "manifest.json", oldPrefix + "slide-0.html",
		recentPrefix + "manifest.json", recentPrefix + "slide-0.html", published,
	}}
	svc := courses.New(courses.Options{Pool: pool, Objects: objects})

	now := time.Now().UTC()
	if _, err := svc.PurgeDeletedCourses(ctx, now.Add(-30*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Inside the retention window nothing is deleted from storage.
	if n, err := svc.SweepPurgedObjects(ctx, now); err != nil || n != 0 || len(objects.deleted) != 0 {
		t.Fatalf("objects deleted before retention: n=%d err=%v deleted=%v", n, err, objects.deleted)
	}
	later := now.Add(courses.ObjectPurgeRetention + time.Hour)
	// A failing store surfaces the error and leaves the ledger row pending so the next run retries.
	objects.failDelete = true
	if _, err := svc.SweepPurgedObjects(ctx, later); err == nil {
		t.Fatal("storage failure must surface")
	}
	objects.failDelete = false
	n, err := svc.SweepPurgedObjects(ctx, later)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	slices.Sort(objects.deleted)
	if want := []string{oldPrefix + "manifest.json", oldPrefix + "slide-0.html"}; !slices.Equal(objects.deleted, want) {
		t.Fatalf("deleted %v, want exactly %v (the recent course and published snapshots must stay)", objects.deleted, want)
	}
	if n, err = svc.SweepPurgedObjects(ctx, later); err != nil || n != 0 {
		t.Fatalf("second sweep must be a no-op: n=%d err=%v", n, err)
	}
}

func TestObjectPurgeKeepsPrefixStillReferencedByALiveManifestIntegration(t *testing.T) {
	ctx := t.Context()
	pool, learner := objectPurgeDB(t)
	// A live (not deleted) course still points at this prefix, so its objects must stay.
	_, _, prefix := seedGeneratedLesson(t, pool, learner, 0)
	if _, err := pool.Exec(ctx, `UPDATE courses SET deleted_at=NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO course_object_purges(prefix, purge_after) VALUES($1, now()-interval '1 hour')`, prefix); err != nil {
		t.Fatal(err)
	}
	objects := &fakeObjects{keys: []string{prefix + "manifest.json"}}
	svc := courses.New(courses.Options{Pool: pool, Objects: objects})
	if n, err := svc.SweepPurgedObjects(ctx, time.Now()); err != nil || n != 0 || len(objects.deleted) != 0 {
		t.Fatalf("referenced prefix was deleted: n=%d err=%v deleted=%v", n, err, objects.deleted)
	}
}
