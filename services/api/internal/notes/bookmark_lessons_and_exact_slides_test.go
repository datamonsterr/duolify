package notes_test

import (
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
)

func TestValidateBookmarkInputBoundaries(t *testing.T) {
	validUUID := "11111111-1111-4111-8111-111111111111"
	slide0 := 0
	slideTooLarge := 10001
	negativeSlide := -1

	tests := []struct {
		name    string
		input   notes.SaveBookmarkInput
		wantErr error
	}{
		{
			name: "empty action key",
			input: notes.SaveBookmarkInput{
				ActionKey:  "",
				LessonID:   validUUID,
				SlideID:    &slide0,
				TargetType: "slide",
			},
			wantErr: notes.ErrMissingActionKey,
		},
		{
			name: "action key exceeds length",
			input: notes.SaveBookmarkInput{
				ActionKey:  strings.Repeat("a", 129),
				LessonID:   validUUID,
				SlideID:    &slide0,
				TargetType: "slide",
			},
			wantErr: notes.ErrInvalidActionKey,
		},
		{
			name: "missing lesson id",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   "",
				SlideID:    &slide0,
				TargetType: "slide",
			},
			wantErr: notes.ErrMissingLesson,
		},
		{
			name: "invalid target type",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    &slide0,
				TargetType: "unknown",
			},
			wantErr: notes.ErrInvalidTargetType,
		},
		{
			name: "slide target missing slide_id",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    nil,
				TargetType: "slide",
			},
			wantErr: notes.ErrInvalidSlide,
		},
		{
			name: "slide target negative slide_id",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    &negativeSlide,
				TargetType: "slide",
			},
			wantErr: notes.ErrInvalidSlide,
		},
		{
			name: "slide target slide_id exceeds bound",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    &slideTooLarge,
				TargetType: "slide",
			},
			wantErr: notes.ErrInvalidSlide,
		},
		{
			name: "lesson target with slide_id must be rejected",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    &slide0,
				TargetType: "lesson",
			},
			wantErr: notes.ErrInvalidSlide,
		},
		{
			name: "title exceeds bound",
			input: notes.SaveBookmarkInput{
				ActionKey:   "act-1",
				LessonID:    validUUID,
				SlideID:     &slide0,
				TargetType:  "slide",
				CourseTitle: strings.Repeat("x", 501),
			},
			wantErr: notes.ErrTitleTooLong,
		},
		{
			name: "valid slide target",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    &slide0,
				TargetType: "slide",
			},
			wantErr: nil,
		},
		{
			name: "valid lesson target without slide_id",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    nil,
				TargetType: "lesson",
			},
			wantErr: nil,
		},
		{
			name: "valid slide target with slide_id",
			input: notes.SaveBookmarkInput{
				ActionKey:  "act-1",
				LessonID:   validUUID,
				SlideID:    &slide0,
				TargetType: "slide",
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if err != tt.wantErr && !strings.Contains(err.Error(), tt.wantErr.Error()) {
					t.Fatalf("expected error %v, got %v", tt.wantErr, err)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestBookmarkAnalyticsEventEmitters(t *testing.T) {
	recorder := &testEventsRecorder{}
	learnerID := "22222222-2222-4222-8222-222222222222"
	courseID := "33333333-3333-4333-8333-333333333333"
	lessonID := "44444444-4444-4444-8444-444444444444"
	slideID := 2

	// 1. EmitBookmarkAdded
	err := notes.EmitBookmarkAdded(recorder, learnerID, "slide", courseID, lessonID, &slideID, "web", "0.1.0")
	if err != nil {
		t.Fatalf("EmitBookmarkAdded err: %v", err)
	}

	// 2. EmitBookmarkRemoved
	err = notes.EmitBookmarkRemoved(recorder, learnerID, "slide", courseID, lessonID, &slideID, "web", "0.1.0")
	if err != nil {
		t.Fatalf("EmitBookmarkRemoved err: %v", err)
	}

	// 3. EmitBookmarkOpened
	err = notes.EmitBookmarkOpened(recorder, learnerID, "slide", courseID, lessonID, &slideID, "web", "0.1.0")
	if err != nil {
		t.Fatalf("EmitBookmarkOpened err: %v", err)
	}

	if len(recorder.events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(recorder.events))
	}

	expectedNames := []string{"bookmark_added", "bookmark_removed", "bookmark_opened"}
	for i, name := range expectedNames {
		ev := recorder.events[i]
		if ev.Event != name {
			t.Errorf("event %d: expected name %q, got %q", i, name, ev.Event)
		}
		if ev.Properties["target_type"] != "slide" {
			t.Errorf("event %d: expected target_type 'slide', got %v", i, ev.Properties["target_type"])
		}
		if ev.Properties["course_id"] != courseID {
			t.Errorf("event %d: expected course_id %q, got %v", i, courseID, ev.Properties["course_id"])
		}
		if ev.Properties["lesson_id"] != lessonID {
			t.Errorf("event %d: expected lesson_id %q, got %v", i, lessonID, ev.Properties["lesson_id"])
		}
	}
}
