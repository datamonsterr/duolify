package notes

import (
	"context"
	"errors"
	"log/slog"
	"sort"
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

// ErrStaleVersion indicates that a raw note was updated since it was queued for enrichment.
var ErrStaleVersion = errors.New("raw revision conflict: note has been modified since queued")

// ValidateExpectedVersion verifies that the note's current version has not advanced past the expected version.
func ValidateExpectedVersion(currentVersion, expectedVersion int) error {
	if expectedVersion == 0 {
		return nil
	}
	if currentVersion > expectedVersion {
		return ErrStaleVersion
	}
	return nil
}

// PendingNoteItem models a quick note awaiting enrichment.
type PendingNoteItem struct {
	ID         string    `json:"id"`
	LearnerID  string    `json:"learner_id,omitempty"`
	RawVersion int       `json:"raw_version,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SortPendingNotesOldestFirst sorts pending note items by CreatedAt ascending (oldest first).
func SortPendingNotesOldestFirst(items []PendingNoteItem) []PendingNoteItem {
	sorted := make([]PendingNoteItem, len(items))
	copy(sorted, items)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
	})
	return sorted
}

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
	NoteID          string `json:"note_id"`
	LearnerID       string `json:"learner_id"`
	ExpectedVersion int    `json:"expected_version,omitempty"`
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
	err := w.service.ProcessEnrichmentWithVersion(ctx, j.Args.LearnerID, j.Args.NoteID, j.Args.ExpectedVersion)
	if err != nil && (errors.Is(err, credits.ErrInsufficient) || errors.Is(err, ErrStaleVersion)) {
		return nil
	}
	return err
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
func (s *Service) ProcessEnrichment(ctx context.Context, learnerID, noteID string) error {
	return s.ProcessEnrichmentWithVersion(ctx, learnerID, noteID, 0)
}

// ProcessEnrichmentWithVersion processes credit reservation and AI enrichment while ensuring raw_version integrity.
func (s *Service) ProcessEnrichmentWithVersion(ctx context.Context, learnerID, noteID string, expectedVersion int) error {
	noteUUID, err := parseUUID(noteID)
	if err != nil {
		return ErrNotFound
	}

	learnerUUID, err := parseUUID(learnerID)
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
		RawVersion int32
	}
	err = s.pool.QueryRow(ctx, `
		SELECT id, learner_id, course_id, lesson_id, slide_title, raw_text, status, raw_version
		FROM quick_notes
		WHERE id = $1 AND learner_id = $2
	`, noteUUID, learnerUUID).Scan(
		&note.ID,
		&note.LearnerID,
		&note.CourseID,
		&note.LessonID,
		&note.SlideTitle,
		&note.RawText,
		&note.Status,
		&note.RawVersion,
	)
	if err != nil {
		return ErrNotFound
	}

	// Validate expected revision before spending credits or touching AI
	if err := ValidateExpectedVersion(int(note.RawVersion), expectedVersion); err != nil {
		return err
	}

	// If raw_only, do not enrich
	if note.Status == "raw_only" {
		return nil
	}

	// If already enriched with this version, do not charge or re-enrich
	if note.Status == "enriched" {
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
			return credits.ErrInsufficient
		}
		return err
	}

	resUUID, _ := parseUUID(res.ID)

	// Update status to enriching
	_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
		ID:            noteUUID,
		LearnerID:     learnerUUID,
		Status:        "enriching",
		ReservationID: resUUID,
		QuoteID:       quoteUUID,
	})

	// 3. Claim reservation
	if err = s.credits.Claim(ctx, learnerIDStr, res.ID); err != nil {
		_, _ = s.credits.Release(ctx, learnerIDStr, res.ID)
		_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
			ID:            noteUUID,
			LearnerID:     learnerUUID,
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
			LearnerID:     learnerUUID,
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
		_, _ = s.credits.Settle(ctx, learnerIDStr, res.ID, nil)
		_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
			ID:            noteUUID,
			LearnerID:     learnerUUID,
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
		_, _ = s.credits.Settle(ctx, learnerIDStr, res.ID, nil)
		_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
			ID:            noteUUID,
			LearnerID:     learnerUUID,
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

	// Check if raw note was modified while engine was running
	var currentVersion int32
	_ = s.pool.QueryRow(ctx, "SELECT raw_version FROM quick_notes WHERE id = $1 AND learner_id = $2", noteUUID, learnerUUID).Scan(&currentVersion)
	if currentVersion != note.RawVersion {
		return ErrStaleVersion
	}

	// 10. Persist enrichment and update status to enriched
	_, err = s.queries.UpsertQuickNoteEnrichment(ctx, db.UpsertQuickNoteEnrichmentParams{
		NoteID:    noteUUID,
		LearnerID: note.LearnerID,
		Summary:   resp.Summary,
		Tags:      resp.Tags,
		Links:     groundedLinks,
		Version:   note.RawVersion,
		Model:     "claude-haiku-4.5",
		Provider:  "anthropic",
	})
	if err != nil {
		return err
	}

	_, _ = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{
		ID:            noteUUID,
		LearnerID:     learnerUUID,
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

// ResumePendingEnrichments finds pending notes for a learner (oldest first) and enriches them
// until credits are exhausted or all pending notes are processed.
func (s *Service) ResumePendingEnrichments(ctx context.Context, learnerID string) (int, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return 0, errors.New("invalid learner id")
	}

	processed := 0
	for {
		// Claim the oldest pending note atomically using FOR UPDATE SKIP LOCKED
		var noteID pgtype.UUID
		var rawVersion int32
		err := s.pool.QueryRow(ctx, `
			UPDATE quick_notes
			SET status = 'enriching', updated_at = now()
			WHERE id = (
				SELECT id
				FROM quick_notes
				WHERE learner_id = $1 AND status = 'enrichment_pending'
				ORDER BY created_at ASC
				LIMIT 1
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, raw_version
		`, learnerUUID).Scan(&noteID, &rawVersion)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// No more pending notes
				break
			}
			return processed, err
		}

		noteIDStr := formatUUID(noteID)

		// Process enrichment
		enrichErr := s.ProcessEnrichmentWithVersion(ctx, learnerID, noteIDStr, int(rawVersion))
		if enrichErr != nil {
			if errors.Is(enrichErr, credits.ErrInsufficient) {
				// Reset back to enrichment_pending
				_, _ = s.pool.Exec(ctx, `
					UPDATE quick_notes
					SET status = 'enrichment_pending', updated_at = now()
					WHERE id = $1 AND learner_id = $2 AND status = 'enriching'
				`, noteID, learnerUUID)
				// Budget exhausted
				break
			}
			// Stale version or failure already handled in ProcessEnrichmentWithVersion
			continue
		}

		processed++
	}

	return processed, nil
}
