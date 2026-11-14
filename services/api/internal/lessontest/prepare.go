package lessontest

import (
	"context"
	"errors"
	"log/slog"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/steering"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
)

// Prepare refusals. The HTTP handler and the Course copy flow (E15-04) map them to their own codes.
var (
	ErrQuoteInvalid         = errors.New("invalid or expired quote")
	ErrLessonNotFound       = errors.New("lesson not found")
	ErrNotReadyToGenerate   = errors.New("lesson not ready to generate")
	ErrPredecessorNotPassed = errors.New("predecessor lesson has not been passed")
	ErrGenerationInProgress = errors.New("lesson generation already in progress")
	ErrPrepareUnavailable   = errors.New("lesson preparation unavailable")
)

// Prepare queues the gated generation of one Lesson for owner: it checks the accepted
// lesson_content quote, the stub's eligibility and the predecessor's authoritative pass, then
// inserts the generation and its River job in one transaction. inTx, when set, runs in that
// same transaction with the generation id so callers can record their own rows atomically.
// It never calls the Engine and never reserves funds; the worker does both.
func (s *Service) Prepare(ctx context.Context, owner, lessonID, quoteID string, inTx func(pgx.Tx, string) error) (string, error) {
	quote, err := pricing.ReadQuote(ctx, s.opts.Pool, owner, quoteID)
	if err != nil || quote.Action != "lesson_content" || !quote.Accepted || quote.Expired {
		return "", ErrQuoteInvalid
	}

	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return "", errors.Join(ErrPrepareUnavailable, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var unitID, status, courseID string
	var lessonPos, unitPos int
	err = tx.QueryRow(ctx, `
		SELECT cls.unit_id::text, cls.position, cls.status, cu.position, cu.course_id::text
		FROM course_lesson_stubs cls
		JOIN course_units cu ON cu.id = cls.unit_id
		WHERE cls.id::text=$1 AND cls.learner_id::text=$2 FOR UPDATE OF cls`, lessonID, owner).Scan(&unitID, &lessonPos, &status, &unitPos, &courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLessonNotFound
	}
	if err != nil {
		return "", errors.Join(ErrPrepareUnavailable, err)
	}
	if status != "ready_to_generate" {
		return "", ErrNotReadyToGenerate
	}

	// Eligibility rule: First lesson (unit 0, pos 0) or predecessor has authoritative pass in outbox
	if unitPos != 0 || lessonPos != 0 {
		var predID, predStatus string
		if lessonPos > 0 {
			_ = tx.QueryRow(ctx, `
				SELECT id::text, status FROM course_lesson_stubs
				WHERE unit_id::text=$1 AND position=$2`, unitID, lessonPos-1).Scan(&predID, &predStatus)
		} else if unitPos > 0 {
			_ = tx.QueryRow(ctx, `
				SELECT cls.id::text, cls.status FROM course_lesson_stubs cls
				JOIN course_units cu ON cu.id = cls.unit_id
				WHERE cu.course_id::text=$1 AND cu.position=$2
				ORDER BY cls.position DESC LIMIT 1`, courseID, unitPos-1).Scan(&predID, &predStatus)
		}
		var outboxExists bool
		if predID != "" {
			_ = tx.QueryRow(ctx, `
				SELECT EXISTS(SELECT 1 FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid)`,
				owner, predID).Scan(&outboxExists)
		}
		if predStatus != "done" || !outboxExists {
			return "", ErrPredecessorNotPassed
		}
	}

	var genID string
	err = tx.QueryRow(ctx, `
		INSERT INTO lesson_generations (learner_id, course_id, lesson_id, quote_id, state)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'queued')
		ON CONFLICT (lesson_id) WHERE state IN ('queued', 'generating', 'validated') DO NOTHING
		RETURNING id::text`, owner, courseID, lessonID, quoteID).Scan(&genID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrGenerationInProgress
	}
	if err != nil {
		return "", errors.Join(ErrPrepareUnavailable, err)
	}

	if err = steering.PinTx(ctx, tx, owner, courseID, genID); err != nil {
		return "", errors.Join(ErrPrepareUnavailable, err)
	}

	// The Cast is pinned in the same transaction as the job and reservation intent,
	// so a rollback leaves nothing and later edits cannot change a running job.
	var pinned *characters.LessonCastSnapshot
	if s.opts.Casts != nil && characters.CustomCharactersEnabled() {
		cast, pinErr := s.opts.Casts.PinJobCastTx(ctx, tx, owner, courseID, genID)
		if pinErr != nil {
			return "", errors.Join(ErrPrepareUnavailable, pinErr)
		}
		pinned = &cast
	}

	if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs SET status='generating' WHERE id=$1::uuid`, lessonID); err != nil {
		return "", errors.Join(ErrPrepareUnavailable, err)
	}
	queue, err := s.LessonQueue(slog.Default())
	if err == nil {
		_, err = queue.InsertTx(ctx, tx, lessonGenerationArgs{ID: genID}, &river.InsertOpts{Queue: "lessons", MaxAttempts: 5})
	}
	if err != nil {
		return "", errors.Join(ErrPrepareUnavailable, err)
	}
	if inTx != nil {
		if err = inTx(tx, genID); err != nil {
			return "", err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return "", errors.Join(ErrPrepareUnavailable, err)
	}

	if s.opts.Events != nil {
		_ = s.opts.Events.Capture(owner, "lesson_generation_requested", map[string]any{
			"source": "api", "platform": "web", "app_version": "1.0.0",
			"course_id": courseID, "lesson_id": lessonID,
		})
		if pinned != nil {
			_ = s.opts.Events.Capture(owner, "lesson_cast_applied", map[string]any{
				"source": "api", "platform": "web", "app_version": "1.0.0",
				"course_id": courseID, "lesson_id": lessonID,
				"cast_source": pinned.CastSource, "cast_version": pinned.CastVersion,
				"speaker_count": len(pinned.Speakers), "fallback_used": pinned.FallbackUsed,
			})
		}
	}
	return genID, nil
}
