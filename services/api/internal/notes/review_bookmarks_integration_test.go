//go:build integration

package notes_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/google/uuid"
)

func TestNotesReviewBookmarksDeriveContextOnServer(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	courseID, lessonID := reviewCourse(t, database, learner)

	service := notes.NewService(database.Pool, nil)

	// Client sends spoofed course and titles
	spoofedCourseID := uuid.NewString()
	slide0 := 0
	bm, err := service.SaveBookmark(t.Context(), learner, notes.SaveBookmarkInput{
		ActionKey:   uuid.NewString(),
		CourseID:    spoofedCourseID,
		LessonID:    lessonID,
		SlideID:     &slide0,
		TargetType:  "slide",
		CourseTitle: "Spoofed Client Course",
		LessonTitle: "Spoofed Client Lesson",
		SlideTitle:  "Spoofed Client Slide",
	})
	if err != nil {
		t.Fatalf("SaveBookmark failed: %v", err)
	}

	// Server must have derived the real course and titles from GetLessonContext
	if bm.CourseID == nil || *bm.CourseID != courseID {
		t.Errorf("CourseID = %v; want real course %v", bm.CourseID, courseID)
	}
	if bm.CourseTitle != "Private course" {
		t.Errorf("CourseTitle = %q; want %q", bm.CourseTitle, "Private course")
	}
	if bm.LessonTitle != "Private lesson title" {
		t.Errorf("LessonTitle = %q; want %q", bm.LessonTitle, "Private lesson title")
	}
	if bm.SlideTitle != "Slide 1" {
		t.Errorf("SlideTitle = %q; want %q", bm.SlideTitle, "Slide 1")
	}
}

func TestNotesReviewBookmarksActionKeyReplayConflict(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	_, lessonID := reviewCourse(t, database, learner)

	service := notes.NewService(database.Pool, nil)
	actionKey := uuid.NewString()
	slide0 := 0
	slide1 := 1

	// Save original bookmark
	bm1, err := service.SaveBookmark(t.Context(), learner, notes.SaveBookmarkInput{
		ActionKey:  actionKey,
		LessonID:   lessonID,
		SlideID:    &slide0,
		TargetType: "slide",
	})
	if err != nil {
		t.Fatalf("SaveBookmark 1 failed: %v", err)
	}

	// Replaying with identical target is idempotent
	bmReplay, err := service.SaveBookmark(t.Context(), learner, notes.SaveBookmarkInput{
		ActionKey:  actionKey,
		LessonID:   lessonID,
		SlideID:    &slide0,
		TargetType: "slide",
	})
	if err != nil {
		t.Fatalf("SaveBookmark replay failed: %v", err)
	}
	if bmReplay.ID != bm1.ID {
		t.Fatalf("Replay ID = %v; want %v", bmReplay.ID, bm1.ID)
	}

	// Replaying with DIFFERENT target must return conflict, not the old row silently
	_, err = service.SaveBookmark(t.Context(), learner, notes.SaveBookmarkInput{
		ActionKey:  actionKey,
		LessonID:   lessonID,
		SlideID:    &slide1,
		TargetType: "slide",
	})
	if !errors.Is(err, notes.ErrActionKeyConflict) {
		t.Fatalf("Replay with different slide got %v; want ErrActionKeyConflict", err)
	}
}

func TestNotesReviewBookmarksAvailableEntitlement(t *testing.T) {
	database := reviewDatabase(t)
	owner, _ := reviewLearner(t, database)
	enrolledLearner, _ := reviewLearner(t, database)
	courseID, lessonID := reviewCourse(t, database, owner)

	// Enroll enrolledLearner in owner's course
	_, err := database.Pool.Exec(t.Context(),
		"INSERT INTO course_enrollments(learner_id, course_id) VALUES($1, $2)",
		enrolledLearner, courseID,
	)
	if err != nil {
		t.Fatal(err)
	}

	service := notes.NewService(database.Pool, nil)
	slide0 := 0
	bm, err := service.SaveBookmark(t.Context(), enrolledLearner, notes.SaveBookmarkInput{
		ActionKey:  uuid.NewString(),
		LessonID:   lessonID,
		SlideID:    &slide0,
		TargetType: "slide",
	})
	if err != nil {
		t.Fatalf("SaveBookmark failed: %v", err)
	}

	// Currently enrolled -> available is true
	got, err := service.GetBookmark(t.Context(), enrolledLearner, bm.ID)
	if err != nil {
		t.Fatalf("GetBookmark failed: %v", err)
	}
	if !got.Available {
		t.Errorf("got available=false for enrolled learner; want true")
	}

	// Remove enrollment -> available must reflect current entitlement and become false
	_, err = database.Pool.Exec(t.Context(),
		"DELETE FROM course_enrollments WHERE learner_id=$1 AND course_id=$2",
		enrolledLearner, courseID,
	)
	if err != nil {
		t.Fatal(err)
	}

	gotAfterUnenroll, err := service.GetBookmark(t.Context(), enrolledLearner, bm.ID)
	if err != nil {
		t.Fatalf("GetBookmark after unenroll failed: %v", err)
	}
	if gotAfterUnenroll.Available {
		t.Errorf("got available=true for unenrolled learner; want false (entitlement revoked)")
	}

	list, err := service.ListBookmarks(t.Context(), enrolledLearner, nil)
	if err != nil {
		t.Fatalf("ListBookmarks failed: %v", err)
	}
	if len(list) != 1 || list[0].Available {
		t.Errorf("ListBookmarks got available=%v; want false", list[0].Available)
	}
}

func TestNotesReviewBookmarksRemovalFailureDoesNotReturnRemovedTrue(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)

	service := notes.NewService(database.Pool, nil)

	// Test HTTP handler directly with empty body to /v1/bookmarks/remove
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(accounts.WithLearnerID(r.Context(), learner))
		service.ServeBookmarksHTTP(w, r)
	})

	body := bytes.NewReader([]byte(`{}`))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/bookmarks/remove", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /v1/bookmarks/remove with empty body got %d; want 400 Bad Request", rec.Code)
	}

	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["removed"] == true {
		t.Fatalf("got removed: true on failure; want error response")
	}
}
