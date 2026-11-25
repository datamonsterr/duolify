package chat

import (
	"context"
	"errors"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/jackc/pgx/v5"
)

// assessment carries the active Lesson test's private answer key and the source of the hint count.
type assessment struct {
	sessionID, lessonID string
	questions           []*enginev1.GuardedQuestion
}

// The queue contains aliases and public choices, while the authoritative answer index comes
// from either the stored base question or that session's immutable served retry snapshot.
// Include unresolved base questions that were replaced by retries as well. Resolve the active
// assessment even at Course level so omitting the optional lesson reference cannot bypass it.
func (s *Service) assessment(ctx context.Context, owner, course string, lesson *lessonInput) (assessment, error) {
	var out assessment
	var lessonID, target string
	if lesson != nil {
		lessonID, target = lesson.LessonID, lesson.QuestionID
	}
	rows, err := s.opts.Pool.Query(ctx, `
		WITH active AS (
			SELECT s.id,s.lesson_stub_id,s.question_queue,s.resolved_topics
			FROM lesson_test_sessions s
			JOIN course_lesson_stubs l ON l.id=s.lesson_stub_id AND l.learner_id=s.learner_id
			JOIN course_units u ON u.id=l.unit_id AND u.learner_id=s.learner_id
			WHERE s.learner_id=$1::uuid AND u.course_id=$2::uuid AND s.state='in_progress'
			ORDER BY (s.lesson_stub_id=NULLIF($3,'')::uuid) DESC,s.updated_at DESC,s.id DESC LIMIT 1
		), queued AS (
			SELECT a.id,a.lesson_stub_id, q.value->>'question_id' AS question_id,
				CASE WHEN q.value->>'is_variant'='true' THEN v.topic_id ELSE b.topic_id END AS topic_id,
				CASE WHEN q.value->>'is_variant'='true' THEN v.prompt ELSE b.prompt END AS prompt,
				CASE WHEN q.value->>'is_variant'='true' THEN v.choices->>v.answer_index ELSE b.choices->>b.answer_index END AS answer,
				b.id AS source_question_id, q.ordinality AS ordering
			FROM active a CROSS JOIN LATERAL jsonb_array_elements(
				CASE WHEN jsonb_typeof(a.question_queue)='array' THEN a.question_queue ELSE '[]'::jsonb END
			) WITH ORDINALITY AS q(value, ordinality)
			LEFT JOIN lesson_questions b ON b.id=CASE
				WHEN q.value->>'is_variant' IS DISTINCT FROM 'true' AND pg_input_is_valid(COALESCE(q.value->>'source_id',q.value->>'question_id'),'uuid')
				THEN COALESCE(q.value->>'source_id',q.value->>'question_id')::uuid END
				AND b.lesson_stub_id=a.lesson_stub_id AND b.learner_id=$1::uuid
			LEFT JOIN lesson_test_variant_snapshots v ON v.session_id=a.id AND v.lesson_stub_id=a.lesson_stub_id
				AND v.learner_id=$1::uuid AND v.question_id=q.value->>'question_id'
			WHERE NOT (a.resolved_topics ? COALESCE(q.value->>'original_id',q.value->>'source_id',q.value->>'question_id',''))
				AND NOT EXISTS (SELECT 1 FROM lesson_test_answers r WHERE r.session_id=a.id AND r.learner_id=$1::uuid AND r.question_id=q.value->>'question_id' AND r.correct)
		), unanswered AS (
			SELECT id,lesson_stub_id,question_id,topic_id,prompt,answer,ordering FROM queued
			UNION ALL
			SELECT a.id,a.lesson_stub_id,b.id::text,b.topic_id,b.prompt,b.choices->>b.answer_index,1000000+b.position
			FROM active a JOIN lesson_questions b ON b.lesson_stub_id=a.lesson_stub_id AND b.learner_id=$1::uuid
			WHERE NOT (a.resolved_topics ? b.id::text)
				AND NOT EXISTS (SELECT 1 FROM queued q WHERE q.source_question_id=b.id)
		)
		SELECT id,lesson_stub_id,question_id,topic_id,prompt,answer
		FROM unanswered WHERE question_id IS NOT NULL AND answer IS NOT NULL
		ORDER BY (question_id=$4) DESC,ordering`, owner, course, lessonID, target)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		q := new(enginev1.GuardedQuestion)
		if err := rows.Scan(&out.sessionID, &out.lessonID, &q.QuestionId, &q.TopicId, &q.Prompt, &q.CorrectChoice); err != nil {
			return out, err
		}
		out.questions = append(out.questions, q)
	}
	return out, rows.Err()
}

// claimHintStage serializes claims for the active assessment across API instances. It uses the
// pending tutor turn, so a failed claim is discarded with that turn and no text is kept here.
func (s *Service) claimHintStage(ctx context.Context, owner, turn string, a assessment) (uint32, error) {
	if len(a.questions) == 0 {
		return 0, nil
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked int
	if err = tx.QueryRow(ctx, `SELECT 1 FROM lesson_test_sessions WHERE id=$1::uuid AND learner_id=$2::uuid FOR UPDATE`, a.sessionID, owner).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	question := a.questions[0].GetQuestionId()
	var previous int64
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tutor_turns
		WHERE learner_id=$1::uuid AND guarded_question_id=$2 AND state<>'released'
			AND (state<>'pending' OR created_at>now()-$3::interval)`, owner, question, stalePending.String()).Scan(&previous); err != nil {
		return 0, err
	}
	result, err := tx.Exec(ctx, `UPDATE tutor_turns SET guarded_question_id=$2 WHERE id=$1::uuid AND state='pending'`, turn, question)
	if err != nil {
		return 0, err
	}
	if result.RowsAffected() != 1 {
		return 0, errTurnLost
	}
	return u32(min(previous, 2)), tx.Commit(ctx)
}
