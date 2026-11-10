package notes

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// FilterGroundedLinks filters links to ensure all returned links are in the allowed list, preserving order.
func FilterGroundedLinks(links []string, allowed []string) []string {
	if len(links) == 0 || len(allowed) == 0 {
		return []string{}
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, a := range allowed {
		allowedSet[a] = true
	}
	grounded := make([]string, 0, len(links))
	for _, l := range links {
		if allowedSet[l] {
			grounded = append(grounded, l)
		}
	}
	return grounded
}

// ValidateEnrichmentBounds validates summary length and tag count bounds.
func ValidateEnrichmentBounds(summary string, tags []string) error {
	trimmed := strings.TrimSpace(summary)
	if trimmed == "" {
		return errors.New("enrichment summary cannot be empty")
	}
	if len(summary) > 1000 {
		return errors.New("enrichment summary exceeds 1000 characters")
	}
	if len(tags) > 10 {
		return errors.New("enrichment tags exceed limit of 10")
	}
	return nil
}

// EmitQuickNoteEnriched captures the quick_note_enriched analytics event with privacy protection.
func EmitQuickNoteEnriched(events analytics.Events, learnerID, noteID, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	if platform == "" {
		platform = "web"
	}
	if appVersion == "" {
		appVersion = "0.1.0"
	}
	return events.Capture(learnerID, "quick_note_enriched", map[string]any{
		"source":      "api",
		"platform":    platform,
		"app_version": appVersion,
		"note_id":     noteID,
	})
}

// EmitQuickNoteEnrichmentFailed captures the quick_note_enrichment_failed analytics event with privacy protection.
func EmitQuickNoteEnrichmentFailed(events analytics.Events, learnerID, noteID, reason, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	if platform == "" {
		platform = "web"
	}
	if appVersion == "" {
		appVersion = "0.1.0"
	}
	return events.Capture(learnerID, "quick_note_enrichment_failed", map[string]any{
		"source":      "api",
		"platform":    platform,
		"app_version": appVersion,
		"note_id":     noteID,
		"reason":      reason,
		"error_code":  reason,
	})
}

// NoteEnrichArgs contains the queue payload for enriching a note asynchronously.
type NoteEnrichArgs struct {
	NoteID string `json:"note_id"`
}

// Kind identifies the job kind for River.
func (NoteEnrichArgs) Kind() string { return "note_enrich" }

// NoteEnrichWorker processes async note enrichment jobs.
type NoteEnrichWorker struct {
	river.WorkerDefaults[NoteEnrichArgs]
	service *Service
}

// Work implements the River worker interface.
func (w *NoteEnrichWorker) Work(ctx context.Context, j *river.Job[NoteEnrichArgs]) error {
	return w.service.ProcessEnrichment(ctx, j.Args.NoteID)
}

// NotesQueue creates a River queue client for asynchronous note enrichment.
func (s *Service) NotesQueue(logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &NoteEnrichWorker{service: s})
	client, err := river.NewClient(riverpgxv5.New(s.pool), &river.Config{
		Logger:                 logger,
		Workers:                workers,
		Queues:                 map[string]river.QueueConfig{"notes": {MaxWorkers: 4}},
		LeaderElectionDisabled: true,
	})
	if err != nil {
		return nil, err
	}
	s.riverClient = client
	return client, nil
}

// ProcessEnrichment processes credit reservation, engine gRPC invocation, grounded link filtering, and settlement.
func (s *Service) ProcessEnrichment(ctx context.Context, noteID string) error {
	noteUUID, err := parseUUID(noteID)
	if err != nil {
		return ErrNotFound
	}

	// Fetch raw note
	var note struct {
		ID         pgtype.UUID
		LearnerID  pgtype.UUID
		CourseID   pgtype.UUID
		LessonID   pgtype.UUID
		SlideTitle string
		RawText    string
		Status     string
	}
	err = s.pool.QueryRow(ctx, `
		SELECT id, learner_id, course_id, lesson_id, slide_title, raw_text, status
		FROM quick_notes
		WHERE id = $1
	`, noteUUID).Scan(
		&note.ID,
		&note.LearnerID,
		&note.CourseID,
		&note.LessonID,
		&note.SlideTitle,
		&note.RawText,
		&note.Status,
	)
	if err != nil {
		return ErrNotFound
	}

	// If raw_only, do not enrich
	if note.Status == "raw_only" {
		return nil
	}

	learnerIDStr := formatUUID(note.LearnerID)

	// 1. Create quote for quick_note action from pricing configuration
	quotes := pricing.NewQuotes(s.pool)
	quote, err := quotes.Create(ctx, learnerIDStr, pricing.QuoteInput{Action: "quick_note"})
	if err != nil {
		return err
	}

	if err := quotes.Accept(ctx, learnerIDStr, quote.ID); err != nil {
		return err
	}

	quoteUUID, _ := parseUUID(quote.ID)

	// 2. Reserve daily-first eligible credits
	if s.credits == nil {
		return errors.New("credits service not configured")
	}

	res, err := s.credits.ReserveAction(ctx, learnerIDStr, quote.ID, "quick_note")
	if err != nil {
		if errors.Is(err, credits.ErrInsufficient) {
			// Zero balance leaves enrichment_pending (E9-03 resumes later; do not build resume)
			return nil
		}
		return err
	}

	resUUID, _ := parseUUID(res.ID)

	// Update status to enriching
	_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
		ID:            noteUUID,
		Status:        "enriching",
		ReservationID: resUUID,
		QuoteID:       quoteUUID,
	})

	// 3. Claim reservation
	if err = s.credits.Claim(ctx, learnerIDStr, res.ID); err != nil {
		_, _ = s.credits.Release(ctx, learnerIDStr, res.ID)
		_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
			ID:            noteUUID,
			Status:        "enrichment_failed",
			ReservationID: pgtype.UUID{},
			QuoteID:       pgtype.UUID{},
		})
		_ = EmitQuickNoteEnrichmentFailed(s.events, learnerIDStr, noteID, "claim_failed", "web", "0.1.0")
		return err
	}

	// 4. Retrieve course lessons for grounding
	courseID := note.CourseID
	if !courseID.Valid && note.LessonID.Valid {
		cID, err := s.queries.GetCourseIDForLesson(ctx, note.LessonID)
		if err == nil {
			courseID = cID
		}
	}
	if !courseID.Valid {
		var cID pgtype.UUID
		if err := s.pool.QueryRow(ctx, "SELECT c.id FROM courses c JOIN course_enrollments e ON e.course_id=c.id WHERE e.learner_id=$1 LIMIT 1", note.LearnerID).Scan(&cID); err == nil {
			courseID = cID
		}
	}

	var allowedLessons []*enginev1.LessonRef
	var allowedIDs []string
	if courseID.Valid {
		lessons, err := s.queries.ListCourseLessonsForCourse(ctx, courseID)
		if err == nil {
			allowedLessons = make([]*enginev1.LessonRef, len(lessons))
			allowedIDs = make([]string, len(lessons))
			for i, l := range lessons {
				lID := formatUUID(l.ID)
				allowedLessons[i] = &enginev1.LessonRef{
					LessonId: lID,
					Title:    l.Title,
				}
				allowedIDs[i] = lID
			}
		}
	}

	// 5. Retrieve usage limits from credits reservation
	limits, _ := s.credits.Limits(ctx, learnerIDStr, res.ID, "quick_note")

	// 6. Call engine gRPC
	if s.engineClient == nil {
		_, _ = s.credits.Release(ctx, learnerIDStr, res.ID)
		_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
			ID:            noteUUID,
			Status:        "enrichment_failed",
			ReservationID: pgtype.UUID{},
			QuoteID:       pgtype.UUID{},
		})
		_ = EmitQuickNoteEnrichmentFailed(s.events, learnerIDStr, noteID, "engine_unavailable", "web", "0.1.0")
		return errors.New("engine client not configured")
	}

	deadline := res.DeadlineAt
	if deadline.IsZero() {
		deadline = time.Now().Add(15 * time.Second)
	}
	callCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	var lessonRef *enginev1.LessonRef
	if note.LessonID.Valid {
		lessonRef = &enginev1.LessonRef{
			LessonId: formatUUID(note.LessonID),
			Title:    note.SlideTitle,
		}
	}

	req := &enginev1.EnrichNoteRequest{
		Note:           note.RawText,
		Lesson:         lessonRef,
		Learner:        &enginev1.LearnerProfile{},
		Limits:         limits,
		AllowedLessons: allowedLessons,
	}

	start := time.Now()
	resp, callErr := s.engineClient.EnrichNote(callCtx, req)
	latencyMs := time.Since(start).Milliseconds()

	if callErr != nil {
		// Terminal failure: release reservation, keep raw note, set enrichment_failed
		_, _ = s.credits.Release(ctx, learnerIDStr, res.ID)
		_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
			ID:            noteUUID,
			Status:        "enrichment_failed",
			ReservationID: pgtype.UUID{},
			QuoteID:       pgtype.UUID{},
		})
		_ = EmitQuickNoteEnrichmentFailed(s.events, learnerIDStr, noteID, "engine_failure", "web", "0.1.0")
		if s.events != nil {
			_ = s.events.Capture(learnerIDStr, "$ai_generation", map[string]any{
				"source":       "api",
				"platform":     "web",
				"app_version":  "0.1.0",
				"feature":      "quick_note",
				"succeeded":    false,
				"$ai_is_error": true,
				"error_code":   "engine_failure",
				"latency_ms":   latencyMs,
			})
		}
		return callErr
	}

	// 7. Validate output bounds
	if boundsErr := ValidateEnrichmentBounds(resp.Summary, resp.Tags); boundsErr != nil {
		_, _ = s.credits.Release(ctx, learnerIDStr, res.ID)
		_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
			ID:            noteUUID,
			Status:        "enrichment_failed",
			ReservationID: pgtype.UUID{},
			QuoteID:       pgtype.UUID{},
		})
		_ = EmitQuickNoteEnrichmentFailed(s.events, learnerIDStr, noteID, "bounds_exceeded", "web", "0.1.0")
		return boundsErr
	}

	// 8. Filter grounded links
	groundedLinks := FilterGroundedLinks(resp.Links, allowedIDs)

	// 9. Settle reservation
	attempts := resp.Attempts
	if len(attempts) == 0 && resp.Usage != nil {
		attempts = []*enginev1.Usage{resp.Usage}
	}
	_, _ = s.credits.Settle(ctx, learnerIDStr, res.ID, attempts)

	// 10. Persist enrichment and update status to enriched
	_, err = s.queries.UpsertQuickNoteEnrichment(ctx, db.UpsertQuickNoteEnrichmentParams{
		NoteID:    noteUUID,
		LearnerID: note.LearnerID,
		Summary:   resp.Summary,
		Tags:      resp.Tags,
		Links:     groundedLinks,
		Version:   1,
		Model:     "claude-haiku-4.5",
		Provider:  "anthropic",
	})
	if err != nil {
		return err
	}

	_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
		ID:            noteUUID,
		Status:        "enriched",
		ReservationID: resUUID,
		QuoteID:       quoteUUID,
	})

	// 11. Emit analytics
	_ = EmitQuickNoteEnriched(s.events, learnerIDStr, noteID, "web", "0.1.0")
	if s.events != nil {
		var inTokens, outTokens int64
		if resp.Usage != nil {
			inTokens = int64(resp.Usage.InputTokens)
			outTokens = int64(resp.Usage.OutputTokens)
		}
		_ = s.events.Capture(learnerIDStr, "$ai_generation", map[string]any{
			"source":        "api",
			"platform":      "web",
			"app_version":   "0.1.0",
			"feature":       "quick_note",
			"succeeded":     true,
			"$ai_is_error":  false,
			"input_tokens":  inTokens,
			"output_tokens": outTokens,
			"latency_ms":    latencyMs,
		})
	}

	return nil
}
