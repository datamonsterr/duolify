//go:build integration

package notes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/google/uuid"
)

func TestNotesReviewILIKEWildcardEscaping(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	service := notes.NewService(database.Pool, nil)

	// Note 1 contains literal '%'
	_, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{
		ActionKey: uuid.NewString(),
		Source:    "chat",
		RawText:   "Special offer 100% guarantee",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Note 2 contains prefix without '%'
	_, err = service.SaveNote(t.Context(), learner, notes.SaveNoteInput{
		ActionKey: uuid.NewString(),
		Source:    "chat",
		RawText:   "Special offer 100 dollars",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Note 3 contains literal '_'
	_, err = service.SaveNote(t.Context(), learner, notes.SaveNoteInput{
		ActionKey: uuid.NewString(),
		Source:    "chat",
		RawText:   "Variable test_case name",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Note 4 contains character matching '_' wildcard
	_, err = service.SaveNote(t.Context(), learner, notes.SaveNoteInput{
		ActionKey: uuid.NewString(),
		Source:    "chat",
		RawText:   "Variable testXcase name",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Search for literal "100%" - must ONLY match Note 1, NOT Note 2
	resultsPercent, err := service.FilterNotes(t.Context(), learner, notes.FilterNotesInput{
		Query: "100%",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resultsPercent) != 1 {
		t.Fatalf("search for '100%%' returned %d notes; want exactly 1 (literal match only)", len(resultsPercent))
	}
	if resultsPercent[0].RawText != "Special offer 100% guarantee" {
		t.Errorf("got %q; want %q", resultsPercent[0].RawText, "Special offer 100% guarantee")
	}

	// Search for literal "test_case" - must ONLY match Note 3, NOT Note 4
	resultsUnderscore, err := service.FilterNotes(t.Context(), learner, notes.FilterNotesInput{
		Query: "test_case",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resultsUnderscore) != 1 {
		t.Fatalf("search for 'test_case' returned %d notes; want exactly 1 (literal match only)", len(resultsUnderscore))
	}
	if resultsUnderscore[0].RawText != "Variable test_case name" {
		t.Errorf("got %q; want %q", resultsUnderscore[0].RawText, "Variable test_case name")
	}
}

func TestNotesReviewInvalidFiltersReturn400(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	service := notes.NewService(database.Pool, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(accounts.WithLearnerID(r.Context(), learner))
		service.ServeHTTP(w, r)
	})

	bmHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(accounts.WithLearnerID(r.Context(), learner))
		service.ServeBookmarksHTTP(w, r)
	})

	testCases := []struct {
		name    string
		handler http.Handler
		path    string
	}{
		{
			name:    "invalid status filter",
			handler: handler,
			path:    "/v1/notes?status=invalid_status_xyz",
		},
		{
			name:    "invalid reviewed filter",
			handler: handler,
			path:    "/v1/notes?reviewed=not_a_bool",
		},
		{
			name:    "invalid course_id filter in notes",
			handler: handler,
			path:    "/v1/notes?course_id=not-a-uuid",
		},
		{
			name:    "invalid lesson_id filter in notes",
			handler: handler,
			path:    "/v1/notes?lesson_id=not-a-uuid",
		},
		{
			name:    "invalid course_id filter in bookmarks",
			handler: bmHandler,
			path:    "/v1/bookmarks?course_id=not-a-uuid",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			tc.handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: got status %d; want 400 Bad Request", tc.name, rec.Code)
			}
		})
	}
}

func TestNotesReviewMaxBytesReaderOnRequestBodies(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	service := notes.NewService(database.Pool, nil)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(accounts.WithLearnerID(r.Context(), learner))
		service.ServeHTTP(w, r)
	})

	bmHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(accounts.WithLearnerID(r.Context(), learner))
		service.ServeBookmarksHTTP(w, r)
	})

	// Create oversized payload (> 64KB)
	hugePayload := map[string]any{
		"action_key": uuid.NewString(),
		"source":     "chat",
		"raw_text":   strings.Repeat("a", 70000),
	}
	bodyBytes, _ := json.Marshal(hugePayload)

	// POST /v1/notes with oversized body
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/notes", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge && rec.Code != http.StatusBadRequest {
		t.Errorf("POST /v1/notes oversized got status %d; want 413 or 400", rec.Code)
	}

	// POST /v1/bookmarks with oversized body
	reqBm := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/bookmarks", bytes.NewReader(bodyBytes))
	reqBm.Header.Set("Content-Type", "application/json")
	recBm := httptest.NewRecorder()
	bmHandler.ServeHTTP(recBm, reqBm)

	if recBm.Code != http.StatusRequestEntityTooLarge && recBm.Code != http.StatusBadRequest {
		t.Errorf("POST /v1/bookmarks oversized got status %d; want 413 or 400", recBm.Code)
	}
}

func TestNotesReviewAnalyticsConfigAndHeaders(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	courseID, lessonID := reviewCourse(t, database, learner)

	events := &bookmarksTestEventsDouble{}
	service := notes.NewService(database.Pool, events)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(accounts.WithLearnerID(r.Context(), learner))
		service.ServeHTTP(w, r)
	})

	bmHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(accounts.WithLearnerID(r.Context(), learner))
		service.ServeBookmarksHTTP(w, r)
	})

	// Request with custom X-Platform and X-App-Version headers
	noteBody, _ := json.Marshal(map[string]any{
		"action_key": uuid.NewString(),
		"source":     "chat",
		"raw_text":   "Testing analytics headers",
	})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/notes", bytes.NewReader(noteBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform", "desktop")
	req.Header.Set("X-App-Version", "2.1.0")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Save note failed with status %d: %s", rec.Code, rec.Body.String())
	}

	// Check that emitted quick_note_created event has desktop and 2.1.0
	events.mu.Lock()
	var noteCreatedEvent *capturedEvent
	for i := range events.events {
		if events.events[i].Event == "quick_note_created" {
			noteCreatedEvent = &events.events[i]
			break
		}
	}
	events.mu.Unlock()

	if noteCreatedEvent == nil {
		t.Fatal("quick_note_created event not captured")
	}
	if noteCreatedEvent.Properties["platform"] != "desktop" {
		t.Errorf("quick_note_created platform = %v; want 'desktop'", noteCreatedEvent.Properties["platform"])
	}
	if noteCreatedEvent.Properties["app_version"] != "2.1.0" {
		t.Errorf("quick_note_created app_version = %v; want '2.1.0'", noteCreatedEvent.Properties["app_version"])
	}

	// Bookmark request with custom headers
	bmBody, _ := json.Marshal(map[string]any{
		"action_key":  uuid.NewString(),
		"course_id":   courseID,
		"lesson_id":   lessonID,
		"target_type": "lesson",
	})
	reqBm := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/bookmarks", bytes.NewReader(bmBody))
	reqBm.Header.Set("Content-Type", "application/json")
	reqBm.Header.Set("X-Platform", "desktop")
	reqBm.Header.Set("X-App-Version", "2.1.0")
	recBm := httptest.NewRecorder()
	bmHandler.ServeHTTP(recBm, reqBm)

	if recBm.Code != http.StatusOK {
		t.Fatalf("Save bookmark failed with status %d: %s", recBm.Code, recBm.Body.String())
	}

	events.mu.Lock()
	var bmAddedEvent *capturedEvent
	for i := range events.events {
		if events.events[i].Event == "bookmark_added" {
			bmAddedEvent = &events.events[i]
			break
		}
	}
	events.mu.Unlock()

	if bmAddedEvent == nil {
		t.Fatal("bookmark_added event not captured")
	}
	if bmAddedEvent.Properties["platform"] != "desktop" {
		t.Errorf("bookmark_added platform = %v; want 'desktop'", bmAddedEvent.Properties["platform"])
	}
	if bmAddedEvent.Properties["app_version"] != "2.1.0" {
		t.Errorf("bookmark_added app_version = %v; want '2.1.0'", bmAddedEvent.Properties["app_version"])
	}
}

func TestNotesReviewModelAndProviderFromEngineResponse(t *testing.T) {
	database := reviewDatabase(t)
	learner, financial := reviewLearner(t, database)
	courseID, lessonID := reviewCourse(t, database, learner)

	engineMock := &reviewEngine{
		enrich: func(_ context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
			res := reviewResponse(req)
			// Return distinct provider and model from engine
			res.Usage = &enginev1.Usage{
				Provider:     "anthropic-bedrock",
				Model:        "claude-3-7-sonnet-20250219",
				Attempt:      1,
				Component:    "content",
				Delivered:    true,
				InputTokens:  120,
				OutputTokens: 60,
			}
			return res, nil
		},
	}

	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, engineMock)
	lessonIDStr := lessonID
	courseIDStr := courseID
	note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{
		ActionKey: uuid.NewString(),
		Source:    "lesson",
		RawText:   "Test model and provider persistence from engine",
		LessonID:  &lessonIDStr,
		CourseID:  &courseIDStr,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := service.ProcessEnrichment(t.Context(), learner, note.ID); err != nil {
		t.Fatalf("ProcessEnrichment failed: %v", err)
	}

	// Verify enriched note has provider and model from the engine response in database
	var storedModel, storedProvider string
	err = database.Pool.QueryRow(t.Context(), "SELECT model, provider FROM quick_note_enrichments WHERE note_id=$1", note.ID).Scan(&storedModel, &storedProvider)
	if err != nil {
		t.Fatalf("failed to query quick_note_enrichments: %v", err)
	}
	if storedModel != "claude-3-7-sonnet-20250219" {
		t.Errorf("stored model = %q; want 'claude-3-7-sonnet-20250219'", storedModel)
	}
	if storedProvider != "anthropic-bedrock" {
		t.Errorf("stored provider = %q; want 'anthropic-bedrock'", storedProvider)
	}
}
