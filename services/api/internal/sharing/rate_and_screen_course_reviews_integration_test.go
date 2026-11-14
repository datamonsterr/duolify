//go:build integration

package sharing_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

func TestRateAndScreenCourseReviews(t *testing.T) {
	f := backuptest.New(t)
	s := marketService(t, f, &capture{}, nil, nil)
	o := marketOwner(t, f, "reviews-owner")
	l := listCourse(t, s, o, marketCourse(t, f, o, "Reviews course", "Description", 3), "tech", "en")
	learner := newLearner(t, f, "reviewer")
	if _, err := s.EnrollMarketplace(t.Context(), learner, l.CourseID); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	pass := func(h http.Handler) http.Handler { return h }
	sharing.Mount(mux, s, pass, pass)
	req := httptest.NewRequestWithContext(accounts.WithLearnerID(t.Context(), learner), http.MethodPut, "/v1/marketplace/courses/"+l.CourseID+"/review", strings.NewReader(`{"stars":5,"text":"Helpful"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 422 || !strings.Contains(rec.Body.String(), `"lessons_needed":3`) {
		t.Fatalf("early review = %d %s; want 422 with 3 lessons needed", rec.Code, rec.Body)
	}
}
