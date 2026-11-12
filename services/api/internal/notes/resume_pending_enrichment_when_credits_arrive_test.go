package notes_test

import (
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
)

type resumeMockEventsRecorder struct {
	events []resumeCapturedEvent
}

type resumeCapturedEvent struct {
	learnerID string
	event     string
	props     map[string]any
}

func (m *resumeMockEventsRecorder) Capture(learnerID, event string, props map[string]any) error {
	m.events = append(m.events, resumeCapturedEvent{
		learnerID: learnerID,
		event:     event,
		props:     props,
	})
	return nil
}

func (m *resumeMockEventsRecorder) Close() error { return nil }

func TestStaleRawRevisionCheck(t *testing.T) {
	// If expected version matches current version, no error
	if err := notes.ValidateExpectedVersion(1, 1); err != nil {
		t.Fatalf("expected version match to succeed, got %v", err)
	}

	// If expected version is 0 (unspecified), accept current
	if err := notes.ValidateExpectedVersion(2, 0); err != nil {
		t.Fatalf("expected version 0 to accept, got %v", err)
	}

	// If expected version is older than current version, must return ErrStaleVersion
	if err := notes.ValidateExpectedVersion(2, 1); err == nil {
		t.Fatal("expected ErrStaleVersion when current version > expected version")
	}
}

func TestSortPendingNotesOldestFirst(t *testing.T) {
	now := time.Now()
	items := []notes.PendingNoteItem{
		{ID: "note-3", CreatedAt: now.Add(2 * time.Minute)},
		{ID: "note-1", CreatedAt: now},
		{ID: "note-2", CreatedAt: now.Add(1 * time.Minute)},
	}

	sorted := notes.SortPendingNotesOldestFirst(items)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 items, got %d", len(sorted))
	}
	if sorted[0].ID != "note-1" || sorted[1].ID != "note-2" || sorted[2].ID != "note-3" {
		t.Fatalf("items not sorted oldest first: %v", sorted)
	}
}

func TestCreditWakeupEventEmitters(t *testing.T) {
	events := &resumeMockEventsRecorder{}
	learnerID := "11111111-1111-4111-8111-111111111111"
	noteID := "22222222-2222-4222-8222-222222222222"

	// 1. Emit quick_note_enriched
	err := notes.EmitQuickNoteEnriched(events, learnerID, noteID, "web", "0.1.0")
	if err != nil {
		t.Fatalf("EmitQuickNoteEnriched failed: %v", err)
	}

	// 2. Emit quick_note_enrichment_failed
	err = notes.EmitQuickNoteEnrichmentFailed(events, learnerID, noteID, "insufficient_credits", "web", "0.1.0")
	if err != nil {
		t.Fatalf("EmitQuickNoteEnrichmentFailed failed: %v", err)
	}

	if len(events.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events.events))
	}

	if events.events[0].event != "quick_note_enriched" {
		t.Fatalf("expected quick_note_enriched, got %s", events.events[0].event)
	}
	if events.events[1].event != "quick_note_enrichment_failed" {
		t.Fatalf("expected quick_note_enrichment_failed, got %s", events.events[1].event)
	}

	// Ensure no private note text was captured in any event
	for i, ev := range events.events {
		for _, forbidden := range []string{"text", "raw_text", "note_text", "summary"} {
			if ev.props[forbidden] != nil {
				t.Fatalf("event %d leaked forbidden property %q: %v", i, forbidden, ev.props)
			}
		}
	}
}
