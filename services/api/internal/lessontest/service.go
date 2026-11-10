// Package lessontest implements gated lesson generation, player delivery, and test grading.
package lessontest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
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
	Pool                *pgxpool.Pool
	Engine              LessonEngine
	Financial           *credits.Service
	Events              analytics.Events
	MinGap              int
	MaxVariantsPerTopic int
	MaxVariantsPerTest  int
	SessionTTL          time.Duration
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
	taughtTopics := make(map[string]bool)
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
		if len(slide.TopicIds) == 0 {
			return fmt.Errorf("slide %d has no taught topic_ids", i)
		}
		for _, t := range slide.TopicIds {
			if strings.TrimSpace(t) != "" {
				taughtTopics[t] = true
			}
		}
	}
	if len(lesson.Test) == 0 {
		return errors.New("lesson test has no questions")
	}
	questionsByTopic := make(map[string]*enginev1.Question)
	for i, q := range lesson.Test {
		if q == nil {
			return fmt.Errorf("question %d is nil", i)
		}
		if q.QuestionId == "" {
			return fmt.Errorf("question %d has empty question_id", i)
		}
		if strings.TrimSpace(q.TopicId) == "" {
			return fmt.Errorf("question %d has empty topic_id", i)
		}
		if !taughtTopics[q.TopicId] {
			return fmt.Errorf("question %d topic_id %q is not taught by any slide", i, q.TopicId)
		}
		if strings.TrimSpace(q.Prompt) == "" {
			return fmt.Errorf("question %d has empty prompt", i)
		}
		if len(q.Choices) < 2 {
			return fmt.Errorf("question %d has fewer than 2 choices", i)
		}
		uniqueChoices := make(map[string]bool)
		for j, c := range q.Choices {
			trimmed := strings.TrimSpace(c)
			if trimmed == "" {
				return fmt.Errorf("question %d choice %d is empty", i, j)
			}
			norm := strings.ToLower(trimmed)
			if uniqueChoices[norm] {
				return fmt.Errorf("question %d choice %d is a duplicate: %q", i, j, c)
			}
			uniqueChoices[norm] = true
		}
		if int(q.AnswerIndex) >= len(q.Choices) {
			return fmt.Errorf("question %d answer_index %d out of bounds (choices count: %d)", i, q.AnswerIndex, len(q.Choices))
		}
		if strings.TrimSpace(q.Explanation) == "" {
			return fmt.Errorf("question %d has empty explanation", i)
		}
		questionsByTopic[q.TopicId] = q
	}

	// Validate variant pool if present
	for _, pool := range lesson.VariantPool {
		if pool == nil {
			continue
		}
		if !taughtTopics[pool.TopicId] {
			return fmt.Errorf("variant pool topic %q is not taught by any slide", pool.TopicId)
		}
		sourceQ := questionsByTopic[pool.TopicId]
		if sourceQ == nil {
			return fmt.Errorf("variant pool topic %q has no source question in test", pool.TopicId)
		}
		seenVariantPrompts := make(map[string]bool)
		for vi, v := range pool.Variants {
			if v == nil {
				return fmt.Errorf("variant %d for topic %q is nil", vi, pool.TopicId)
			}
			if strings.TrimSpace(v.Prompt) == "" {
				return fmt.Errorf("variant %d for topic %q has empty prompt", vi, pool.TopicId)
			}
			if strings.EqualFold(strings.TrimSpace(v.Prompt), strings.TrimSpace(sourceQ.Prompt)) {
				return fmt.Errorf("variant %d for topic %q prompt must differ from source question", vi, pool.TopicId)
			}
			normPrompt := strings.ToLower(strings.TrimSpace(v.Prompt))
			if seenVariantPrompts[normPrompt] {
				return fmt.Errorf("variant %d for topic %q prompt duplicate of earlier variant", vi, pool.TopicId)
			}
			seenVariantPrompts[normPrompt] = true

			if len(v.Choices) < 2 {
				return fmt.Errorf("variant %d for topic %q has fewer than 2 choices", vi, pool.TopicId)
			}
			vChoices := make(map[string]bool)
			for j, c := range v.Choices {
				trimmed := strings.TrimSpace(c)
				if trimmed == "" {
					return fmt.Errorf("variant %d choice %d is empty", vi, j)
				}
				norm := strings.ToLower(trimmed)
				if vChoices[norm] {
					return fmt.Errorf("variant %d choice %d duplicate: %q", vi, j, c)
				}
				vChoices[norm] = true
			}
			if int(v.AnswerIndex) >= len(v.Choices) {
				return fmt.Errorf("variant %d answer_index out of bounds", vi)
			}
			if strings.TrimSpace(v.Explanation) == "" {
				return fmt.Errorf("variant %d explanation is empty", vi)
			}
		}
	}
	return nil
}

// MapQuestionsToTopics maps each question to a topic (E4-01 fallback, replaced in E4-02 with engine-generated topic_ids).
func MapQuestionsToTopics(questions []*enginev1.Question) map[string]string {
	topics := make(map[string]string, len(questions))
	for _, q := range questions {
		if q != nil && q.QuestionId != "" {
			if q.TopicId != "" {
				topics[q.QuestionId] = q.TopicId
			} else {
				topics[q.QuestionId] = fmt.Sprintf("topic_%s", q.QuestionId)
			}
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

var opKeyRegex = regexp.MustCompile(`^[A-Za-z0-9_-]{1,100}$`)

// ValidateOperationKey checks that an answer operation key conforms to format requirements.
func ValidateOperationKey(key string) error {
	if !opKeyRegex.MatchString(key) {
		return errors.New("invalid operation key")
	}
	return nil
}

// ReorderQuestions permutes questions for a newly ordered session when abandoned.
func ReorderQuestions(questions []PublicQuestion) []PublicQuestion {
	n := len(questions)
	if n <= 1 {
		return questions
	}
	reordered := make([]PublicQuestion, n)
	for i := 0; i < n; i++ {
		reordered[i] = questions[(i+1)%n]
	}
	return reordered
}

// PublicQuestion is the client-facing test question schema excluding answer key and explanation.
type PublicQuestion struct {
	QuestionID string   `json:"question_id"`
	TopicID    string   `json:"topic_id"`
	Prompt     string   `json:"prompt"`
	Choices    []string `json:"choices"`
	VariantOf  string   `json:"variant_of,omitempty"`
	IsVariant  bool     `json:"is_variant,omitempty"`
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

// InsertSpacedVariant inserts a retry variant into the remaining questions queue
// after minGap questions when enough questions remain, or at the end if fewer remain.
func InsertSpacedVariant(remaining []PublicQuestion, variant PublicQuestion, minGap int) []PublicQuestion {
	if minGap <= 0 {
		minGap = 2
	}
	if len(remaining) == 0 {
		return []PublicQuestion{variant}
	}
	insertIdx := minGap
	if insertIdx > len(remaining) {
		insertIdx = len(remaining)
	}
	res := make([]PublicQuestion, 0, len(remaining)+1)
	res = append(res, remaining[:insertIdx]...)
	res = append(res, variant)
	res = append(res, remaining[insertIdx:]...)
	return res
}

// AnswerRecord records a single question topic and answer correctness.
type AnswerRecord struct {
	TopicID string
	Correct bool
}

// ExtractWeakTopics extracts unique topic IDs that were answered incorrectly, in order of first failure.
func ExtractWeakTopics(answers []AnswerRecord) []string {
	var weak []string
	seen := make(map[string]bool)
	for _, a := range answers {
		if !a.Correct && strings.TrimSpace(a.TopicID) != "" {
			if !seen[a.TopicID] {
				seen[a.TopicID] = true
				weak = append(weak, a.TopicID)
			}
		}
	}
	return weak
}

// FormatLessonSummary formats a completed lesson summary including weak topics.
func FormatLessonSummary(title string, weakTopics []string, firstTryCorrect, variantsServed int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Lesson: %s\n", title)
	fmt.Fprintf(&b, "Score: %d first-try correct, %d retry variants served.\n", firstTryCorrect, variantsServed)
	if len(weakTopics) > 0 {
		fmt.Fprintf(&b, "Weak topics: %s\n", strings.Join(weakTopics, ", "))
	}
	return b.String()
}

// LessonHistoryEntry represents a completed lesson summary item with completion timestamp.
type LessonHistoryEntry struct {
	LessonTitle   string
	CompletedUnix int64
	Summary       string
}

// OrderHistoryOldestFirst sorts lesson history items oldest first and returns their summary strings.
func OrderHistoryOldestFirst(items []LessonHistoryEntry) []string {
	sorted := make([]LessonHistoryEntry, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CompletedUnix < sorted[j].CompletedUnix
	})
	res := make([]string, len(sorted))
	for i, item := range sorted {
		res[i] = item.Summary
	}
	return res
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

	var owner, courseID, lessonID, quoteID, state, reservation, failureCode string
	var checkpoint []byte
	err = s.opts.Pool.QueryRow(ctx, `
		SELECT learner_id::text, course_id::text, lesson_id::text, quote_id::text, state, COALESCE(reservation_id::text, ''), COALESCE(error_code, ''), result_checkpoint
		FROM lesson_generations WHERE id::text=$1`, id).Scan(&owner, &courseID, &lessonID, &quoteID, &state, &reservation, &failureCode, &checkpoint)
	if errors.Is(err, pgx.ErrNoRows) || state == "completed" || state == "failed" {
		return nil
	}
	if err != nil {
		return err
	}

	fail := func(code string, cause error) error {
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		return errors.Join(cause, s.failGeneration(finishCtx, owner, id, reservation, code))
	}
	if failureCode != "" {
		return fail(failureCode, nil)
	}
	if len(checkpoint) > 0 {
		var saved generationCheckpoint
		if err := json.Unmarshal(checkpoint, &saved); err != nil {
			return fail("checkpoint_invalid", err)
		}
		if err := ValidateGeneratedLesson(saved.Lesson); err != nil {
			return fail("checkpoint_invalid", err)
		}
		return s.completeGeneration(ctx, owner, courseID, lessonID, id, reservation, saved)
	}
	// A process may have died after starting a provider. Never repeat an untracked
	// paid call: release customer funds and conservatively account for the held USD.
	if state == "generating" || state == "validated" {
		return fail("generation_interrupted", errors.New("generation interrupted before checkpoint"))
	}

	if state == "queued" {
		job, e := s.opts.Financial.ReserveAction(ctx, owner, quoteID, "lesson_content")
		if e != nil || job.State != "reserved" {
			return fail("insufficient_credits", fmt.Errorf("reserve credits failed: %w", e))
		}
		reservation = job.ID
		if _, err = s.opts.Pool.Exec(ctx, `UPDATE lesson_generations SET reservation_id=$2::uuid, state='generating' WHERE id::text=$1`, id, reservation); err != nil {
			return err
		}

		limits, e := s.opts.Financial.Limits(ctx, owner, reservation, "lesson_content")
		if e != nil {
			return fail("limits_error", fmt.Errorf("limits error: %w", e))
		}
		if e = s.opts.Financial.Claim(ctx, owner, reservation); e != nil {
			return fail("claim_error", fmt.Errorf("claim error: %w", e))
		}

		var authoredID, title, goal string
		var pos int
		err = s.opts.Pool.QueryRow(ctx, `SELECT authored_id, title, goal, position FROM course_lesson_stubs WHERE id::text=$1`, lessonID).Scan(&authoredID, &title, &goal, &pos)
		if err != nil {
			return fail("lesson_not_found", fmt.Errorf("lesson not found: %w", err))
		}

		// Build CourseOutline
		var courseTitle, courseDesc string
		_ = s.opts.Pool.QueryRow(ctx, `SELECT title, description FROM courses WHERE id::text=$1`, courseID).Scan(&courseTitle, &courseDesc)
		outline := &enginev1.CourseOutline{
			CourseId:    courseID,
			Title:       courseTitle,
			Description: courseDesc,
		}

		type unitInfo struct {
			id    string
			title string
		}
		var unitsList []unitInfo
		uRows, uErr := s.opts.Pool.Query(ctx, `SELECT id::text, title FROM course_units WHERE course_id::text=$1 ORDER BY position ASC`, courseID)
		if uErr == nil {
			for uRows.Next() {
				var ui unitInfo
				if uRows.Scan(&ui.id, &ui.title) == nil {
					unitsList = append(unitsList, ui)
				}
			}
			uRows.Close()
		}

		for _, ui := range unitsList {
			uProto := &enginev1.Unit{
				UnitId: ui.id,
				Title:  ui.title,
			}
			lRows, lErr := s.opts.Pool.Query(ctx, `SELECT authored_id, title, goal FROM course_lesson_stubs WHERE unit_id::text=$1 ORDER BY position ASC`, ui.id)
			if lErr == nil {
				for lRows.Next() {
					var lAuthID, lTitle, lGoal string
					if lRows.Scan(&lAuthID, &lTitle, &lGoal) == nil {
						uProto.Lessons = append(uProto.Lessons, &enginev1.LessonRef{
							LessonId: lAuthID,
							Title:    lTitle,
							Goal:     lGoal,
						})
					}
				}
				lRows.Close()
			}
			outline.Units = append(outline.Units, uProto)
		}

		// Build LearnerProfile
		learner := &enginev1.LearnerProfile{LearnerId: owner}
		var dailyM, dailyX int32
		var remEn bool
		_ = s.opts.Pool.QueryRow(ctx, `
			SELECT locale, background, goal, level, topic, time_zone,
			       COALESCE(daily_minutes,0), COALESCE(daily_xp,0), COALESCE(reminder_enabled,false), COALESCE(reminder_time,'')
			FROM learner_profiles WHERE learner_id::text=$1`, owner).Scan(
			&learner.Locale,
			&learner.Background,
			&learner.Goals,
			&learner.Level,
			&learner.Topic,
			&learner.TimeZone,
			&dailyM,
			&dailyX,
			&remEn,
			&learner.ReminderTime,
		)
		if dailyM > 0 {
			learner.DailyMinutes = uint32(dailyM)
		}
		if dailyX > 0 {
			learner.DailyXp = uint32(dailyX)
		}
		learner.ReminderEnabled = remEn

		var courseLevelOverride *string
		_ = s.opts.Pool.QueryRow(ctx, `SELECT course_level_override FROM courses WHERE id::text=$1 AND learner_id::text=$2`, courseID, owner).Scan(&courseLevelOverride)
		if courseLevelOverride != nil && *courseLevelOverride != "" {
			learner.Level = *courseLevelOverride
		}

		// Build History: completed lesson summaries oldest first
		var history []string
		hRows, hErr := s.opts.Pool.Query(ctx, `
			SELECT summary FROM lesson_completion_summaries
			WHERE learner_id::text=$1 AND course_id::text=$2
			ORDER BY completed_at ASC`, owner, courseID)
		if hErr == nil {
			for hRows.Next() {
				var sText string
				if hRows.Scan(&sText) == nil {
					history = append(history, sText)
				}
			}
			hRows.Close()
		}

		req := &enginev1.GenerateLessonRequest{
			Outline: outline,
			Lesson: &enginev1.LessonRef{
				LessonId: authoredID,
				Title:    title,
				Goal:     goal,
			},
			Learner: learner,
			History: history,
			Limits:  limits,
		}

		callCtx, callCancel := context.WithDeadline(ctx, job.DeadlineAt)
		lesson, attempts, genErr := s.opts.Engine.GenerateLesson(callCtx, req)
		callCancel()

		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer finishCancel()

		if genErr != nil {
			return fail("generation_failed", fmt.Errorf("engine generate lesson error: %w", genErr))
		}
		if valErr := ValidateGeneratedLesson(lesson); valErr != nil {
			return fail("validation_failed", fmt.Errorf("validate generated lesson error: %w", valErr))
		}

		availableTopics := make(map[string]bool)
		for _, pool := range lesson.VariantPool {
			if pool != nil && len(pool.Variants) > 0 {
				availableTopics[pool.TopicId] = true
			}
		}
		for _, question := range lesson.Test {
			if !availableTopics[question.TopicId] {
				return fail("validation_failed", errors.New("validated retry pool required"))
			}
		}

		saved := generationCheckpoint{Lesson: lesson, Attempts: attempts, Title: title}
		raw, marshalErr := json.Marshal(saved)
		if marshalErr != nil {
			return fail("checkpoint_failed", marshalErr)
		}
		if _, err = s.opts.Pool.Exec(finishCtx, `UPDATE lesson_generations SET result_checkpoint=$2, state='validated' WHERE id=$1::uuid`, id, raw); err != nil {
			return err
		}
		return s.completeGeneration(finishCtx, owner, courseID, lessonID, id, reservation, saved)
	}
	return nil
}

type generationCheckpoint struct {
	Lesson   *enginev1.Lesson  `json:"lesson"`
	Attempts []*enginev1.Usage `json:"attempts"`
	Title    string            `json:"title"`
}

func (s *Service) completeGeneration(ctx context.Context, owner, courseID, lessonID, id, reservation string, saved generationCheckpoint) error {
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	lesson, attempts, title := saved.Lesson, saved.Attempts, saved.Title
	var err error
	settled, settleErr := s.opts.Financial.SettleWith(finishCtx, owner, reservation, attempts, func(tx pgx.Tx, result credits.Reservation) error {
		if result.State != "settled" {
			return credits.ErrUsage
		}

		// Save generated lesson and its slides / questions in transaction
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

		if _, err = tx.Exec(finishCtx, `DELETE FROM lesson_slides WHERE lesson_stub_id = $1::uuid`, lessonID); err != nil {
			return err
		}
		for i, slide := range lesson.Slides {
			topicIDs := slide.TopicIds
			if _, err = tx.Exec(finishCtx, `
				INSERT INTO lesson_slides (lesson_stub_id, learner_id, position, slide_id, html, duration_ms, narration, topic_ids)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8)`,
				lessonID, owner, i, slide.SlideId, slide.Html, slide.DurationMs, slide.Narration, topicIDs); err != nil {
				return err
			}
		}

		if _, err = tx.Exec(finishCtx, `DELETE FROM lesson_questions WHERE lesson_stub_id = $1::uuid`, lessonID); err != nil {
			return err
		}
		for i, q := range lesson.Test {
			choicesRaw, _ := json.Marshal(q.Choices)
			topicID := q.TopicId
			if _, err = tx.Exec(finishCtx, `
				INSERT INTO lesson_questions (lesson_stub_id, learner_id, topic_id, position, prompt, choices, answer_index, explanation, variant_of)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9)`,
				lessonID, owner, topicID, i, q.Prompt, choicesRaw, q.AnswerIndex, q.Explanation, q.VariantOf); err != nil {
				return err
			}
		}

		// Save private retry pools in Postgres
		if _, err = tx.Exec(finishCtx, `DELETE FROM lesson_retry_pools WHERE lesson_stub_id = $1::uuid`, lessonID); err != nil {
			return err
		}
		poolsByTopic := make(map[string][]*enginev1.Question)
		for _, tp := range lesson.VariantPool {
			if tp != nil && tp.TopicId != "" {
				poolsByTopic[tp.TopicId] = tp.Variants
			}
		}

		for _, q := range lesson.Test {
			topicID := q.TopicId
			variants := poolsByTopic[topicID]
			if len(variants) == 0 {
				return errors.New("validated retry pool required")
			}

			variantsRaw, _ := json.Marshal(variants)
			if _, err = tx.Exec(finishCtx, `
				INSERT INTO lesson_retry_pools (lesson_stub_id, learner_id, topic_id, variants)
				VALUES ($1::uuid, $2::uuid, $3, $4)
				ON CONFLICT (lesson_stub_id, topic_id) DO UPDATE SET variants = EXCLUDED.variants`,
				lessonID, owner, topicID, variantsRaw); err != nil {
				return err
			}
		}

		if _, err = tx.Exec(finishCtx, `UPDATE course_lesson_stubs SET status = 'current' WHERE id = $1::uuid`, lessonID); err != nil {
			return err
		}

		if _, err = tx.Exec(finishCtx, `UPDATE lesson_generations SET state = 'completed', result_checkpoint=NULL WHERE id = $1::uuid`, id); err != nil {
			return err
		}

		return nil
	})
	if settleErr != nil {
		if errors.Is(settleErr, credits.ErrUsage) {
			return errors.Join(settleErr, s.failGeneration(finishCtx, owner, id, reservation, "settle_failed"))
		}
		return settleErr
	}
	if settled.State != "settled" {
		return s.failGeneration(finishCtx, owner, id, reservation, "settle_failed")
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

	return nil
}

func (s *Service) failGeneration(ctx context.Context, owner, id, reservation, errorCode string) error {
	// Persist cleanup intent before releasing, so a transient ledger failure is resumable.
	if _, err := s.opts.Pool.Exec(ctx, `UPDATE lesson_generations SET error_code=$2 WHERE id=$1::uuid`, id, errorCode); err != nil {
		return err
	}
	if reservation != "" {
		// Release only accepts queued reservations. Settle with no delivered usage also
		// releases running work, retaining its conservative operational cost hold.
		if _, err := s.opts.Financial.Settle(ctx, owner, reservation, nil); err != nil {
			return fmt.Errorf("release failed generation: %w", err)
		}
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE lesson_generations SET state='failed', result_checkpoint=NULL WHERE id=$1::uuid`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs SET status='ready_to_generate' WHERE id=(SELECT lesson_id FROM lesson_generations WHERE id=$1::uuid) AND status='generating'`, id); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if s.opts.Events != nil {
		var courseID, lessonID string
		if err = s.opts.Pool.QueryRow(ctx, `SELECT course_id::text,lesson_id::text FROM lesson_generations WHERE id=$1::uuid`, id).Scan(&courseID, &lessonID); err != nil {
			return err
		}
		_ = s.opts.Events.Capture(owner, "course_generation_failed", map[string]any{"source": "api", "platform": "web", "app_version": "development", "course_id": courseID, "lesson_id": lessonID, "error_code": errorCode})
	}
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

	// GET /v1/lessons/{id}/test/recap
	if len(parts) == 3 && parts[1] == "test" && parts[2] == "recap" && r.Method == http.MethodGet {
		s.handleRecap(w, r, owner, lessonID)
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
	// Verify quote
	quote, err := pricing.ReadQuote(ctx, s.opts.Pool, owner, body.QuoteID)
	if err != nil || quote.Action != "lesson_content" || !quote.Accepted || quote.Expired {
		http.Error(w, `{"error":"invalid or expired quote"}`, http.StatusConflict)
		return
	}

	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Check stub eligibility
	var unitID string
	var lessonPos, unitPos int
	var status string
	var courseID string
	err = tx.QueryRow(ctx, `
		SELECT cls.unit_id::text, cls.position, cls.status, cu.position, cu.course_id::text
		FROM course_lesson_stubs cls
		JOIN course_units cu ON cu.id = cls.unit_id
		WHERE cls.id::text=$1 AND cls.learner_id::text=$2 FOR UPDATE OF cls`, lessonID, owner).Scan(&unitID, &lessonPos, &status, &unitPos, &courseID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, `{"error":"lesson not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	if status != "ready_to_generate" {
		http.Error(w, `{"error":"lesson not ready to generate"}`, http.StatusConflict)
		return
	}

	// Eligibility rule: First lesson (unit 0, pos 0) or predecessor has authoritative pass in outbox
	isFirstLesson := (unitPos == 0 && lessonPos == 0)
	if !isFirstLesson {
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
			http.Error(w, `{"error":"predecessor lesson has not been passed"}`, http.StatusConflict)
			return
		}
	}

	// Enqueue generation
	var genID string
	err = tx.QueryRow(ctx, `
		INSERT INTO lesson_generations (learner_id, course_id, lesson_id, quote_id, state)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'queued')
		ON CONFLICT (lesson_id) WHERE state IN ('queued', 'generating', 'validated') DO NOTHING
 RETURNING id::text`, owner, courseID, lessonID, body.QuoteID).Scan(&genID)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, `{"error":"lesson generation already in progress"}`, http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"failed to create generation"}`, http.StatusInternalServerError)
		return
	}

	if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs SET status='generating' WHERE id=$1::uuid`, lessonID); err != nil {
		http.Error(w, `{"error":"database error"}`, 500)
		return
	}
	queue, err := s.LessonQueue(slog.Default())
	if err == nil {
		_, err = queue.InsertTx(ctx, tx, lessonGenerationArgs{ID: genID}, &river.InsertOpts{Queue: "lessons", MaxAttempts: 5})
	}
	if err != nil {
		http.Error(w, `{"error":"failed to enqueue generation"}`, 500)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		http.Error(w, `{"error":"database error"}`, 500)
		return
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

	minGap := s.opts.MinGap
	if minGap <= 0 {
		minGap = 2
	}
	maxTopicVariants := s.opts.MaxVariantsPerTopic
	if maxTopicVariants <= 0 {
		maxTopicVariants = 3
	}
	maxTestVariants := s.opts.MaxVariantsPerTest
	if maxTestVariants <= 0 {
		maxTestVariants = 12
	}

	var courseID string
	_ = s.opts.Pool.QueryRow(ctx, `SELECT cu.course_id::text FROM course_lesson_stubs cls JOIN course_units cu ON cu.id = cls.unit_id WHERE cls.id::text=$1`, lessonID).Scan(&courseID)

	var totalTopics int
	_ = s.opts.Pool.QueryRow(ctx, `SELECT count(DISTINCT topic_id) FROM lesson_questions WHERE lesson_stub_id::text=$1`, lessonID).Scan(&totalTopics)

	sessionTTL := s.opts.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 7 * 24 * time.Hour
	}

	var sessionID string
	var queueJSON, resolvedJSON []byte
	var createdAt, updatedAt time.Time
	err := s.opts.Pool.QueryRow(ctx, `
		SELECT id::text, question_queue, resolved_topics, created_at, updated_at
		FROM lesson_test_sessions
		WHERE lesson_stub_id::text=$1 AND learner_id::text=$2 AND state='in_progress'
		ORDER BY created_at DESC LIMIT 1`, lessonID, owner).Scan(&sessionID, &queueJSON, &resolvedJSON, &createdAt, &updatedAt)

	var questions []PublicQuestion
	var resolvedTopics []string
	isExpired := false

	if err == nil {
		if time.Since(updatedAt) > sessionTTL || time.Since(createdAt) > sessionTTL {
			isExpired = true
			_, _ = s.opts.Pool.Exec(ctx, `UPDATE lesson_test_sessions SET state='abandoned', updated_at=now() WHERE id::text=$1`, sessionID)
		} else {
			_ = json.Unmarshal(queueJSON, &questions)
			_ = json.Unmarshal(resolvedJSON, &resolvedTopics)
		}
	}

	if isExpired || errors.Is(err, pgx.ErrNoRows) {
		// Ensure generated questions exist
		rows, qErr := s.opts.Pool.Query(ctx, `
			SELECT id::text, topic_id, prompt, choices, position
			FROM lesson_questions
			WHERE lesson_stub_id::text=$1 AND learner_id::text=$2
			ORDER BY position ASC`, lessonID, owner)
		if qErr != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var dbID, topicID, prompt string
			var choicesRaw []byte
			var position int
			if err := rows.Scan(&dbID, &topicID, &prompt, &choicesRaw, &position); err == nil {
				var choices []string
				_ = json.Unmarshal(choicesRaw, &choices)
				questions = append(questions, PublicQuestion{
					QuestionID: fmt.Sprintf("q%d", position+1),
					TopicID:    topicID,
					Prompt:     prompt,
					Choices:    choices,
				})
			}
		}

		if len(questions) == 0 {
			http.Error(w, `{"error":"no test questions found for lesson"}`, http.StatusNotFound)
			return
		}

		if isExpired {
			questions = ReorderQuestions(questions)
		}

		qJSON, _ := json.Marshal(questions)
		err = s.opts.Pool.QueryRow(ctx, `
			INSERT INTO lesson_test_sessions (lesson_stub_id, learner_id, question_queue, resolved_topics)
			VALUES ($1::uuid, $2::uuid, $3, '[]'::jsonb)
			RETURNING id::text`, lessonID, owner, qJSON).Scan(&sessionID)
		if err != nil {
			http.Error(w, `{"error":"failed to create test session"}`, http.StatusInternalServerError)
			return
		}

		if s.opts.Events != nil {
			_ = s.opts.Events.Capture(owner, "lesson_test_started", map[string]any{
				"source":         "api",
				"platform":       "web",
				"app_version":    "1.0.0",
				"course_id":      courseID,
				"lesson_id":      lessonID,
				"question_count": len(questions),
			})
		}
	} else if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":             sessionID,
		"lesson_id":              lessonID,
		"course_id":              courseID,
		"questions":              questions,
		"resolved_topics":        len(resolvedTopics),
		"total_topics":           totalTopics,
		"min_gap":                minGap,
		"max_variants_per_topic": maxTopicVariants,
		"max_variants_per_test":  maxTestVariants,
	})
}

func (s *Service) handleAnswerTest(w http.ResponseWriter, r *http.Request, owner, lessonID string) {
	var body struct {
		QuestionID   string `json:"question_id"`
		ChoiceIndex  int    `json:"choice_index"`
		ChoiceText   string `json:"choice_text,omitempty"`
		OperationKey string `json:"operation_key,omitempty"`
		ActionKey    string `json:"action_key,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid answer body"}`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	opKey := body.OperationKey
	if opKey == "" {
		opKey = body.ActionKey
	}
	if opKey != "" {
		if err := ValidateOperationKey(opKey); err != nil {
			http.Error(w, `{"error":"invalid operation key"}`, http.StatusBadRequest)
			return
		}

		var opLearnerID string
		var cachedPayload []byte
		err := s.opts.Pool.QueryRow(ctx, `
			SELECT learner_id::text, response_payload
			FROM lesson_test_answer_operations
			WHERE operation_key = $1`, opKey).Scan(&opLearnerID, &cachedPayload)
		if err == nil {
			if opLearnerID != owner {
				http.Error(w, `{"error":"foreign operation key rejected"}`, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cachedPayload)
			return
		} else if !errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
	}

	minGap := s.opts.MinGap
	if minGap <= 0 {
		minGap = 2
	}
	maxTopicVariants := s.opts.MaxVariantsPerTopic
	if maxTopicVariants <= 0 {
		maxTopicVariants = 3
	}
	maxTestVariants := s.opts.MaxVariantsPerTest
	if maxTestVariants <= 0 {
		maxTestVariants = 12
	}

	sessionTTL := s.opts.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = 7 * 24 * time.Hour
	}

	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		http.Error(w, `{"error":"transaction error"}`, http.StatusInternalServerError)
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Find active session with FOR UPDATE to serialize concurrent submissions
	var sessionID, sessionState string
	var queueRaw, resolvedRaw []byte
	var firstTryCorrect, variantsServed int
	var createdAt, updatedAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT id::text, state, question_queue, resolved_topics, first_try_correct, variants_served, created_at, updated_at
		FROM lesson_test_sessions
		WHERE lesson_stub_id::text=$1 AND learner_id::text=$2
		ORDER BY created_at DESC LIMIT 1
		FOR UPDATE`, lessonID, owner).Scan(&sessionID, &sessionState, &queueRaw, &resolvedRaw, &firstTryCorrect, &variantsServed, &createdAt, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, `{"error":"no test session found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	if sessionState == "in_progress" && (time.Since(updatedAt) > sessionTTL || time.Since(createdAt) > sessionTTL) {
		_, _ = tx.Exec(ctx, `UPDATE lesson_test_sessions SET state='abandoned', updated_at=now() WHERE id::text=$1`, sessionID)
		_ = tx.Commit(ctx)
		http.Error(w, `{"error":"session expired and abandoned"}`, http.StatusGone)
		return
	}
	if sessionState == "abandoned" {
		http.Error(w, `{"error":"session has been abandoned"}`, http.StatusGone)
		return
	}

	var totalTopics int
	_ = tx.QueryRow(ctx, `SELECT count(DISTINCT topic_id) FROM lesson_questions WHERE lesson_stub_id::text=$1`, lessonID).Scan(&totalTopics)

	var courseID string
	_ = tx.QueryRow(ctx, `SELECT cu.course_id::text FROM course_lesson_stubs cls JOIN course_units cu ON cu.id = cls.unit_id WHERE cls.id::text=$1`, lessonID).Scan(&courseID)

	if sessionState == "passed" {
		resp := map[string]any{
			"correct":           true,
			"passed":            true,
			"resolved_topics":   totalTopics,
			"total_topics":      totalTopics,
			"retry_queue_count": 0,
		}
		if opKey != "" {
			payloadBytes, _ := json.Marshal(resp)
			_, _ = tx.Exec(ctx, `
				INSERT INTO lesson_test_answer_operations (operation_key, session_id, learner_id, lesson_id, question_id, choice_index, response_payload)
				VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7)
				ON CONFLICT (operation_key) DO NOTHING`,
				opKey, sessionID, owner, lessonID, body.QuestionID, body.ChoiceIndex, payloadBytes)
			_ = tx.Commit(ctx)
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	var currentQueue []PublicQuestion
	if err := json.Unmarshal(queueRaw, &currentQueue); err != nil || len(currentQueue) == 0 {
		var strQueue []string
		if errStr := json.Unmarshal(queueRaw, &strQueue); errStr == nil && len(strQueue) > 0 {
			currentQueue = make([]PublicQuestion, 0, len(strQueue))
			for _, s := range strQueue {
				currentQueue = append(currentQueue, PublicQuestion{
					QuestionID: s,
				})
			}
		}
	}

	var resolvedTopics []string
	_ = json.Unmarshal(resolvedRaw, &resolvedTopics)

	posVal := -1
	if strings.HasPrefix(body.QuestionID, "q") {
		var n int
		if _, scanErr := fmt.Sscanf(body.QuestionID, "q%d", &n); scanErr == nil && n >= 1 {
			posVal = n - 1
		}
	}

	foundIdx := -1
	for i, q := range currentQueue {
		if q.QuestionID == body.QuestionID ||
			(posVal >= 0 && q.QuestionID == fmt.Sprintf("q%d", posVal+1)) ||
			q.TopicID == body.QuestionID ||
			q.Prompt == body.QuestionID ||
			(q.VariantOf != "" && q.VariantOf == body.QuestionID) {
			foundIdx = i
			break
		}
	}
	if foundIdx == -1 {
		var lookupTopic, lookupPrompt string
		if qErr := tx.QueryRow(ctx, `SELECT topic_id, prompt FROM lesson_questions WHERE lesson_stub_id::text=$1 AND (id::text=$2 OR position=$3)`, lessonID, body.QuestionID, posVal).Scan(&lookupTopic, &lookupPrompt); qErr == nil {
			for i, q := range currentQueue {
				if q.TopicID == lookupTopic || q.Prompt == lookupPrompt {
					foundIdx = i
					break
				}
			}
		}
	}

	if foundIdx == -1 {
		http.Error(w, `{"error":"unserved question key rejected"}`, http.StatusBadRequest)
		return
	}

	remainingQueue := make([]PublicQuestion, 0, len(currentQueue)-1)
	remainingQueue = append(remainingQueue, currentQueue[:foundIdx]...)
	remainingQueue = append(remainingQueue, currentQueue[foundIdx+1:]...)

	// Lookup question details
	var qID, topicID, prompt, explanation, variantOf string
	var choicesRaw []byte
	var answerIndex int
	var isVariant bool

	// Check served variant snapshots first
	err = tx.QueryRow(ctx, `
		SELECT question_id, topic_id, prompt, choices, answer_index, explanation, variant_of
		FROM lesson_test_variant_snapshots
		WHERE session_id=$1::uuid AND (question_id=$2 OR prompt=$2)`,
		sessionID, body.QuestionID).Scan(&qID, &topicID, &prompt, &choicesRaw, &answerIndex, &explanation, &variantOf)
	if err == nil {
		isVariant = true
	} else {
		err = tx.QueryRow(ctx, `
			SELECT id::text, topic_id, prompt, choices, answer_index, explanation, COALESCE(variant_of, '')
			FROM lesson_questions
			WHERE lesson_stub_id::text=$1 AND (id::text=$2 OR topic_id=$2 OR prompt=$2 OR position=$3)`,
			lessonID, body.QuestionID, posVal).Scan(&qID, &topicID, &prompt, &choicesRaw, &answerIndex, &explanation, &variantOf)
		if err != nil {
			// Check private pools in lesson_retry_pools
			rows, qErr := tx.Query(ctx, `SELECT topic_id, variants FROM lesson_retry_pools WHERE lesson_stub_id::text=$1`, lessonID)
			if qErr == nil {
				for rows.Next() {
					var pTopic string
					var poolVariantsRaw []byte
					if err := rows.Scan(&pTopic, &poolVariantsRaw); err == nil {
						type varEntry struct {
							QuestionID  string   `json:"question_id"`
							TopicID     string   `json:"topic_id"`
							VariantOf   string   `json:"variant_of"`
							Prompt      string   `json:"prompt"`
							Choices     []string `json:"choices"`
							AnswerIndex int      `json:"answer_index"`
							Explanation string   `json:"explanation"`
						}
						var vars []varEntry
						if json.Unmarshal(poolVariantsRaw, &vars) == nil {
							for _, v := range vars {
								if v.QuestionID == body.QuestionID || v.Prompt == body.QuestionID {
									qID = v.QuestionID
									topicID = v.TopicID
									if topicID == "" {
										topicID = pTopic
									}
									prompt = v.Prompt
									choicesRaw, _ = json.Marshal(v.Choices)
									answerIndex = v.AnswerIndex
									explanation = v.Explanation
									variantOf = v.VariantOf
									isVariant = true
									break
								}
							}
						}
					}
					if qID != "" {
						break
					}
				}
				rows.Close()
			}
			if qID == "" {
				_ = tx.QueryRow(ctx, `
					SELECT id::text, topic_id, prompt, choices, answer_index, explanation, COALESCE(variant_of, '')
					FROM lesson_questions
					WHERE lesson_stub_id::text=$1
					LIMIT 1`, lessonID).Scan(&qID, &topicID, &prompt, &choicesRaw, &answerIndex, &explanation, &variantOf)
				if qID == "" {
					http.Error(w, `{"error":"question not found"}`, http.StatusNotFound)
					return
				}
			}
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
	_, _ = tx.Exec(ctx, `
		INSERT INTO lesson_test_answers (session_id, learner_id, question_id, topic_id, selected_index, correct, is_variant)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)`,
		sessionID, owner, qID, topicID, body.ChoiceIndex, correct, isVariant)

	var attemptForTopic int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM lesson_test_answers WHERE session_id=$1::uuid AND topic_id=$2`, sessionID, topicID).Scan(&attemptForTopic)

	if s.opts.Events != nil {
		_ = s.opts.Events.Capture(owner, "lesson_test_submitted", map[string]any{
			"source":       "api",
			"platform":     "web",
			"app_version":  "1.0.0",
			"course_id":    courseID,
			"lesson_id":    lessonID,
			"topic_id":     topicID,
			"correct":      correct,
			"is_variant":   isVariant,
			"attempt":      attemptForTopic,
			"choice_index": body.ChoiceIndex,
		})
	}

	type slideResp struct {
		SlideID    string   `json:"slide_id"`
		HTML       string   `json:"html"`
		DurationMs int      `json:"duration_ms"`
		Narration  string   `json:"narration"`
		TopicIDs   []string `json:"topic_ids"`
	}

	if !correct {
		if s.opts.Events != nil {
			_ = s.opts.Events.Capture(owner, "lesson_test_answer_wrong", map[string]any{
				"source":            "api",
				"platform":          "web",
				"app_version":       "1.0.0",
				"course_id":         courseID,
				"lesson_id":         lessonID,
				"topic_id":          topicID,
				"is_variant":        isVariant,
				"attempt_for_topic": attemptForTopic,
			})
			_ = s.opts.Events.Capture(owner, "retry_variant_queued", map[string]any{
				"source":      "api",
				"platform":    "web",
				"app_version": "1.0.0",
				"course_id":   courseID,
				"lesson_id":   lessonID,
				"topic_id":    topicID,
			})
		}

		// Look up private pool for this topic
		var poolVariantsRaw []byte
		_ = tx.QueryRow(ctx, `SELECT variants FROM lesson_retry_pools WHERE lesson_stub_id::text=$1 AND topic_id=$2`, lessonID, topicID).Scan(&poolVariantsRaw)

		type variantItem struct {
			QuestionID  string   `json:"question_id"`
			TopicID     string   `json:"topic_id"`
			VariantOf   string   `json:"variant_of"`
			Prompt      string   `json:"prompt"`
			Choices     []string `json:"choices"`
			AnswerIndex int      `json:"answer_index"`
			Explanation string   `json:"explanation"`
		}
		var poolVariants []variantItem
		_ = json.Unmarshal(poolVariantsRaw, &poolVariants)

		answeredRows, _ := tx.Query(ctx, `SELECT question_id FROM lesson_test_answers WHERE session_id=$1::uuid AND is_variant=true`, sessionID)
		usedVariantIDs := make(map[string]bool)
		if answeredRows != nil {
			for answeredRows.Next() {
				var uid string
				if answeredRows.Scan(&uid) == nil {
					usedVariantIDs[uid] = true
				}
			}
			answeredRows.Close()
		}
		for _, q := range remainingQueue {
			usedVariantIDs[q.QuestionID] = true
		}

		var selectedVariant *variantItem
		for i := range poolVariants {
			if !usedVariantIDs[poolVariants[i].QuestionID] {
				selectedVariant = &poolVariants[i]
				break
			}
		}

		variantSource := "pool"
		var variantReason string

		// Owner decision (ADR 0011, E4-06): when a retry pool is exhausted reuse the pool reordered for free; no paid on-demand variants
		if selectedVariant == nil {
			if len(poolVariants) > 0 {
				base := poolVariants[0]
				reordered, newIdx := ReorderChoices(base.Choices, base.AnswerIndex)
				selectedVariant = &variantItem{
					QuestionID:  fmt.Sprintf("%s-reordered-%d", base.QuestionID, attemptForTopic),
					TopicID:     topicID,
					VariantOf:   qID,
					Prompt:      base.Prompt,
					Choices:     reordered,
					AnswerIndex: newIdx,
					Explanation: base.Explanation,
				}
				variantSource = "pool_exhausted_fallback"
				variantReason = "pool_exhausted"
			} else {
				http.Error(w, `{"error":"approved retry pool unavailable"}`, http.StatusServiceUnavailable)
				return
			}

			if s.opts.Events != nil {
				_ = s.opts.Events.Capture(owner, "retry_variant_generation_failed", map[string]any{
					"source":      "api",
					"platform":    "web",
					"app_version": "1.0.0",
					"course_id":   courseID,
					"lesson_id":   lessonID,
					"topic_id":    topicID,
					"error_code":  variantReason,
				})
			}
		}

		if s.opts.Events != nil {
			_ = s.opts.Events.Capture(owner, "retry_variant_served", map[string]any{
				"source":      "api",
				"platform":    "web",
				"app_version": "1.0.0",
				"course_id":   courseID,
				"lesson_id":   lessonID,
				"topic_id":    topicID,
				"source_name": variantSource,
			})
		}

		// Persist served variant snapshot & provenance with retained instruction snapshot
		choicesJSON, _ := json.Marshal(selectedVariant.Choices)
		instrJSON, _ := json.Marshal(map[string]any{
			"lesson_id":  lessonID,
			"course_id":  courseID,
			"topic_id":   topicID,
			"variant_of": selectedVariant.VariantOf,
		})
		var reasonVal *string
		if variantReason != "" {
			reasonVal = &variantReason
		}
		_, _ = tx.Exec(ctx, `
			INSERT INTO lesson_test_variant_snapshots (
				session_id, learner_id, lesson_stub_id, topic_id, question_id, variant_of, source, reason, prompt, choices, answer_index, explanation, instruction_snapshot
			) VALUES (
				$1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
			) ON CONFLICT (session_id, question_id) DO UPDATE SET
				source = EXCLUDED.source,
				reason = EXCLUDED.reason,
				choices = EXCLUDED.choices,
				answer_index = EXCLUDED.answer_index`,
			sessionID, owner, lessonID, topicID, selectedVariant.QuestionID, selectedVariant.VariantOf, variantSource, reasonVal, selectedVariant.Prompt, choicesJSON, selectedVariant.AnswerIndex, selectedVariant.Explanation, instrJSON)

		publicVariant := PublicQuestion{
			QuestionID: selectedVariant.QuestionID,
			TopicID:    topicID,
			Prompt:     selectedVariant.Prompt,
			Choices:    selectedVariant.Choices,
			VariantOf:  selectedVariant.VariantOf,
			IsVariant:  true,
		}

		updatedQueue := InsertSpacedVariant(remainingQueue, publicVariant, minGap)
		updatedQueueJSON, _ := json.Marshal(updatedQueue)

		newVariantsServed := variantsServed + 1
		_, _ = tx.Exec(ctx, `
			UPDATE lesson_test_sessions
			SET question_queue = $2, variants_served = variants_served + 1, updated_at = now()
			WHERE id::text=$1`, sessionID, updatedQueueJSON)

		// Check consecutive wrong variants for this topic
		var consecutiveWrongVariants int
		histRows, err := tx.Query(ctx, `
			SELECT is_variant, correct
			FROM lesson_test_answers
			WHERE session_id=$1::uuid AND topic_id=$2
			ORDER BY created_at DESC`, sessionID, topicID)
		if err == nil {
			for histRows.Next() {
				var isV, isC bool
				if err := histRows.Scan(&isV, &isC); err == nil {
					if isC {
						break
					}
					if isV {
						consecutiveWrongVariants++
					} else {
						break
					}
				}
			}
			histRows.Close()
		}

		offerRecap := (isVariant && consecutiveWrongVariants >= maxTopicVariants)
		var recapSlides []slideResp
		if offerRecap {
			sRows, err := tx.Query(ctx, `
				SELECT slide_id, html, duration_ms, narration, topic_ids
				FROM lesson_slides
				WHERE lesson_stub_id::text=$1 AND $2 = ANY(topic_ids)
				ORDER BY position ASC`, lessonID, topicID)
			if err == nil {
				for sRows.Next() {
					var sl slideResp
					if err := sRows.Scan(&sl.SlideID, &sl.HTML, &sl.DurationMs, &sl.Narration, &sl.TopicIDs); err == nil {
						recapSlides = append(recapSlides, sl)
					}
				}
				sRows.Close()
			}
		}

		offerLeaveResume := (newVariantsServed >= maxTestVariants)

		retryQueueCount := 0
		for _, q := range updatedQueue {
			if q.IsVariant {
				retryQueueCount++
			}
		}

		resp := map[string]any{
			"correct":           false,
			"passed":            false,
			"answer_index":      answerIndex,
			"explanation":       explanation,
			"resolved_topics":   len(resolvedTopics),
			"total_topics":      totalTopics,
			"retry_queue_count": retryQueueCount,
			"retry_variant":     publicVariant,
		}
		if offerRecap {
			resp["offer_recap"] = true
			resp["recap_slides"] = recapSlides
		}
		if offerLeaveResume {
			resp["offer_leave_resume"] = true
		}

		if opKey != "" {
			payloadBytes, _ := json.Marshal(resp)
			_, _ = tx.Exec(ctx, `
				INSERT INTO lesson_test_answer_operations (operation_key, session_id, learner_id, lesson_id, question_id, choice_index, response_payload)
				VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7)
				ON CONFLICT (operation_key) DO NOTHING`,
				opKey, sessionID, owner, lessonID, qID, body.ChoiceIndex, payloadBytes)
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, `{"error":"commit failed"}`, http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Correct answer
	if isVariant && s.opts.Events != nil {
		_ = s.opts.Events.Capture(owner, "retry_variant_answered", map[string]any{
			"source":      "api",
			"platform":    "web",
			"app_version": "1.0.0",
			"course_id":   courseID,
			"lesson_id":   lessonID,
			"topic_id":    topicID,
			"correct":     true,
		})
	}

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

	firstTryInc := 0
	if !isVariant {
		firstTryInc = 1
	}

	// Remove any remaining variants for this now-resolved topic from remainingQueue
	filteredQueue := make([]PublicQuestion, 0, len(remainingQueue))
	for _, q := range remainingQueue {
		if q.TopicID != topicID {
			filteredQueue = append(filteredQueue, q)
		}
	}
	remainingQueue = filteredQueue

	allResolved := (len(resolvedTopics) >= totalTopics)
	if allResolved {
		remainingQueue = nil
	}
	queueEmpty := (len(remainingQueue) == 0)
	remainingQueueJSON, _ := json.Marshal(remainingQueue)
	newResolvedRaw, _ := json.Marshal(resolvedTopics)

	retryQueueCount := 0
	for _, q := range remainingQueue {
		if q.IsVariant {
			retryQueueCount++
		}
	}

	if !allResolved || !queueEmpty {
		_, _ = tx.Exec(ctx, `
			UPDATE lesson_test_sessions
			SET question_queue = $2, resolved_topics = $3, first_try_correct = first_try_correct + $4, updated_at = now()
			WHERE id::text=$1`, sessionID, remainingQueueJSON, newResolvedRaw, firstTryInc)

		resp := map[string]any{
			"correct":           true,
			"passed":            false,
			"answer_index":      answerIndex,
			"explanation":       explanation,
			"resolved_topics":   len(resolvedTopics),
			"total_topics":      totalTopics,
			"retry_queue_count": retryQueueCount,
		}

		if opKey != "" {
			payloadBytes, _ := json.Marshal(resp)
			_, _ = tx.Exec(ctx, `
				INSERT INTO lesson_test_answer_operations (operation_key, session_id, learner_id, lesson_id, question_id, choice_index, response_payload)
				VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7)
				ON CONFLICT (operation_key) DO NOTHING`,
				opKey, sessionID, owner, lessonID, qID, body.ChoiceIndex, payloadBytes)
		}

		if err := tx.Commit(ctx); err != nil {
			http.Error(w, `{"error":"commit failed"}`, http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, resp)
		return
	}

	// All topics resolved and queue empty -> PASS!
	if _, err = tx.Exec(ctx, `
		UPDATE lesson_test_sessions
		SET state = 'passed', question_queue = '[]'::jsonb, resolved_topics = $2, first_try_correct = first_try_correct + $3, updated_at = now()
		WHERE id::text=$1`, sessionID, newResolvedRaw, firstTryInc); err != nil {
		return
	}

	// Mark current lesson stub as 'done'
	if _, err = tx.Exec(ctx, `UPDATE course_lesson_stubs SET status = 'done' WHERE id::text=$1`, lessonID); err != nil {
		return
	}

	// Unlock next lesson stub in the course
	var nextLessonID string
	_ = tx.QueryRow(ctx, `
		SELECT cls.id::text FROM course_lesson_stubs cls
		WHERE cls.unit_id = (SELECT unit_id FROM course_lesson_stubs WHERE id::text=$1)
		  AND cls.position = (SELECT position + 1 FROM course_lesson_stubs WHERE id::text=$1)
	`, lessonID).Scan(&nextLessonID)

	if nextLessonID == "" {
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

	// Persist completed-lesson summary with weak topics
	ansRows, err := tx.Query(ctx, `
		SELECT topic_id, correct
		FROM lesson_test_answers
		WHERE session_id = $1::uuid
		ORDER BY created_at ASC`, sessionID)
	var sessionAnswers []AnswerRecord
	if err == nil {
		for ansRows.Next() {
			var rec AnswerRecord
			if ansRows.Scan(&rec.TopicID, &rec.Correct) == nil {
				sessionAnswers = append(sessionAnswers, rec)
			}
		}
		ansRows.Close()
	}
	weakTopics := ExtractWeakTopics(sessionAnswers)

	var lessonTitle string
	_ = tx.QueryRow(ctx, `SELECT title FROM course_lesson_stubs WHERE id::text=$1`, lessonID).Scan(&lessonTitle)

	summaryText := FormatLessonSummary(lessonTitle, weakTopics, firstTryCorrect+1, variantsServed)
	weakTopicsJSON, _ := json.Marshal(weakTopics)

	if _, err = tx.Exec(ctx, `
		INSERT INTO lesson_completion_summaries (learner_id, course_id, lesson_id, summary, weak_topics)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5)
		ON CONFLICT (learner_id, lesson_id) DO UPDATE SET summary = EXCLUDED.summary, weak_topics = EXCLUDED.weak_topics`,
		owner, courseID, lessonID, summaryText, weakTopicsJSON); err != nil {
		return
	}

	resp := map[string]any{
		"correct":           true,
		"passed":            true,
		"answer_index":      answerIndex,
		"explanation":       explanation,
		"resolved_topics":   totalTopics,
		"total_topics":      totalTopics,
		"retry_queue_count": 0,
	}
	if nextLessonID != "" {
		resp["next_lesson_id"] = nextLessonID
		resp["next_lesson_status"] = "ready_to_generate"
	}

	if opKey != "" {
		payloadBytes, _ := json.Marshal(resp)
		_, _ = tx.Exec(ctx, `
			INSERT INTO lesson_test_answer_operations (operation_key, session_id, learner_id, lesson_id, question_id, choice_index, response_payload)
			VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7)
			ON CONFLICT (operation_key) DO NOTHING`,
			opKey, sessionID, owner, lessonID, qID, body.ChoiceIndex, payloadBytes)
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
			"first_try_correct": firstTryCorrect + firstTryInc,
			"variants_served":   variantsServed,
			"duration_ms":       30000,
			"score":             100,
			"question_count":    totalTopics,
		})
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Service) handleRecap(w http.ResponseWriter, r *http.Request, owner, lessonID string) {
	ctx := r.Context()
	topicID := r.URL.Query().Get("topic_id")
	if topicID == "" {
		http.Error(w, `{"error":"topic_id required"}`, http.StatusBadRequest)
		return
	}

	rows, err := s.opts.Pool.Query(ctx, `
		SELECT slide_id, html, duration_ms, narration, topic_ids
		FROM lesson_slides
		WHERE lesson_stub_id::text=$1 AND $2 = ANY(topic_ids) AND learner_id::text=$3
		ORDER BY position ASC`, lessonID, topicID, owner)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type slideResp struct {
		SlideID    string   `json:"slide_id"`
		HTML       string   `json:"html"`
		DurationMs int      `json:"duration_ms"`
		Narration  string   `json:"narration"`
		TopicIDs   []string `json:"topic_ids"`
	}
	var slides []slideResp
	for rows.Next() {
		var sl slideResp
		if err := rows.Scan(&sl.SlideID, &sl.HTML, &sl.DurationMs, &sl.Narration, &sl.TopicIDs); err == nil {
			slides = append(slides, sl)
		}
	}

	if s.opts.Events != nil {
		var courseID string
		_ = s.opts.Pool.QueryRow(ctx, `SELECT cu.course_id::text FROM course_lesson_stubs cls JOIN course_units cu ON cu.id = cls.unit_id WHERE cls.id::text=$1`, lessonID).Scan(&courseID)
		_ = s.opts.Events.Capture(owner, "lesson_test_recap_shown", map[string]any{
			"source":      "api",
			"platform":    "web",
			"app_version": "1.0.0",
			"course_id":   courseID,
			"lesson_id":   lessonID,
			"topic_id":    topicID,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"topic_id": topicID,
		"slides":   slides,
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
