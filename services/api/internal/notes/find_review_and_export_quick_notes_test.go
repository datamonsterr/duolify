package notes_test

import (
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
)

func TestUpdateNoteInputValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     notes.UpdateNoteInput
		expectErr error
	}{
		{
			name: "valid edit",
			input: notes.UpdateNoteInput{
				RawText:         "Valid updated note text.",
				ExpectedVersion: 1,
			},
			expectErr: nil,
		},
		{
			name: "empty raw text",
			input: notes.UpdateNoteInput{
				RawText:         "",
				ExpectedVersion: 1,
			},
			expectErr: notes.ErrInvalidText,
		},
		{
			name: "whitespace raw text",
			input: notes.UpdateNoteInput{
				RawText:         "   \n\t  ",
				ExpectedVersion: 1,
			},
			expectErr: notes.ErrInvalidText,
		},
		{
			name: "too long raw text",
			input: notes.UpdateNoteInput{
				RawText:         strings.Repeat("a", 2001),
				ExpectedVersion: 1,
			},
			expectErr: notes.ErrTextTooLong,
		},
		{
			name: "invalid expected version",
			input: notes.UpdateNoteInput{
				RawText:         "Valid text",
				ExpectedVersion: 0,
			},
			expectErr: notes.ErrInvalidVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.input.Validate()
			if tt.expectErr == nil && err != nil {
				t.Fatalf("expected nil error, got: %v", err)
			}
			if tt.expectErr != nil && err != tt.expectErr {
				t.Fatalf("expected error %v, got %v", tt.expectErr, err)
			}
		})
	}
}

func TestFormatNoteMarkdown(t *testing.T) {
	summary := "Earth rotation causes apparent star motion."
	now := time.Now()
	courseID := "course-astronomy"
	lessonID := "lesson-sky"
	slideID := 2

	noteWithEnrichment := notes.NoteDTO{
		ID:         "note-123",
		RawText:    "Stars move because Earth spins.",
		SlideTitle: "Why the sky changes · Slide 2",
		CourseID:   &courseID,
		LessonID:   &lessonID,
		SlideID:    &slideID,
		Source:     "slide",
		Status:     "enriched",
		Reviewed:   true,
		ReviewedAt: &now,
		Summary:    &summary,
		Tags:       []string{"rotation", "polaris"},
		Links:      []string{"Following a star"},
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	md := notes.FormatNoteMarkdown(noteWithEnrichment)

	// Check clear separation
	if !strings.Contains(md, "## Your original note") {
		t.Fatalf("missing 'Your original note' header: %s", md)
	}
	if !strings.Contains(md, "## AI additions") {
		t.Fatalf("missing 'AI additions' header: %s", md)
	}
	if !strings.Contains(md, "Stars move because Earth spins.") {
		t.Fatalf("missing raw note text: %s", md)
	}
	if !strings.Contains(md, "Summary: Earth rotation causes apparent star motion.") {
		t.Fatalf("missing summary: %s", md)
	}
	if !strings.Contains(md, "#rotation") || !strings.Contains(md, "#polaris") {
		t.Fatalf("missing tags: %s", md)
	}
	if !strings.Contains(md, "Following a star") {
		t.Fatalf("missing related link: %s", md)
	}

	// Note without enrichment should NOT include AI additions section
	noteWithoutEnrichment := notes.NoteDTO{
		ID:        "note-456",
		RawText:   "Raw note only.",
		Source:    "chat",
		Status:    "raw_only",
		CreatedAt: now,
		UpdatedAt: now,
	}

	mdRaw := notes.FormatNoteMarkdown(noteWithoutEnrichment)
	if !strings.Contains(mdRaw, "## Your original note") {
		t.Fatalf("missing 'Your original note' in raw export: %s", mdRaw)
	}
	if strings.Contains(mdRaw, "## AI additions") {
		t.Fatalf("raw export should not contain 'AI additions': %s", mdRaw)
	}
}

func TestNoteReviewAndDeleteAnalyticsDoNotLeakPrivateText(t *testing.T) {
	events := &testEventsRecorder{}
	learnerID := "22222222-2222-4222-8222-222222222222"
	noteID := "33333333-3333-4333-8333-333333333333"

	// Emit review event
	err := notes.EmitQuickNoteReviewed(events, learnerID, noteID, "web", "0.1.0")
	if err != nil {
		t.Fatalf("emit reviewed: %v", err)
	}

	// Emit delete event
	err = notes.EmitQuickNoteDeleted(events, learnerID, noteID, "web", "0.1.0")
	if err != nil {
		t.Fatalf("emit deleted: %v", err)
	}

	if len(events.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events.events))
	}

	for _, ev := range events.events {
		if ev.LearnerID != learnerID {
			t.Fatalf("expected learnerID %s, got %s", learnerID, ev.LearnerID)
		}
		if ev.Properties["note_id"] != noteID {
			t.Fatalf("expected note_id %s, got %v", noteID, ev.Properties["note_id"])
		}
		// Confirm NO note text, search query, or private content in properties
		for _, forbidden := range []string{"raw_text", "text", "query", "search", "content"} {
			if ev.Properties[forbidden] != nil {
				t.Fatalf("leaked forbidden property %s in event %s", forbidden, ev.Event)
			}
		}
	}
}
