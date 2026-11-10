//go:build integration

package notes_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type mockEngineServer struct {
	enginev1.UnimplementedEngineServiceServer
	lastRequest   *enginev1.EnrichNoteRequest
	validLessonID string
	shouldFail    bool
}

func (m *mockEngineServer) EnrichNote(_ context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
	m.lastRequest = req
	if m.shouldFail {
		return nil, fmt.Errorf("engine failure")
	}
	validLink := m.validLessonID
	if validLink == "" {
		validLink = "valid-lesson-id"
	}
	return &enginev1.EnrichNoteResponse{
		Summary: "Apparent star motion comes from Earth's rotation; Polaris barely moves.",
		Tags:    []string{"rotation", "polaris"},
		Links:   []string{validLink, "hallucinated-lesson-uuid"},
		Usage: &enginev1.Usage{
			InputTokens:  120,
			OutputTokens: 60,
			Delivered:    true,
			Component:    "content",
		},
		Attempts: []*enginev1.Usage{
			{
				InputTokens:  120,
				OutputTokens: 60,
				Delivered:    true,
				Component:    "content",
			},
		},
	}, nil
}

func startMockEngine(t *testing.T, srv *mockEngineServer) (string, func()) {
	t.Helper()
	lis, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(s, srv)
	go func() {
		_ = s.Serve(lis)
	}()
	return lis.Addr().String(), func() {
		s.Stop()
	}
}

func TestEnrichNotesWithGroundedSummariesAndTagsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("notes_e902"),
		postgres.WithUsername("fixture"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, container)

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}

	if err = migrations.Up(ctx, url); err != nil {
		t.Fatal(err)
	}

	db, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}

	events := &testEventsDouble{}
	creditsSvc := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, events)

	mockEngine := &mockEngineServer{}
	engineAddr, stopEngine := startMockEngine(t, mockEngine)
	defer stopEngine()

	conn, err := grpc.NewClient(engineAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	engineClient := enginev1.NewEngineServiceClient(conn)

	notesService := notes.NewServiceWithEnrichment(db.Pool, events, creditsSvc, engineClient)

	// Create learner
	learnerID := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `INSERT INTO learners(id, display_name) VALUES($1::uuid, 'Test Learner')`, learnerID)
	if err != nil {
		t.Fatal(err)
	}

	// Create course, unit, lessons
	var quoteID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'ref1', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerID, catalogue.Config().Version).Scan(&quoteID)
	if err != nil {
		t.Fatalf("insert quote: %v", err)
	}
	var creationID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, quoteID).Scan(&creationID)
	if err != nil {
		t.Fatalf("insert creation: %v", err)
	}
	var courseID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title) VALUES($1::uuid, $2::uuid, 'Astronomy') RETURNING id::text`, learnerID, creationID).Scan(&courseID)
	if err != nil {
		t.Fatalf("insert course: %v", err)
	}
	unitID := uuid.New().String()
	_, err = db.Pool.Exec(ctx, `INSERT INTO course_units(id, learner_id, course_id, authored_id, position, title) VALUES($1::uuid, $2::uuid, $3::uuid, 'u1', 0, 'Unit 1')`, unitID, learnerID, courseID)
	if err != nil {
		t.Fatal(err)
	}
	lessonID := uuid.New().String()
	validLessonID := uuid.New().String()
	mockEngine.validLessonID = validLessonID
	// Map validLessonID to an actual uuid in DB
	_, err = db.Pool.Exec(ctx, `INSERT INTO course_lesson_stubs(id, learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1::uuid, $2::uuid, $3::uuid, 'l1', 0, 'Why the sky changes', 'Observe star motion', 'ready_to_generate')`, lessonID, learnerID, unitID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Pool.Exec(ctx, `INSERT INTO course_lesson_stubs(id, learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1::uuid, $2::uuid, $3::uuid, 'l2', 1, 'Following a star', 'Track Polaris', 'ready_to_generate')`, validLessonID, learnerID, unitID)
	if err != nil {
		t.Fatal(err)
	}

	// Grant initial daily credits
	_, err = creditsSvc.EnsureDailyGrant(ctx, learnerID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}

	// 1. Save raw note with lesson context
	rawText := `Stars "move" because Earth spins west → east. Check: does Polaris move?`
	savedNote, err := notesService.SaveNote(ctx, learnerID, notes.SaveNoteInput{
		ActionKey:  "test-e902-key-1",
		CourseID:   &courseID,
		LessonID:   &lessonID,
		SlideID:    ptr(1),
		SlideTitle: "Slide 2 · Why the sky changes",
		Source:     "slide",
		RawText:    rawText,
	})
	if err != nil {
		t.Fatalf("SaveNote failed: %v", err)
	}
	if savedNote.Status != "enrichment_pending" && savedNote.Status != "enriching" {
		t.Fatalf("expected pending or enriching status, got %s", savedNote.Status)
	}

	// 2. Process enrichment job
	err = notesService.ProcessEnrichment(ctx, learnerID, savedNote.ID)
	if err != nil {
		t.Fatalf("ProcessEnrichment failed: %v", err)
	}

	// 3. Verify mockEngine received EnrichNoteRequest with allowed_lessons
	if mockEngine.lastRequest == nil {
		t.Fatal("engine EnrichNote was not called")
	}
	if len(mockEngine.lastRequest.AllowedLessons) == 0 {
		t.Fatal("EnrichNoteRequest.AllowedLessons was empty, expected course lessons")
	}

	// 4. Retrieve note and verify enrichment and grounded links
	enrichedNote, err := notesService.GetNote(ctx, learnerID, savedNote.ID)
	if err != nil {
		t.Fatalf("GetNote failed: %v", err)
	}
	if enrichedNote.Status != "enriched" {
		t.Fatalf("expected status enriched, got %s", enrichedNote.Status)
	}
	if enrichedNote.RawText != rawText {
		t.Fatalf("raw text was modified! expected %q, got %q", rawText, enrichedNote.RawText)
	}
	if enrichedNote.Summary == nil || *enrichedNote.Summary == "" {
		t.Fatal("summary is empty")
	}
	if len(enrichedNote.Tags) == 0 {
		t.Fatal("tags are empty")
	}

	// Foreign/hallucinated link "hallucinated-lesson-uuid" MUST be rejected/dropped!
	for _, link := range enrichedNote.Links {
		if link == "hallucinated-lesson-uuid" {
			t.Fatalf("foreign/hallucinated link was not dropped: %s", link)
		}
	}

	// 5. Verify analytics events: quick_note_enriched and $ai_generation
	var foundEnriched, foundAiGen bool
	events.mu.Lock()
	for _, ev := range events.events {
		if ev.Event == "quick_note_enriched" {
			foundEnriched = true
			if _, hasRaw := ev.Properties["raw_text"]; hasRaw {
				t.Fatal("quick_note_enriched leaked private raw_text!")
			}
		}
		if ev.Event == "$ai_generation" {
			foundAiGen = true
			if ev.Properties["feature"] != "quick_note" {
				t.Fatalf("expected $ai_generation feature quick_note, got %v", ev.Properties["feature"])
			}
		}
	}
	events.mu.Unlock()

	if !foundEnriched {
		t.Fatal("quick_note_enriched event was not captured")
	}
	if !foundAiGen {
		t.Fatal("$ai_generation event was not captured")
	}

	// 6. Test terminal failure releases reservation and keeps raw note intact
	mockEngine.shouldFail = true
	failingNote, err := notesService.SaveNote(ctx, learnerID, notes.SaveNoteInput{
		ActionKey: "test-e902-key-failing",
		CourseID:  &courseID,
		LessonID:  &lessonID,
		SlideID:   ptr(1),
		Source:    "slide",
		RawText:   "Another note that will fail enrichment",
	})
	if err != nil {
		t.Fatal(err)
	}

	_ = notesService.ProcessEnrichment(ctx, learnerID, failingNote.ID)

	refreshedFailing, err := notesService.GetNote(ctx, learnerID, failingNote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshedFailing.Status != "enrichment_failed" {
		t.Fatalf("expected status enrichment_failed, got %s", refreshedFailing.Status)
	}
	if refreshedFailing.RawText != "Another note that will fail enrichment" {
		t.Fatal("failing note raw text was lost or mutated")
	}
}

func ptr[T any](v T) *T {
	return &v
}
