//go:build integration

package courses_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
)

// Soft delete and the active-course reset are one transaction: when the reset fails the course
// must stay visible, never hidden while still the learner's active course.
func TestCourseDeleteResetsActiveCourseAtomicallyIntegration(t *testing.T) {
	ctx := t.Context()
	pool, learner := objectPurgeDB(t)
	course, _, _ := seedGeneratedLesson(t, pool, learner, 0)
	for _, q := range []string{
		`UPDATE courses SET deleted_at=NULL`,
		`INSERT INTO learner_profiles(learner_id) VALUES('` + learner + `')`,
		`UPDATE learner_profiles SET active_course_id='` + course + `'`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	svc := courses.New(courses.Options{Pool: pool})
	del := func() int {
		req := httptest.NewRequestWithContext(ctx, http.MethodDelete, "/v1/courses/"+course, nil)
		req.SetPathValue("id", course)
		req = req.WithContext(accounts.WithLearnerID(req.Context(), learner))
		rec := httptest.NewRecorder()
		svc.ServeCourseDeleteHTTP(rec, req)
		return rec.Code
	}
	state := func() (deleted bool, active string) {
		if err := pool.QueryRow(ctx, `SELECT c.deleted_at IS NOT NULL, COALESCE(p.active_course_id::text,'') FROM courses c, learner_profiles p WHERE c.id=$1::uuid`, course).Scan(&deleted, &active); err != nil {
			t.Fatal(err)
		}
		return
	}
	for _, q := range []string{
		`CREATE FUNCTION fail_profile_update() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected'; END $$`,
		`CREATE TRIGGER fail_profile_update BEFORE UPDATE ON learner_profiles FOR EACH ROW EXECUTE FUNCTION fail_profile_update()`,
	} {
		if _, err := pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if code := del(); code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 when the active-course reset fails", code)
	}
	if deleted, active := state(); deleted || active != course {
		t.Fatalf("failed reset left deleted=%v active=%q; the soft delete must roll back", deleted, active)
	}
	if _, err := pool.Exec(ctx, `DROP TRIGGER fail_profile_update ON learner_profiles`); err != nil {
		t.Fatal(err)
	}
	if code := del(); code != http.StatusOK {
		t.Fatalf("status %d, want 200", code)
	}
	if deleted, active := state(); !deleted || active != "" {
		t.Fatalf("after delete deleted=%v active=%q, want deleted and no active course", deleted, active)
	}
}
