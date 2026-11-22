//go:build integration

package courses_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
)

// Ids are bound as `$n::uuid` against native columns, so Postgres would raise 22P02 for a malformed
// one. Every route answers it exactly like an unknown id, never with a database error.
func TestMalformedIDsAnswerLikeUnknownIDsIntegration(t *testing.T) {
	ctx := t.Context()
	pool, learner := objectPurgeDB(t)
	svc := courses.New(courses.Options{Pool: pool})
	const unknown = "00000000-0000-4000-8000-000000000000"
	routes := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string // {id} is the id under test
		body    string
	}{
		{"course path", svc.ServeCourseHTTP, http.MethodGet, "/v1/courses/{id}", ""},
		{"course delete", svc.ServeCourseDeleteHTTP, http.MethodDelete, "/v1/courses/{id}", ""},
		{"course select", svc.ServeCourseSelectHTTP, http.MethodPost, "/v1/courses/{id}/select", ""},
		{"course level", svc.ServeCourseLevelHTTP, http.MethodPut, "/v1/courses/{id}/level", `{"level":"beginner"}`},
		{"creation status", svc.ServeCreationHTTP, http.MethodGet, "/v1/course-creations/{id}", ""},
		{"creation events", svc.ServeCreationHTTP, http.MethodGet, "/v1/course-creations/{id}/events", ""},
		{"creation quote", svc.ServeCreationHTTP, http.MethodPost, "/v1/course-creations/quote", `{"draft_id":"{id}","expected_revision":1}`},
		{"creation start", svc.ServeCreationHTTP, http.MethodPost, "/v1/course-creations", `{"draft_id":"{id}","expected_revision":1,"quote_id":"{id}"}`},
		{"outline read", svc.ServeOutlineHTTP, http.MethodGet, "/v1/outline-drafts/{id}", ""},
		{"outline delete", svc.ServeOutlineHTTP, http.MethodDelete, "/v1/outline-drafts/{id}", ""},
		{"outline confirm", svc.ServeOutlineHTTP, http.MethodPost, "/v1/outline-drafts/{id}/confirm", `{"expected_revision":1}`},
		{"outline confirmed", svc.ServeOutlineHTTP, http.MethodGet, "/v1/outline-drafts/{id}/confirmed?expected_revision=1", ""},
		{"outline by brief", svc.ServeOutlineHTTP, http.MethodGet, "/v1/outline-drafts?brief_revision={id}", ""},
		{"proposal read", svc.ServeProposalHTTP, http.MethodGet, "/v1/outline-proposals/{id}", ""},
		{"proposal quote lookup", svc.ServeProposalHTTP, http.MethodGet, "/v1/outline-proposals/" + unknown + "?quote_id={id}", ""},
		{"proposal apply", svc.ServeProposalHTTP, http.MethodPost, "/v1/outline-proposals/{id}/apply", `{}`},
	}
	for _, route := range routes {
		call := func(id string) (int, string) {
			body := strings.NewReader(strings.ReplaceAll(route.body, "{id}", id))
			req := httptest.NewRequestWithContext(accounts.WithLearnerID(ctx, learner), route.method, strings.ReplaceAll(route.path, "{id}", id), body)
			rec := httptest.NewRecorder()
			route.handler(rec, req)
			return rec.Code, rec.Body.String()
		}
		for _, malformed := range []string{"not-a-uuid", "astronomy", "demo", "123", "0000000000000000000000000000000z", unknown + "x"} {
			t.Run(route.name+"/"+malformed, func(t *testing.T) {
				want, wantBody := call(unknown)
				got, gotBody := call(malformed)
				if got >= 500 {
					t.Fatalf("malformed id %q: status %d %s", malformed, got, gotBody)
				}
				if got != want || gotBody != wantBody {
					t.Fatalf("malformed id %q: %d %s; unknown id: %d %s", malformed, got, gotBody, want, wantBody)
				}
			})
		}
	}
}
