//go:build integration

package courses_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
)

// A failed active-course read must not look like "no active course".
func TestCoursesListFailsWhenActiveCourseLookupFailsIntegration(t *testing.T) {
	ctx := t.Context()
	pool, learner := objectPurgeDB(t)
	svc := courses.New(courses.Options{Pool: pool})
	list := func() int {
		req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/courses", nil)
		req = req.WithContext(accounts.WithLearnerID(req.Context(), learner))
		rec := httptest.NewRecorder()
		svc.ServeCoursesListHTTP(rec, req)
		return rec.Code
	}
	if code := list(); code != http.StatusOK {
		t.Fatalf("no profile row is a normal empty state: status %d", code)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE learner_profiles RENAME TO learner_profiles_gone`); err != nil {
		t.Fatal(err)
	}
	if code := list(); code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 when the active-course read fails", code)
	}
}
