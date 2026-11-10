//go:build integration

package notes_test

import (
	"errors"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/google/uuid"
)

func TestNotesReviewFixtureBackdoorsRemoved(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	courseID, lessonID := reviewCourse(t, database, learner)

	// Rename course and lesson to match the former backdoor strings
	if _, err := database.Pool.Exec(t.Context(), "UPDATE courses SET title='Everyday astronomy' WHERE id=$1", courseID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Pool.Exec(t.Context(), "UPDATE course_lesson_stubs SET title='Why the sky changes' WHERE id=$1", lessonID); err != nil {
		t.Fatal(err)
	}

	service := notes.NewService(database.Pool, nil)

	// 1. SaveNote with CourseID = "astronomy" or LessonID = "sky" must not trigger backdoor ILIKE lookups.
	// Specifically, LessonID = "sky" must return ErrUnauthorizedSource (not resolve to lessonID).
	skyStr := "sky"
	_, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{
		ActionKey: uuid.NewString(),
		Source:    "lesson",
		RawText:   "Testing backdoor removal",
		LessonID:  &skyStr,
	})
	if !errors.Is(err, notes.ErrUnauthorizedSource) {
		t.Errorf("SaveNote with LessonID='sky' got %v; want ErrUnauthorizedSource", err)
	}

	// CourseID = "astronomy" must not resolve to courseID
	astronomyStr := "astronomy"
	note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{
		ActionKey: uuid.NewString(),
		Source:    "chat",
		RawText:   "Testing course backdoor",
		CourseID:  &astronomyStr,
	})
	if err != nil {
		t.Fatal(err)
	}
	if note.CourseID != nil {
		t.Errorf("SaveNote with CourseID='astronomy' resolved course %s; want nil", *note.CourseID)
	}

	// 2. FilterNotes with CourseID = "astronomy" or LessonID = "sky" must not resolve backdoor lookups.
	filtered, err := service.FilterNotes(t.Context(), learner, notes.FilterNotesInput{
		LessonID: "sky",
	})
	if err == nil {
		t.Errorf("FilterNotes with LessonID='sky' returned %d notes via backdoor; want error", len(filtered))
	}

	// 3. SaveBookmark with LessonID = "sky" must fail (ErrUnauthorizedSource), not create a NULL-lesson bookmark.
	_, err = service.SaveBookmark(t.Context(), learner, notes.SaveBookmarkInput{
		ActionKey:  uuid.NewString(),
		LessonID:   "sky",
		TargetType: "lesson",
	})
	if !errors.Is(err, notes.ErrUnauthorizedSource) {
		t.Errorf("SaveBookmark with LessonID='sky' got %v; want ErrUnauthorizedSource", err)
	}

	// 4. NULL-lesson bookmarks in the database must not be reported as available: true.
	var nullBookmarkID string
	err = database.Pool.QueryRow(t.Context(), `
		INSERT INTO bookmarks(learner_id, action_key, course_id, lesson_id, slide_id, target_type, course_title, lesson_title, slide_title)
		VALUES($1::uuid, $2, $3::uuid, NULL, NULL, 'lesson', 'Everyday astronomy', 'Why the sky changes', '')
		RETURNING id::text
	`, learner, uuid.NewString(), courseID).Scan(&nullBookmarkID)
	if err != nil {
		t.Fatal(err)
	}
	bm, err := service.GetBookmark(t.Context(), learner, nullBookmarkID)
	if err != nil {
		t.Fatal(err)
	}
	if bm.Available {
		t.Errorf("NULL-lesson bookmark available=%v; want false", bm.Available)
	}
}
