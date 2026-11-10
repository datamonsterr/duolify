package notes_test

import (
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
)

func TestFilterGroundedLinks(t *testing.T) {
	allowed := []string{"lesson-1", "lesson-2", "lesson-3"}
	links := []string{"lesson-2", "unknown-foreign-lesson", "lesson-1", "hallucinated-id"}

	grounded := notes.FilterGroundedLinks(links, allowed)
	if len(grounded) != 2 {
		t.Fatalf("expected 2 grounded links, got %d: %v", len(grounded), grounded)
	}
	if grounded[0] != "lesson-2" || grounded[1] != "lesson-1" {
		t.Fatalf("unexpected grounded links: %v", grounded)
	}
}

func TestValidateEnrichmentBounds(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		tags    []string
		wantErr bool
	}{
		{
			name:    "valid summary and tags",
			summary: "Earth rotates west to east.",
			tags:    []string{"rotation", "polaris"},
			wantErr: false,
		},
		{
			name:    "empty summary",
			summary: "   ",
			tags:    []string{"astronomy"},
			wantErr: true,
		},
		{
			name:    "summary exceeds 500 chars (501 chars)",
			summary: strings.Repeat("a", 501),
			tags:    []string{"astronomy"},
			wantErr: true,
		},
		{
			name:    "summary too long (over 1000 chars)",
			summary: strings.Repeat("a", 1001),
			tags:    []string{"astronomy"},
			wantErr: true,
		},
		{
			name:    "too many tags (over 10 tags)",
			summary: "Valid summary",
			tags:    []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := notes.ValidateEnrichmentBounds(tt.summary, tt.tags)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateEnrichmentBounds(%q, %v) err = %v, wantErr = %v", tt.summary, tt.tags, err, tt.wantErr)
			}
		})
	}
}

func TestEmitEnrichmentAnalytics(t *testing.T) {
	recorder := &testEventsRecorder{}
	learnerID := "11111111-1111-4111-8111-111111111111"

	err := notes.EmitQuickNoteEnriched(recorder, learnerID, "note-uuid-1", "web", "0.1.0")
	if err != nil {
		t.Fatalf("EmitQuickNoteEnriched failed: %v", err)
	}

	err = notes.EmitQuickNoteEnrichmentFailed(recorder, learnerID, "note-uuid-2", "timeout", "web", "0.1.0")
	if err != nil {
		t.Fatalf("EmitQuickNoteEnrichmentFailed failed: %v", err)
	}

	if len(recorder.events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(recorder.events))
	}

	e1 := recorder.events[0]
	if e1.Event != "quick_note_enriched" {
		t.Fatalf("expected quick_note_enriched, got %s", e1.Event)
	}
	if _, ok := e1.Properties["raw_text"]; ok {
		t.Fatal("quick_note_enriched leaked private text")
	}

	e2 := recorder.events[1]
	if e2.Event != "quick_note_enrichment_failed" {
		t.Fatalf("expected quick_note_enrichment_failed, got %s", e2.Event)
	}
	if _, ok := e2.Properties["raw_text"]; ok {
		t.Fatal("quick_note_enrichment_failed leaked private text")
	}
}
