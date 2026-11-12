package notes_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
)

type testEventsRecorder struct {
	mu     sync.Mutex
	events []struct {
		LearnerID  string
		Event      string
		Properties map[string]any
	}
}

func (r *testEventsRecorder) Capture(learnerID, event string, properties map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, struct {
		LearnerID  string
		Event      string
		Properties map[string]any
	}{learnerID, event, properties})
	return nil
}

func (r *testEventsRecorder) Close() error { return nil }

func TestValidateQuickNoteInputBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		input   notes.SaveNoteInput
		wantErr error
	}{
		{
			name: "empty action key",
			input: notes.SaveNoteInput{
				ActionKey: "",
				Source:    "slide",
				RawText:   "Valid text",
			},
			wantErr: notes.ErrMissingActionKey,
		},
		{
			name: "empty raw text",
			input: notes.SaveNoteInput{
				ActionKey: "act-1",
				Source:    "slide",
				RawText:   "",
			},
			wantErr: notes.ErrInvalidText,
		},
		{
			name: "whitespace only raw text",
			input: notes.SaveNoteInput{
				ActionKey: "act-1",
				Source:    "slide",
				RawText:   "   \n\t  ",
			},
			wantErr: notes.ErrInvalidText,
		},
		{
			name: "raw text exceeding 2000 chars",
			input: notes.SaveNoteInput{
				ActionKey: "act-1",
				Source:    "slide",
				RawText:   strings.Repeat("x", 2001),
			},
			wantErr: notes.ErrTextTooLong,
		},
		{
			name: "invalid source",
			input: notes.SaveNoteInput{
				ActionKey: "act-1",
				Source:    "invalid_source",
				RawText:   "Valid note",
			},
			wantErr: notes.ErrInvalidSource,
		},
		{
			name: "slide source without slide id",
			input: notes.SaveNoteInput{
				ActionKey: "act-1",
				Source:    "slide",
				RawText:   "Valid note",
				SlideID:   nil,
			},
			wantErr: notes.ErrInvalidSlide,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if err != tc.wantErr {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestQuickNoteAnalyticsEmitterExcludesPrivateText(t *testing.T) {
	events := &testEventsRecorder{}
	learnerID := "11111111-1111-4111-8111-111111111111"
	privateNoteText := "Secret password or private notes"

	err := notes.EmitQuickNoteCreated(events, learnerID, "slide", len(privateNoteText), "web", "0.1.0")
	if err != nil {
		t.Fatalf("emit error: %v", err)
	}

	events.mu.Lock()
	defer events.mu.Unlock()
	if len(events.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events.events))
	}

	ev := events.events[0]
	if ev.Event != "quick_note_created" {
		t.Errorf("expected quick_note_created, got %s", ev.Event)
	}
	if ev.LearnerID != learnerID {
		t.Errorf("expected learner ID %s, got %s", learnerID, ev.LearnerID)
	}
	if ev.Properties["source"] != "slide" {
		t.Errorf("expected source slide, got %v", ev.Properties["source"])
	}
	if ev.Properties["length"] != len(privateNoteText) {
		t.Errorf("expected length %d, got %v", len(privateNoteText), ev.Properties["length"])
	}

	rawProps, _ := json.Marshal(ev.Properties)
	if strings.Contains(string(rawProps), "Secret") || strings.Contains(string(rawProps), privateNoteText) {
		t.Fatalf("private note text leaked into analytics properties: %s", string(rawProps))
	}
}
