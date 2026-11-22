//go:build integration

package courses_test

import (
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestCoursePurgeJobRemovesOnlyCoursesPastGraceIntegration(t *testing.T) {
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("purge"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"),
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
	defer db.Close()

	var learnerID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('purge') RETURNING id::text`).Scan(&learnerID); err != nil {
		t.Fatal(err)
	}
	seed := func(title string, age time.Duration) string {
		var creationID, courseID, quoteID string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid,'seed-'||gen_random_uuid(),(SELECT max(version) FROM pricing_versions),'{}',now(),now()+interval '10 minutes') RETURNING id::text`, learnerID).Scan(&quoteID); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, quoteID).Scan(&creationID); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title, deleted_at) VALUES($1::uuid,$2::uuid,$3, now()-make_interval(secs => $4)) RETURNING id::text`, learnerID, creationID, title, age.Seconds()).Scan(&courseID); err != nil {
			t.Fatal(err)
		}
		return courseID
	}
	old := seed("deleted 31 days ago", 31*24*time.Hour)
	recent := seed("deleted 29 days ago", 29*24*time.Hour)

	svc := courses.New(courses.Options{Pool: db.Pool})
	if err = (&worker.CoursePurgeWorker{Service: svc}).Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
	exists := func(id string) bool {
		var ok bool
		if err := db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM courses WHERE id=$1::uuid)`, id).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if exists(old) {
		t.Fatal("course deleted 31 days ago was not purged")
	}
	if !exists(recent) {
		t.Fatal("course deleted 29 days ago must be kept")
	}
}
