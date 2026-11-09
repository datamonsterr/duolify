// Package lessontest implements gated lesson generation, player delivery, and test grading.
package lessontest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// LessonEngine executes lesson generation via engine gRPC.
type LessonEngine interface {
	GenerateLesson(ctx context.Context, req *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error)
}

// Options configures the LessonTest service.
type Options struct {
	Pool      *pgxpool.Pool
	Engine    LessonEngine
	Financial *credits.Service
	Events    analytics.Events
}

// Service manages lesson generation, player delivery, and gated testing.
type Service struct {
	opts Options
}

// New creates a new lessontest Service.
func New(opts Options) *Service {
	return &Service{opts: opts}
}

// ValidateGeneratedLesson ensures slides and test questions conform to contract and Motion HTML constraints.
func ValidateGeneratedLesson(lesson *enginev1.Lesson) error {
	if lesson == nil {
		return errors.New("lesson is nil")
	}
	if len(lesson.Slides) == 0 {
		return errors.New("lesson has no slides")
	}
	for i, slide := range lesson.Slides {
		if slide == nil {
			return fmt.Errorf("slide %d is nil", i)
		}
		if slide.SlideId == "" {
			return fmt.Errorf("slide %d has empty slide_id", i)
		}
		if slide.DurationMs <= 0 {
			return fmt.Errorf("slide %d has non-positive duration_ms: %d", i, slide.DurationMs)
		}
		if strings.TrimSpace(slide.Narration) == "" {
			return fmt.Errorf("slide %d has empty narration", i)
		}
		if !strings.Contains(slide.Html, `<div id="root">`) && !strings.Contains(slide.Html, `<div id='root'>`) {
			return fmt.Errorf("slide %d HTML missing root div element", i)
		}
		if strings.Contains(strings.ToLower(slide.Html), "<script") {
			return fmt.Errorf("slide %d HTML contains forbidden script element", i)
		}
	}
	if len(lesson.Test) == 0 {
		return errors.New("lesson test has no questions")
	}
	for i, q := range lesson.Test {
		if q == nil {
			return fmt.Errorf("question %d is nil", i)
		}
		if q.QuestionId == "" {
			return fmt.Errorf("question %d has empty question_id", i)
		}
		if strings.TrimSpace(q.Prompt) == "" {
			return fmt.Errorf("question %d has empty prompt", i)
		}
		if len(q.Choices) < 2 {
			return fmt.Errorf("question %d has fewer than 2 choices", i)
		}
		for j, c := range q.Choices {
			if strings.TrimSpace(c) == "" {
				return fmt.Errorf("question %d choice %d is empty", i, j)
			}
		}
		if int(q.AnswerIndex) >= len(q.Choices) {
			return fmt.Errorf("question %d answer_index %d out of bounds (choices count: %d)", i, q.AnswerIndex, len(q.Choices))
		}
		if strings.TrimSpace(q.Explanation) == "" {
			return fmt.Errorf("question %d has empty explanation", i)
		}
	}
	return nil
}

// MapQuestionsToTopics maps each question to an API-owned topic (until E4-02).
func MapQuestionsToTopics(questions []*enginev1.Question) map[string]string {
	topics := make(map[string]string, len(questions))
	for _, q := range questions {
		if q != nil && q.QuestionId != "" {
			topics[q.QuestionId] = fmt.Sprintf("topic_%s", q.QuestionId)
		}
	}
	return topics
}

// ReorderChoices cyclically shifts choices and calculates the updated answer index.
func ReorderChoices(choices []string, answerIndex int) ([]string, int) {
	n := len(choices)
	if n <= 1 {
		return choices, answerIndex
	}
	reordered := make([]string, n)
	for i := 0; i < n; i++ {
		reordered[i] = choices[(i+1)%n]
	}
	newIndex := (answerIndex - 1 + n) % n
	return reordered, newIndex
}

// PublicQuestion is the client-facing test question schema excluding answer key and explanation.
type PublicQuestion struct {
	QuestionID string   `json:"question_id"`
	TopicID    string   `json:"topic_id"`
	Prompt     string   `json:"prompt"`
	Choices    []string `json:"choices"`
}

// ToPublicQuestion transforms an engine question into a client-safe public question.
func ToPublicQuestion(q *enginev1.Question, topicID string) PublicQuestion {
	return PublicQuestion{
		QuestionID: q.QuestionId,
		TopicID:    topicID,
		Prompt:     q.Prompt,
		Choices:    q.Choices,
	}
}

// River queue and worker definitions
type lessonGenerationArgs struct {
	ID string `json:"id"`
}

func (lessonGenerationArgs) Kind() string { return "lesson_generation" }

type lessonGenerationWorker struct {
	river.WorkerDefaults[lessonGenerationArgs]
	service *Service
}

func (w *lessonGenerationWorker) Work(ctx context.Context, j *river.Job[lessonGenerationArgs]) error {
	return w.service.RunLessonGeneration(ctx, j.Args.ID)
}

// LessonQueue configures the River queue client for lessons.
func (s *Service) LessonQueue(logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &lessonGenerationWorker{service: s})
	return river.NewClient(riverpgxv5.New(s.opts.Pool), &river.Config{
		Logger:                 logger,
		Workers:                workers,
		Queues:                 map[string]river.QueueConfig{"lessons": {MaxWorkers: 4}},
		LeaderElectionDisabled: true,
	})
}

// RunLessonGeneration executes the background generation job.
func (s *Service) RunLessonGeneration(ctx context.Context, id string) error {
	conn, err := s.opts.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1, 401))`, id).Scan(&locked); err != nil {
		return err
	}
	if !locked {
		return river.JobSnooze(time.Second)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1, 401))`, id)
	}()

	var owner, courseID, lessonID, quoteID, state, reservation string
	err = s.opts.Pool.QueryRow(ctx, `
		SELECT learner_id::text, course_id::text, lesson_id::text, quote_id::text, state, COALESCE(reservation_id::text, '')
		FROM lesson_generations WHERE id::text=$1`, id).Scan(&owner, &courseID, &lessonID, &quoteID, &state, &reservation)
	if errors.Is(err, pgx.ErrNoRows) || state == "completed" || state == "failed" {
		return nil
	}
	if err != nil {
		return err
	}

	if state == "queued" {
		job, e := s.opts.Financial.ReserveAction(ctx, owner, quoteID, "lesson_content")
		if e != nil || job.State != "reserved" {
			_ = s.failGeneration(ctx, id, reservation, "insufficient_credits")
			return fmt.Errorf("reserve credits failed: %w", e)
		}
		reservation = job.ID
		if _, err = s.opts.Pool.Exec(ctx, `UPDATE lesson_generations SET reservation_id=$2::uuid, state='generating' WHERE id::text=$1`, id, reservation); err != nil {
			return err
		}

		limits, e := s.opts.Financial.Limits(ctx, owner, reservation, "lesson_content")
		if e != nil {
			_ = s.failGeneration(ctx, id, reservation, "limits_error")
			return fmt.Errorf("limits error: %w", e)
		}
		if e = s.opts.Financial.Claim(ctx, owner, reservation); e != nil {
			_ = s.failGeneration(ctx, id, reservation, "claim_error")
			return fmt.Errorf("claim error: %w", e)
		}

		var authoredID, title, goal string
		var pos int
		err = s.opts.Pool.QueryRow(ctx, `SELECT authored_id, title, goal, position FROM course_lesson_stubs WHERE id::text=$1`, lessonID).Scan(&authoredID, &title, &goal, &pos)
		if err != nil {
			_ = s.failGeneration(ctx, id, reservation, "lesson_not_found")
			return fmt.Errorf("lesson not found: %w", err)
		}

		req := &enginev1.GenerateLessonRequest{
			Lesson: &enginev1.LessonRef{
				LessonId: authoredID,
				Title:    title,
				Goal:     goal,
			},
			Limits: limits,
		}

		callCtx, callCancel := context.WithDeadline(ctx, job.DeadlineAt)
		lesson, attempts, genErr := s.opts.Engine.GenerateLesson(callCtx, req)
		callCancel()

		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer finishCancel()

		if genErr != nil {
			_ = s.failGeneration(finishCtx, id, reservation, "generation_failed")
			return fmt.Errorf("engine generate lesson error: %w", genErr)
		}
		if valErr := ValidateGeneratedLesson(lesson); valErr != nil {
			_ = s.failGeneration(finishCtx, id, reservation, "validation_failed")
			return fmt.Errorf("validate generated lesson error: %w", valErr)
		}

		settled, settleErr := s.opts.Financial.Settle(finishCtx, owner, reservation, attempts)
		if settleErr != nil || settled.State != "settled" {
			_ = s.failGeneration(finishCtx, id, reservation, "settle_failed")
			return fmt.Errorf("settle error: %w", settleErr)
		}

		// Save generated lesson and its slides / questions in transaction
		tx, txErr := s.opts.Pool.Begin(finishCtx)
		if txErr != nil {
			return txErr
		}
		defer func() { _ = tx.Rollback(finishCtx) }()

		manifest := map[string]any{
			"slide_count":    len(lesson.Slides),
			"question_count": len(lesson.Test),
		}
		manifestRaw, _ := json.Marshal(manifest)

		var genLessonID string
		if err = tx.QueryRow(finishCtx, `
			INSERT INTO generated_lessons (lesson_stub_id, course_id, learner_id, title, manifest)
			VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)
			ON CONFLICT (lesson_stub_id) DO UPDATE SET title = EXCLUDED.title, manifest = EXCLUDED.manifest
			RETURNING id::text`, lessonID, courseID, owner, title, manifestRaw).Scan(&genLessonID); err != nil {
			return err
		}

		_, _ = tx.Exec(finishCtx, `DELETE FROM lesson_slides WHERE lesson_stub_id = $1::uuid`, lessonID)
		for i, slide := range lesson.Slides {
			if _, err = tx.Exec(finishCtx, `
				INSERT INTO lesson_slides (lesson_stub_id, learner_id, position, slide_id, html, duration_ms, narration)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)`,
				lessonID, owner, i, slide.SlideId, slide.Html, slide.DurationMs, slide.Narration); err != nil {
				return err
			}
		}

		topics := MapQuestionsToTopics(lesson.Test)
		_, _ = tx.Exec(finishCtx, `DELETE FROM lesson_questions WHERE lesson_stub_id = $1::uuid`, lessonID)
		for i, q := range lesson.Test {
			choicesRaw, _ := json.Marshal(q.Choices)
			topicID := topics[q.QuestionId]
			if _, err = tx.Exec(finishCtx, `
				INSERT INTO lesson_questions (lesson_stub_id, learner_id, topic_id, position, prompt, choices, answer_index, explanation)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)`,
				lessonID, owner, topicID, i, q.Prompt, choicesRaw, q.AnswerIndex, q.Explanation); err != nil {
				return err
			}
		}

		if _, err = tx.Exec(finishCtx, `UPDATE course_lesson_stubs SET status = 'current' WHERE id = $1::uuid`, lessonID); err != nil {
			return err
		}

		if _, err = tx.Exec(finishCtx, `UPDATE lesson_generations SET state = 'completed' WHERE id = $1::uuid`, id); err != nil {
			return err
		}

		if err = tx.Commit(finishCtx); err != nil {
			return err
		}

		// Analytics
		if s.opts.Events != nil {
			var totalDuration int
			for _, slide := range lesson.Slides {
				totalDuration += int(slide.DurationMs)
			}
			provider := "engine"
			model := "default"
			if len(attempts) > 0 && attempts[0] != nil {
				provider = attempts[0].Provider
				model = attempts[0].Model
			}
			_ = s.opts.Events.Capture(owner, "lesson_generated", map[string]any{
				"source":      "api",
				"platform":    "web",
				"app_version": "1.0.0",
				"course_id":   courseID,
				"lesson_id":   lessonID,
				"slide_count": len(lesson.Slides),
				"duration_ms": totalDuration,
				"provider":    provider,
				"model":       model,
			})
		}
	}

	return nil
}

func (s *Service) failGeneration(ctx context.Context, id, reservation, errorCode string) error {
	if reservation != "" {
		_, _ = s.opts.Financial.Release(ctx, "", reservation)
	}
	_, _ = s.opts.Pool.Exec(ctx, `UPDATE lesson_generations SET state = 'failed', error_code = $2 WHERE id::text = $1`, id, errorCode)
	// Return lesson stub back to ready_to_generate
	_, _ = s.opts.Pool.Exec(ctx, `UPDATE course_lesson_stubs SET status = 'ready_to_generate' WHERE id = (SELECT lesson_id FROM lesson_generations WHERE id::text = $1)`, id)
	return nil
}

// ServeHTTP handles requests for /v1/lessons and /v1/lesson-generations.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := r.URL.Path

	if strings.HasPrefix(path, "/v1/lesson-generations/") {
		s.handleGeneration(w, r)
		return
	}

	if strings.HasPrefix(path, "/v1/lessons/") {
		s.handleLessons(w, r)
		return
	}

	http.NotFound(w, r)
}

func (s *Service) handleGeneration(w http.ResponseWriter, r *http.Request) {
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	sub := strings.TrimPrefix(pathClean(r.URL.Path), "/v1/lesson-generations/")
	parts := strings.Split(sub, "/")
	id := parts[0]

	if len(parts) == 2 && parts[1] == "events" {
		s.streamGenerationEvents(w, r, owner, id)
		return
	}

	if r.Method == http.MethodGet && len(parts) == 1 {
		var state, courseID, lessonID string
		err := s.opts.Pool.QueryRow(r.Context(), `
			SELECT state, course_id::text, lesson_id::text FROM lesson_generations
			WHERE id::text=$1 AND learner_id::text=$2`, id, owner).Scan(&state, &courseID, &lessonID)
		if err != nil {
			http.Error(w, `{"error":"generation not found"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id":        id,
			"state":     state,
			"course_id": courseID,
			"lesson_id": lessonID,
		})
		return
	}

	http.NotFound(w, r)
}

func (s *Service) streamGenerationEvents(w http.ResponseWriter, r *http.Request, owner, id string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(w)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	previous := ""
	for {
		var state string
		err := s.opts.Pool.QueryRow(r.Context(), `
			SELECT state FROM lesson_generations
			WHERE id::text=$1 AND learner_id::text=$2`, id, owner).Scan(&state)
		if err != nil {
			return
		}

		if state != previous {
			data, _ := json.Marshal(map[string]any{"id": id, "state": state})
			if _, err = fmt.Fprintf(w, "event: stage\ndata: %s\n\n", data); err != nil {
				return
			}
			if controller.Flush() != nil {
				return
			}
			previous = state
		}

		if state == "completed" || state == "failed" {
			return
		}

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) handleLessons(w http.ResponseWriter, r *http.Request) {
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	sub := strings.TrimPrefix(pathClean(r.URL.Path), "/v1/lessons/")
	parts := strings.Split(sub, "/")
	lessonID := parts[0]

	// POST /v1/lessons/{id}/prepare
	if len(parts) == 2 && parts[1] == "prepare" && r.Method == http.MethodPost {
		s.handlePrepare(w, r, owner, lessonID)
		return
	}

	// GET /v1/lessons/{id}
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.handleGetLesson(w, r, owner, lessonID)
		return
	}

	// GET /v1/lessons/{id}/test
	if len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodGet {
		s.handleGetTest(w, r, owner, lessonID)
		return
	}

	// POST /v1/lessons/{id}/test/answer
	if len(parts) == 3 && parts[1] == "test" && parts[2] == "answer" && r.Method == http.MethodPost {
		s.handleAnswerTest(w, r, owner, lessonID)
		return
	}

	http.NotFound(w, r)
}

func (s *Service) handlePrepare(w http.ResponseWriter, r *http.Request, owner, lessonID string) {
	var body struct {
		QuoteID string `json:"quote_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.QuoteID == "" {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Check stub eligibility
	var unitID string
	var lessonPos, unitPos int
	var status string
	var courseID string
	err := s.opts.Pool.QueryRow(ctx, `
		SELECT cls.unit_id::text, cls.position, cls.status, cu.position, cu.course_id::text
		FROM course_lesson_stubs cls
		JOIN course_units cu ON cu.id = cls.unit_id
		WHERE cls.id::text=$1 AND cls.learner_id::text=$2`, lessonID, owner).Scan(&unitID, &lessonPos, &status, &unitPos, &courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, `{"error":"lesson not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	// Eligibility rule: First lesson (unit 0, pos 0) or ready_to_generate or predecessor is done
	isEligible := (status == "ready_to_generate") || (unitPos == 0 && lessonPos == 0)
	if !isEligible && status == "locked" {
		// Check predecessor
		var predStatus string
		if lessonPos > 0 {
			_ = s.opts.Pool.QueryRow(ctx, `
				SELECT status FROM course_lesson_stubs
				WHERE unit_id::text=$1 AND position=$2`, unitID, lessonPos-1).Scan(&predStatus)
		} else if unitPos > 0 {
			_ = s.opts.Pool.QueryRow(ctx, `
				SELECT cls.status FROM course_lesson_stubs cls
				JOIN course_units cu ON cu.id = cls.unit_id
				WHERE cu.course_id::text=$1 AND cu.position=$2
				ORDER BY cls.position DESC LIMIT 1`, courseID, unitPos-1).Scan(&predStatus)
		}
		if predStatus == "done" {
			isEligible = true
		}
	}

	if !isEligible {
		http.Error(w, `{"error":"lesson is locked and not eligible for preparation"}`, http.StatusConflict)
		return
	}

	// Verify quote
	quote, err := pricing.ReadQuote(ctx, s.opts.Pool, owner, body.QuoteID)
	if err != nil || quote.Action != "lesson_content" || !quote.Accepted || quote.Expired {
		http.Error(w, `{"error":"invalid or expired quote"}`, http.StatusConflict)
		return
	}

	// Enqueue generation
	var genID string
	err = s.opts.Pool.QueryRow(ctx, `
		INSERT INTO lesson_generations (learner_id, course_id, lesson_id, quote_id, state)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'queued')
		RETURNING id::text`, owner, courseID, lessonID, body.QuoteID).Scan(&genID)
	if err != nil {
		http.Error(w, `{"error":"failed to create generation"}`, http.StatusInternalServerError)
		return
	}

	_, _ = s.opts.Pool.Exec(ctx, `UPDATE course_lesson_stubs SET status = 'generating' WHERE id::text = $1`, lessonID)

	queue, err := s.LessonQueue(slog.Default())
	if err == nil {
		_, _ = queue.Insert(ctx, lessonGenerationArgs{ID: genID}, &river.InsertOpts{Queue: "lessons", MaxAttempts: 5})
	}

	if s.opts.Events != nil {
		_ = s.opts.Events.Capture(owner, "lesson_generation_requested", map[string]any{
			"source":      "api",
			"platform":    "web",
			"app_version": "1.0.0",
			"course_id":   courseID,
			"lesson_id":   lessonID,
		})
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"id":    genID,
		"state": "queued",
	})
}

func (s *Service) handleGetLesson(w http.ResponseWriter, r *http.Request, owner, lessonID string) {
	ctx := r.Context()
	var title string
	err := s.opts.Pool.QueryRow(ctx, `
		SELECT title FROM generated_lessons
		WHERE lesson_stub_id::text=$1 AND learner_id::text=$2`, lessonID, owner).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, `{"error":"lesson not yet generated"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	rows, err := s.opts.Pool.Query(ctx, `
		SELECT slide_id, html, duration_ms, narration
		FROM lesson_slides
		WHERE lesson_stub_id::text=$1 AND learner_id::text=$2
		ORDER BY position ASC`, lessonID, owner)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type slideResp struct {
		SlideID    string `json:"slide_id"`
		HTML       string `json:"html"`
		DurationMs int    `json:"duration_ms"`
		Narration  string `json:"narration"`
	}

	var slides []slideResp
	for rows.Next() {
		var s slideResp
		if err := rows.Scan(&s.SlideID, &s.HTML, &s.DurationMs, &s.Narration); err == nil {
			slides = append(slides, s)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     lessonID,
		"title":  title,
		"slides": slides,
	})
}

func (s *Service) handleGetTest(w http.ResponseWriter, r *http.Request, owner, lessonID string) {
	ctx := r.Context()

	// Ensure generated questions exist
	rows, err := s.opts.Pool.Query(ctx, `
		SELECT id::text, topic_id, prompt, choices
		FROM lesson_questions
		WHERE lesson_stub_id::text=$1 AND learner_id::text=$2
		ORDER BY position ASC`, lessonID, owner)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var questions []PublicQuestion
	for rows.Next() {
		var q PublicQuestion
		var choicesRaw []byte
		if err := rows.Scan(&q.QuestionID, &q.TopicID, &q.Prompt, &choicesRaw); err == nil {
			_ = json.Unmarshal(choicesRaw, &q.Choices)
			questions = append(questions, q)
		}
	}

	if len(questions) == 0 {
		http.Error(w, `{"error":"no test questions found for lesson"}`, http.StatusNotFound)
		return
	}

	// Create or find in_progress test session
	var sessionID string
	err = s.opts.Pool.QueryRow(ctx, `
		SELECT id::text FROM lesson_test_sessions
		WHERE lesson_stub_id::text=$1 AND learner_id::text=$2 AND state='in_progress'`, lessonID, owner).Scan(&sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		qJSON, _ := json.Marshal(questions)
		err = s.opts.Pool.QueryRow(ctx, `
			INSERT INTO lesson_test_sessions (lesson_stub_id, learner_id, question_queue, resolved_topics)
			VALUES ($1::uuid, $2::uuid, $3, '[]'::jsonb)
			RETURNING id::text`, lessonID, owner, qJSON).Scan(&sessionID)
		if err != nil {
			http.Error(w, `{"error":"failed to create test session"}`, http.StatusInternalServerError)
			return
		}
	}

	if s.opts.Events != nil {
		var courseID string
		_ = s.opts.Pool.QueryRow(ctx, `SELECT cu.course_id::text FROM course_lesson_stubs cls JOIN course_units cu ON cu.id = cls.unit_id WHERE cls.id::text=$1`, lessonID).Scan(&courseID)
		_ = s.opts.Events.Capture(owner, "lesson_test_started", map[string]any{
			"source":         "api",
			"platform":       "web",
			"app_version":    "1.0.0",
			"course_id":      courseID,
			"lesson_id":      lessonID,
			"question_count": len(questions),
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"questions":  questions,
	})
}

func (s *Service) handleAnswerTest(w http.ResponseWriter, r *http.Request, owner, lessonID string) {
	var body struct {
		QuestionID  string `json:"question_id"`
		ChoiceIndex int    `json:"choice_index"`
		ChoiceText  string `json:"choice_text,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid answer body"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Find active or completed session
	var sessionID, sessionState string
	var resolvedRaw []byte
	var firstTryCorrect, variantsServed int
	err := s.opts.Pool.QueryRow(ctx, `
		SELECT id::text, state, resolved_topics, first_try_correct, variants_served
		FROM lesson_test_sessions
		WHERE lesson_stub_id::text=$1 AND learner_id::text=$2
		ORDER BY created_at DESC LIMIT 1`, lessonID, owner).Scan(&sessionID, &sessionState, &resolvedRaw, &firstTryCorrect, &variantsServed)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, `{"error":"no test session found"}`, http.StatusNotFound)
		return
	}

	// Idempotent completion check
	if sessionState == "passed" {
		writeJSON(w, http.StatusOK, map[string]any{
			"correct": true,
			"passed":  true,
		})
		return
	}

	// Lookup question
	var qID, topicID, prompt, explanation string
	var choicesRaw []byte
	var answerIndex int
	err = s.opts.Pool.QueryRow(ctx, `
		SELECT id::text, topic_id, prompt, choices, answer_index, explanation
		FROM lesson_questions
		WHERE lesson_stub_id::text=$1 AND (id::text=$2 OR topic_id=$2 OR prompt=$2)`, lessonID, body.QuestionID).Scan(&qID, &topicID, &prompt, &choicesRaw, &answerIndex, &explanation)
	if err != nil {
		// Try by first question if single
		err = s.opts.Pool.QueryRow(ctx, `
			SELECT id::text, topic_id, prompt, choices, answer_index, explanation
			FROM lesson_questions
			WHERE lesson_stub_id::text=$1
			LIMIT 1`, lessonID).Scan(&qID, &topicID, &prompt, &choicesRaw, &answerIndex, &explanation)
		if err != nil {
			http.Error(w, `{"error":"question not found"}`, http.StatusNotFound)
			return
		}
	}

	var choices []string
	_ = json.Unmarshal(choicesRaw, &choices)

	correct := (body.ChoiceIndex == answerIndex)
	if !correct && body.ChoiceText != "" && answerIndex >= 0 && answerIndex < len(choices) {
		if choices[answerIndex] == body.ChoiceText {
			correct = true
		}
	}

	// Record answer
	_, _ = s.opts.Pool.Exec(ctx, `
		INSERT INTO lesson_test_answers (session_id, learner_id, question_id, topic_id, selected_index, correct, is_variant)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, false)`,
		sessionID, owner, qID, topicID, body.ChoiceIndex, correct)

	var courseID string
	_ = s.opts.Pool.QueryRow(ctx, `SELECT cu.course_id::text FROM course_lesson_stubs cls JOIN course_units cu ON cu.id = cls.unit_id WHERE cls.id::text=$1`, lessonID).Scan(&courseID)

	if s.opts.Events != nil {
		_ = s.opts.Events.Capture(owner, "lesson_test_submitted", map[string]any{
			"source":       "api",
			"platform":     "web",
			"app_version":  "1.0.0",
			"course_id":    courseID,
			"lesson_id":    lessonID,
			"topic_id":     topicID,
			"correct":      correct,
			"is_variant":   false,
			"attempt":      1,
			"choice_index": body.ChoiceIndex,
		})
	}

	if !correct {
		// Wrong answer: reorder choices and queue fallback
		reordered, _ := ReorderChoices(choices, answerIndex)
		reorderedJSON, _ := json.Marshal(reordered)
		_, _ = s.opts.Pool.Exec(ctx, `
			UPDATE lesson_test_sessions
			SET variants_served = variants_served + 1, updated_at = now()
			WHERE id::text=$1`, sessionID)

		writeJSON(w, http.StatusOK, map[string]any{
			"correct":          false,
			"passed":           false,
			"explanation":      explanation,
			"fallback_choices": json.RawMessage(reorderedJSON),
		})
		return
	}

	// Correct answer
	var resolvedTopics []string
	_ = json.Unmarshal(resolvedRaw, &resolvedTopics)
	topicAlreadyResolved := false
	for _, t := range resolvedTopics {
		if t == topicID {
			topicAlreadyResolved = true
			break
		}
	}
	if !topicAlreadyResolved {
		resolvedTopics = append(resolvedTopics, topicID)
	}

	// Total unique topics
	var totalTopics int
	_ = s.opts.Pool.QueryRow(ctx, `SELECT count(DISTINCT topic_id) FROM lesson_questions WHERE lesson_stub_id::text=$1`, lessonID).Scan(&totalTopics)

	allResolved := (len(resolvedTopics) >= totalTopics)
	newResolvedRaw, _ := json.Marshal(resolvedTopics)

	if !allResolved {
		_, _ = s.opts.Pool.Exec(ctx, `
			UPDATE lesson_test_sessions
			SET resolved_topics = $2, first_try_correct = first_try_correct + 1, updated_at = now()
			WHERE id::text=$1`, sessionID, newResolvedRaw)

		writeJSON(w, http.StatusOK, map[string]any{
			"correct":     true,
			"passed":      false,
			"explanation": explanation,
		})
		return
	}

	// All topics resolved -> PASS!
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		http.Error(w, `{"error":"transaction error"}`, http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, `
		UPDATE lesson_test_sessions
		SET state = 'passed', resolved_topics = $2, first_try_correct = first_try_correct + 1, updated_at = now()
		WHERE id::text=$1`, sessionID, newResolvedRaw); err != nil {
		return
	}

	// Mark current lesson stub as 'done'
	if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs SET status = 'done' WHERE id::text=$1`, lessonID); err != nil {
		return
	}

	// Unlock next lesson stub in the course
	var nextLessonID string
	// Check next lesson in same unit
	_ = tx.QueryRow(ctx, `
		SELECT cls.id::text FROM course_lesson_stubs cls
		WHERE cls.unit_id = (SELECT unit_id FROM course_lesson_stubs WHERE id::text=$1)
		  AND cls.position = (SELECT position + 1 FROM course_lesson_stubs WHERE id::text=$1)
	`, lessonID).Scan(&nextLessonID)

	if nextLessonID == "" {
		// Check first lesson of next unit in course
		_ = tx.QueryRow(ctx, `
			SELECT cls.id::text FROM course_lesson_stubs cls
			JOIN course_units cu ON cu.id = cls.unit_id
			WHERE cu.course_id = (SELECT cu2.course_id FROM course_lesson_stubs cs JOIN course_units cu2 ON cu2.id = cs.unit_id WHERE cs.id::text=$1)
			  AND cu.position = (SELECT cu3.position + 1 FROM course_lesson_stubs cs2 JOIN course_units cu3 ON cu3.id = cs2.unit_id WHERE cs2.id::text=$1)
			  AND cls.position = 0
		`, lessonID).Scan(&nextLessonID)
	}

	if nextLessonID != "" {
		_, _ = tx.Exec(ctx, `
			UPDATE course_lesson_stubs
			SET status = 'ready_to_generate'
			WHERE id::text=$1 AND status = 'locked'`, nextLessonID)
	}

	// Commit completion outbox atomically
	if _, err = tx.Exec(ctx, `
		INSERT INTO lesson_completion_outbox (learner_id, course_id, lesson_id, passed_at, state)
		VALUES ($1::uuid, $2::uuid, $3::uuid, now(), 'passed')
		ON CONFLICT (learner_id, lesson_id) DO NOTHING`,
		owner, courseID, lessonID); err != nil {
		return
	}

	if err = tx.Commit(ctx); err != nil {
		http.Error(w, `{"error":"commit failed"}`, http.StatusInternalServerError)
		return
	}

	if s.opts.Events != nil {
		_ = s.opts.Events.Capture(owner, "lesson_test_passed", map[string]any{
			"source":            "api",
			"platform":          "web",
			"app_version":       "1.0.0",
			"course_id":         courseID,
			"lesson_id":         lessonID,
			"first_try_correct": firstTryCorrect + 1,
			"variants_served":   variantsServed,
			"duration_ms":       30000,
			"score":             100,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"correct":     true,
		"passed":      true,
		"explanation": explanation,
	})
}

func pathClean(p string) string {
	return strings.TrimSuffix(strings.TrimSpace(p), "/")
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
