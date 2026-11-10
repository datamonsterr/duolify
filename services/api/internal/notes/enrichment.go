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
	if len(summary) > 500 {
		return errors.New("enrichment summary exceeds 500 characters")
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
	NoteID          string `json:"note_id" river:"unique"`
	LearnerID       string `json:"learner_id"`
	ExpectedVersion int    `json:"expected_version" river:"unique"`
}

// InsertOpts keeps retries and competing producers unique per raw revision.
func (NoteEnrichArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "notes", UniqueOpts: river.UniqueOpts{ByArgs: true}}
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
	if err == credits.ErrInsufficient || err == ErrStaleVersion {
		return nil
	}
	return err
}

// NotesQueue creates a River queue client for asynchronous note enrichment.
func (s *Service) NotesQueue(logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	s.queueMu.Lock()
	defer s.queueMu.Unlock()
	if s.riverClient != nil {
		return s.riverClient, nil
	}
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
	var claimID pgtype.UUID
	err = s.pool.QueryRow(ctx, `UPDATE quick_notes SET status='enriching',enrichment_claim_id=gen_random_uuid(),enrichment_lease_until=clock_timestamp()+interval '2 minutes',updated_at=now()
 WHERE id=$1 AND learner_id=$2 AND raw_version=$3 AND reservation_id IS NOT DISTINCT FROM $4
 AND (status IN ('enrichment_pending','enrichment_failed') OR (status='enriching' AND coalesce(enrichment_lease_until,updated_at+interval '2 minutes')<clock_timestamp()))
 RETURNING enrichment_claim_id`, noteUUID, learnerUUID, note.RawVersion, note.ReservationID).Scan(&claimID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	delivered := false
	failureReason := "enrichment_failed"
	var engineStarted time.Time
	var reservationID string
	var rate pricing.ModelRate
	recoveryReservationID, recoveryQuoteID := note.ReservationID, note.QuoteID
	preserveReservation := note.ReservationID.Valid
	// A claim covers failures before quoting or reserving as well as provider work.
	defer func() {
		if delivered {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
		defer cancel()
		var settleErr error
		if reservationID != "" {
			_, settleErr = s.credits.Settle(cleanupCtx, learnerID, reservationID, []*enginev1.Usage{{Provider: rate.Provider, Model: rate.Model, Attempt: 1, Component: "content"}})
			preserveReservation = settleErr != nil
		}
		status := "enrichment_failed"
		if errors.Is(resultErr, credits.ErrInsufficient) {
			status = "enrichment_pending"
		}
		_, statusErr := s.pool.Exec(cleanupCtx, `UPDATE quick_notes SET status=$4,reservation_id=CASE WHEN $5 THEN $6::uuid ELSE NULL END,quote_id=CASE WHEN $5 THEN $7::uuid ELSE NULL END,enrichment_claim_id=NULL,enrichment_lease_until=NULL,updated_at=now() WHERE id=$1 AND learner_id=$2 AND enrichment_claim_id=$3`, noteUUID, learnerUUID, claimID, status, preserveReservation, recoveryReservationID, recoveryQuoteID)
		if settleErr != nil || statusErr != nil {
			resultErr = errors.Join(resultErr, settleErr, statusErr)
		}
		meta := s.AnalyticsMetadata(cleanupCtx)
		_ = EmitQuickNoteEnrichmentFailed(s.events, learnerID, noteID, failureReason, meta.Platform, meta.AppVersion)
		if s.events != nil && !engineStarted.IsZero() {
			_ = s.events.Capture(learnerID, "$ai_generation", map[string]any{"source": "api", "platform": meta.Platform, "app_version": meta.AppVersion, "feature": "quick_note", "succeeded": false, "$ai_is_error": true, "error_code": failureReason, "latency_ms": time.Since(engineStarted).Milliseconds()})
		}
	}()
	if s.credits == nil {
		return errors.New("credits service not configured")
	}
	// Reclaiming a crashed worker first returns its outstanding reservation.
	if note.ReservationID.Valid {
		previous, err := pricing.NewQuotes(s.pool).Get(ctx, learnerID, formatUUID(note.QuoteID))
		if err != nil {
			return err
		}
		reservationID, rate = formatUUID(note.ReservationID), previous.Rate
		if _, err = s.credits.Settle(ctx, learnerID, reservationID, []*enginev1.Usage{{Provider: rate.Provider, Model: rate.Model, Attempt: 1, Component: "content"}}); err != nil {
			return err
		}
		reservationID = ""
		preserveReservation = false
	}
	courseID, err := s.authorizedNoteCourse(ctx, learnerUUID, note.CourseID, note.LessonID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(note.RawText) == "" {
		return ErrInvalidText
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
	reservationID, rate = res.ID, quote.Rate
	recoveryReservationID, recoveryQuoteID = resUUID, quoteUUID
	preserveReservation = true
	command, err := s.pool.Exec(ctx, `UPDATE quick_notes SET reservation_id=$4,quote_id=$5,enrichment_lease_until=$6 WHERE id=$1 AND learner_id=$2 AND enrichment_claim_id=$3 AND raw_version=$7 AND status='enriching'`, noteUUID, learnerUUID, claimID, resUUID, quoteUUID, res.DeadlineAt.Add(15*time.Second), note.RawVersion)
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return ErrStaleVersion
	}
	if err = s.credits.Claim(ctx, learnerID, res.ID); err != nil {
		failureReason = "claim_failed"
		return err
	}
	var allowedLessons []*enginev1.LessonRef
	var allowedIDs []string
	if courseID.Valid {
		lessons, e := s.queries.ListCourseLessonsForCourse(ctx, db.ListCourseLessonsForCourseParams{CourseID: courseID, LearnerID: learnerUUID})
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
		failureReason = "engine_unavailable"
		return errors.New("engine client not configured")
	}
	callCtx, cancel := context.WithDeadline(ctx, res.DeadlineAt)
	defer cancel()
	var lessonRef *enginev1.LessonRef
	if note.LessonID.Valid {
		lessonRef = &enginev1.LessonRef{LessonId: formatUUID(note.LessonID), Title: note.SlideTitle}
	}
	engineStarted = time.Now()
	response, err := s.engineClient.EnrichNote(callCtx, &enginev1.EnrichNoteRequest{Note: note.RawText, Lesson: lessonRef, Learner: &enginev1.LearnerProfile{}, Limits: limits, AllowedLessons: allowedLessons})
	if err != nil {
		failureReason = "engine_failure"
		return err
	}
	if response == nil {
		failureReason = "engine_failure"
		return errors.New("engine returned no enrichment")
	}
	if err = ValidateEnrichmentBounds(response.Summary, response.Tags); err != nil {
		failureReason = "bounds_exceeded"
		return err
	}
	attempts := response.Attempts
	if len(attempts) == 0 && response.Usage != nil {
		attempts = []*enginev1.Usage{response.Usage}
	}
	if len(attempts) == 0 {
		failureReason = "usage_unavailable"
		return credits.ErrUsage
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
		if current.RawVersion != note.RawVersion || current.ReservationID != resUUID || current.Status != "enriching" || current.EnrichmentClaimID != claimID || !current.EnrichmentLeaseUntil.Valid || !time.Now().Before(current.EnrichmentLeaseUntil.Time) {
			return ErrStaleVersion
		}
		model, provider := "", ""
		if response.Usage != nil {
			model, provider = response.Usage.Model, response.Usage.Provider
		}
		if model == "" && len(response.Attempts) > 0 {
			last := response.Attempts[len(response.Attempts)-1]
			model, provider = last.Model, last.Provider
		}
		if model == "" {
			model = rate.Model
		}
		if provider == "" {
			provider = rate.Provider
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
	meta := s.AnalyticsMetadata(ctx)
	_ = EmitQuickNoteEnriched(s.events, learnerID, noteID, meta.Platform, meta.AppVersion)
	if s.events != nil {
		_ = s.events.Capture(learnerID, "$ai_generation", map[string]any{"source": "api", "platform": meta.Platform, "app_version": meta.AppVersion, "feature": "quick_note", "succeeded": true, "$ai_is_error": false, "latency_ms": time.Since(engineStarted).Milliseconds()})
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
		var id pgtype.UUID
		var rawVersion int32
		err = s.pool.QueryRow(ctx, `SELECT id,raw_version FROM quick_notes WHERE learner_id=$1 AND (status='enrichment_pending' OR (status='enriching' AND coalesce(enrichment_lease_until,updated_at+interval '2 minutes')<clock_timestamp())) ORDER BY created_at,id LIMIT 1`, learnerUUID).Scan(&id, &rawVersion)
		if errors.Is(err, pgx.ErrNoRows) {
			return processed, nil
		}
		if err != nil {
			return processed, err
		}
		err = s.ProcessEnrichmentWithVersion(ctx, learnerID, formatUUID(id), int(rawVersion))
		if err == credits.ErrInsufficient {
			return processed, nil
		}
		if err != nil {
			return processed, err
		}
		processed++
	}
}
