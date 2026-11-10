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
func (s *Service) ProcessEnrichmentWithVersion(ctx context.Context, learnerID, noteID string, expectedVersion int) (resultErr error) {
	noteUUID, err := parseUUID(noteID)
	if err != nil {
		return ErrNotFound
	}
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return ErrNotFound
	}
	note, err := s.queries.GetQuickNoteWithEnrichmentByID(ctx, db.GetQuickNoteWithEnrichmentByIDParams{ID: noteUUID, LearnerID: learnerUUID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err = ValidateExpectedVersion(int(note.RawVersion), expectedVersion); err != nil {
		return err
	}
	if note.Status == "raw_only" || note.Status == "enriched" {
		return nil
	}
	if s.credits == nil {
		return errors.New("credits service not configured")
	}
	quotes := pricing.NewQuotes(s.pool)
	quote, err := quotes.Create(ctx, learnerID, pricing.QuoteInput{Action: "quick_note"})
	if err != nil {
		return err
	}
	if err = quotes.Accept(ctx, learnerID, quote.ID); err != nil {
		return err
	}
	quoteUUID, err := parseUUID(quote.ID)
	if err != nil {
		return err
	}
	res, err := s.credits.ReserveAction(ctx, learnerID, quote.ID, "quick_note")
	if err != nil {
		return err
	}
	resUUID, err := parseUUID(res.ID)
	if err != nil {
		return err
	}
	delivered := false
	// Cleanup has its own bounded context so HTTP cancellation cannot strand a hold.
	defer func() {
		if delivered {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		// An explicit non-delivered attempt releases both queued and running work;
		// nil usage means unknown provider cost in the shared credits API.
		_, settleErr := s.credits.Settle(cleanupCtx, learnerID, res.ID, []*enginev1.Usage{{Provider: quote.Rate.Provider, Model: quote.Rate.Model, Attempt: 1, Component: "content"}})
		_, statusErr := s.pool.Exec(cleanupCtx, `UPDATE quick_notes SET status='enrichment_failed', reservation_id=NULL, quote_id=NULL, updated_at=now() WHERE id=$1 AND learner_id=$2 AND raw_version=$3 AND (reservation_id=$4 OR reservation_id IS NULL)`, noteUUID, learnerUUID, note.RawVersion, resUUID)
		resultErr = errors.Join(resultErr, settleErr, statusErr)
	}()
	if _, err = s.queries.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{ID: noteUUID, LearnerID: learnerUUID, Status: "enriching", ReservationID: resUUID, QuoteID: quoteUUID}); err != nil {
		return err
	}
	if err = s.credits.Claim(ctx, learnerID, res.ID); err != nil {
		return err
	}
	courseID := note.CourseID
	if !courseID.Valid && note.LessonID.Valid {
		courseID, err = s.queries.GetCourseIDForLesson(ctx, note.LessonID)
		if err != nil {
			return err
		}
	}
	if !courseID.Valid {
		err = s.pool.QueryRow(ctx, "SELECT c.id FROM courses c JOIN course_enrollments e ON e.course_id=c.id WHERE e.learner_id=$1 LIMIT 1", learnerUUID).Scan(&courseID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	var allowedLessons []*enginev1.LessonRef
	var allowedIDs []string
	if courseID.Valid {
		lessons, e := s.queries.ListCourseLessonsForCourse(ctx, courseID)
		if e != nil {
			return e
		}
		for _, lesson := range lessons {
			id := formatUUID(lesson.ID)
			allowedLessons = append(allowedLessons, &enginev1.LessonRef{LessonId: id, Title: lesson.Title})
			allowedIDs = append(allowedIDs, id)
		}
	}
	limits, err := s.credits.Limits(ctx, learnerID, res.ID, "quick_note")
	if err != nil {
		return err
	}
	if s.engineClient == nil {
		return errors.New("engine client not configured")
	}
	callCtx, cancel := context.WithDeadline(ctx, res.DeadlineAt)
	defer cancel()
	var lessonRef *enginev1.LessonRef
	if note.LessonID.Valid {
		lessonRef = &enginev1.LessonRef{LessonId: formatUUID(note.LessonID), Title: note.SlideTitle}
	}
	start := time.Now()
	response, err := s.engineClient.EnrichNote(callCtx, &enginev1.EnrichNoteRequest{Note: note.RawText, Lesson: lessonRef, Learner: &enginev1.LearnerProfile{}, Limits: limits, AllowedLessons: allowedLessons})
	if err != nil {
		return err
	}
	if response == nil {
		return errors.New("engine returned no enrichment")
	}
	if err = ValidateEnrichmentBounds(response.Summary, response.Tags); err != nil {
		return err
	}
	attempts := response.Attempts
	if len(attempts) == 0 && response.Usage != nil {
		attempts = []*enginev1.Usage{response.Usage}
	}
	// The financial transaction locks the note before testing the raw revision;
	// updates and the final status either commit with the charge or all roll back.
	committed := false
	_, err = s.credits.SettleWith(ctx, learnerID, res.ID, attempts, func(tx pgx.Tx, _ credits.Reservation) error {
		qtx := s.queries.WithTx(tx)
		current, e := qtx.GetQuickNoteForUpdate(ctx, db.GetQuickNoteForUpdateParams{ID: noteUUID, LearnerID: learnerUUID})
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if current.RawVersion != note.RawVersion || current.ReservationID != resUUID || current.Status != "enriching" {
			return ErrStaleVersion
		}
		model, provider := "", ""
		if response.Usage != nil {
			model, provider = response.Usage.Model, response.Usage.Provider
		}
		if _, e = qtx.UpsertQuickNoteEnrichment(ctx, db.UpsertQuickNoteEnrichmentParams{NoteID: noteUUID, LearnerID: learnerUUID, Summary: response.Summary, Tags: response.Tags, Links: FilterGroundedLinks(response.Links, allowedIDs), Version: note.RawVersion, Model: model, Provider: provider}); e != nil {
			return e
		}
		if _, e = qtx.UpdateQuickNoteStatus(ctx, db.UpdateQuickNoteStatusParams{ID: noteUUID, LearnerID: learnerUUID, Status: "enriched", ReservationID: resUUID, QuoteID: quoteUUID}); e != nil {
			return e
		}
		committed = true
		return nil
	})
	if err != nil {
		return err
	}
	if !committed {
		return credits.ErrReservation
	}
	delivered = true
	_ = EmitQuickNoteEnriched(s.events, learnerID, noteID, "web", "0.1.0")
	if s.events != nil {
		_ = s.events.Capture(learnerID, "$ai_generation", map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "feature": "quick_note", "succeeded": true, "$ai_is_error": false, "latency_ms": time.Since(start).Milliseconds()})
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
