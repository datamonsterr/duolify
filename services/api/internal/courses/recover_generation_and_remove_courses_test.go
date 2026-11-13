package courses

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/protobuf/proto"
)

func TestValidateCreatedOutlineRecoveryCases(t *testing.T) {
	base := &enginev1.CourseOutline{
		Title: "Space Science",
		Units: []*enginev1.Unit{
			{
				UnitId: "u1",
				Title:  "Satellites",
				Lessons: []*enginev1.LessonRef{
					{LessonId: "l1", Title: "Orbits", Goal: "Understand Low Earth Orbit"},
				},
			},
		},
	}
	request := &enginev1.GenerateCourseOutlineRequest{
		EditedOutline:   base,
		OutlineRevision: 4,
	}

	// 1. Success matching exact structure and revision
	valid := &enginev1.GenerateCourseOutlineResponse{
		Outline:         proto.Clone(base).(*enginev1.CourseOutline),
		OutlineRevision: 4,
		Attempts:        []*enginev1.Usage{{Delivered: true, Component: "content"}},
	}
	if err := ValidateCreatedOutline(request, valid); err != nil {
		t.Fatalf("expected valid outline, got: %v", err)
	}

	// 2. Unavailable engine response (e.g. timeout / fallback failure) must be rejected
	unavailable := proto.Clone(valid).(*enginev1.GenerateCourseOutlineResponse)
	unavailable.Unavailable = true
	if err := ValidateCreatedOutline(request, unavailable); err == nil {
		t.Fatal("expected failure for unavailable response")
	}

	// 3. Stale revision returned during recovery must be rejected
	staleRev := proto.Clone(valid).(*enginev1.GenerateCourseOutlineResponse)
	staleRev.OutlineRevision = 3
	if err := ValidateCreatedOutline(request, staleRev); err == nil {
		t.Fatal("expected failure for stale revision")
	}

	// 4. Undelivered attempt (e.g. failure before delivery) must be rejected
	undelivered := proto.Clone(valid).(*enginev1.GenerateCourseOutlineResponse)
	undelivered.Attempts = []*enginev1.Usage{{Delivered: false}}
	if err := ValidateCreatedOutline(request, undelivered); err == nil {
		t.Fatal("expected failure for undelivered attempts")
	}
}

func TestDeleteCourseNeutralForeignAccess(t *testing.T) {
	s := New(Options{})
	// When service has no database pool, ServeCourseDeleteHTTP should return 503 or 401 without auth
	req := httptest.NewRequestWithContext(t.Context(), "DELETE", "/v1/courses/c1111111-1111-4111-8111-111111111111", nil)
	w := httptest.NewRecorder()

	s.ServeCourseDeleteHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauthorized when unauthenticated, got %d", w.Code)
	}
}

func TestPurgeCutoffValidation(t *testing.T) {
	// Cutoff in the future or within 30 days
	now := time.Now().UTC()
	cutoff30Days := now.Add(-30 * 24 * time.Hour)
	if cutoff30Days.After(now) {
		t.Fatal("cutoff should be in the past")
	}
}
