//go:build integration

package lessontest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/steering"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type reviewFixture struct {
	financial                    *credits.Service
	db                           *store.Store
	engine                       *mockLessonEngine
	service                      *lessontest.Service
	owner, lesson, course, token string
	login                        func(string) string
	call                         func(string, string, any, string) (int, map[string]any, string)
}

func newReviewFixture(t *testing.T) *reviewFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("lessontest"),
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
	t.Cleanup(db.Close)

	mail := new(accounts.MailSink)
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key", 5), mail)
	if err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err = worker.Migrate(ctx, db.Pool, logger); err != nil {
		t.Fatal(err)
	}

	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}

	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)

	mockEngine := &mockLessonEngine{
		lesson: &enginev1.Lesson{
			Slides: []*enginev1.Slide{
				{
					SlideId:    "s1",
					Html:       `<div id="root"><h1>Night Sky</h1><p>Observing the stars.</p></div>`,
					DurationMs: 3000,
					Narration:  "Observing the stars tonight.",
					TopicIds:   []string{"top-stars"},
				},
			},
			Test: []*enginev1.Question{
				{
					QuestionId:  "q1",
					TopicId:     "top-stars",
					Prompt:      "What do we observe in the night sky?",
					Choices:     []string{"Stars", "Sun", "Clouds"},
					AnswerIndex: 0,
					Explanation: "Stars shine bright in the night sky.",
				},
			},
			VariantPool: []*enginev1.TopicVariants{
				{
					TopicId: "top-stars",
					Variants: []*enginev1.Question{
						{
							QuestionId:  "v1-q1",
							TopicId:     "top-stars",
							VariantOf:   "q1",
							Prompt:      "What shines brightly across the night?",
							Choices:     []string{"Distant suns", "Space dust", "Moonlight"},
							AnswerIndex: 0,
							Explanation: "Distant suns shine as stars.",
						},
						{
							QuestionId:  "v2-q1",
							TopicId:     "top-stars",
							VariantOf:   "q1",
							Prompt:      "What celestial objects dot the night?",
							Choices:     []string{"Stars", "Sun rays", "Day clouds"},
							AnswerIndex: 0,
							Explanation: "Stars dot the night sky.",
						},
					},
				},
			},
		},
		attempts: []*enginev1.Usage{
			{
				Provider:     "mock",
				Model:        "claude-sonnet-5-5",
				InputTokens:  500,
				OutputTokens: 200,
				LatencyMs:    100,
				Delivered:    true,
				Component:    "content",
			},
		},
	}

	coursesService := courses.New(courses.Options{Pool: db.Pool, Financial: financial})
	lessonService := lessontest.New(lessontest.Options{
		Pool:      db.Pool,
		Engine:    mockEngine,
		Financial: financial,
	})

	lessonQueue, err := lessonService.LessonQueue(logger)
	if err != nil {
		t.Fatal(err)
	}
	_ = lessonQueue

	deps := httpapi.Dependencies{
		Steering:                steering.New(steering.Options{Pool: db.Pool, Engine: steeringScreen{}}),
		Accounts:                auth,
		Quotes:                  pricing.NewQuotes(db.Pool),
		CourseBrief:             coursesService,
		Credits:                 financial,
		LessonTestTracerEnabled: true,
	}
	server := httptest.NewServer(httpapi.NewWithLessonTest(deps, lessonService, logger))
	t.Cleanup(server.Close)

	call := func(method, path string, body any, token string) (int, map[string]any, string) {
		var r io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			r = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, r)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		rawBytes, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(rawBytes, &out)
		return resp.StatusCode, out, string(rawBytes)
	}

	login := func(email string) string {
		if code, _, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); code != 202 {
			t.Fatal(code)
		}
		code, out, _ := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
		if code != 200 {
			t.Fatal(code, out)
		}
		return out["access_token"].(string)
	}

	token := login("learner@test.local")
	var learnerID string
	_ = db.Pool.QueryRow(ctx, "SELECT id::text FROM learners LIMIT 1").Scan(&learnerID)

	// Seed credits: 500
	_, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1, 500, 'purchase', 'test-seed')`, learnerID)
	if err != nil {
		t.Fatal(err)
	}

	// Create Course and Stubs
	var creationQuoteID, creationID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'seed-ref', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerID, cat.Config().Version).Scan(&creationQuoteID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, creationQuoteID).Scan(&creationID); err != nil {
		t.Fatal(err)
	}

	var courseID, unitID, lesson1ID, lesson2ID string
	err = db.Pool.QueryRow(ctx, `
		WITH c AS (
			INSERT INTO courses(learner_id, creation_id, title)
			VALUES($1, $2::uuid, 'Astronomy')
			RETURNING id
		),
		u AS (
			INSERT INTO course_units(learner_id, course_id, authored_id, position, title)
			SELECT $1, c.id, 'u1', 0, 'Night Sky' FROM c
			RETURNING id, course_id
		),
		e AS (
			INSERT INTO course_enrollments(learner_id, course_id)
			SELECT $1, c.id FROM c
		),
		l1 AS (
			INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status)
			SELECT $1, u.id, 'l1', 0, 'Stars', 'Learn stars', 'ready_to_generate' FROM u
			RETURNING id, unit_id
		),
		l2 AS (
			INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status)
			SELECT $1, l1.unit_id, 'l2', 1, 'Moon', 'Learn moon', 'locked' FROM l1
			RETURNING id
		)
		SELECT (SELECT id::text FROM c), (SELECT id::text FROM u), (SELECT id::text FROM l1), (SELECT id::text FROM l2);
	`, learnerID, creationID).Scan(&courseID, &unitID, &lesson1ID, &lesson2ID)
	if err != nil {
		t.Fatal(err)
	}

	_ = unitID
	_ = lesson2ID
	return &reviewFixture{financial: financial, db: db, engine: mockEngine, service: lessonService, owner: learnerID, lesson: lesson1ID, course: courseID, token: token, call: call, login: login}
}

func (f *reviewFixture) prepare(t *testing.T) string {
	t.Helper()
	code, out, raw := f.call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, f.token)
	if code != 201 && code != 200 {
		t.Fatalf("quote: %d %s", code, raw)
	}
	quote := out["id"].(string)
	code, _, raw = f.call("POST", "/v1/quotes/"+quote+"/accept", nil, f.token)
	if code != 200 {
		t.Fatalf("accept: %d %s", code, raw)
	}
	code, out, raw = f.call("POST", "/v1/lessons/"+f.lesson+"/prepare", map[string]any{"quote_id": quote}, f.token)
	if code != 202 {
		t.Fatalf("prepare: %d %s", code, raw)
	}
	return out["id"].(string)
}

func TestReviewFailedGenerationReleasesCredits(t *testing.T) {
	f := newReviewFixture(t)
	id := f.prepare(t)
	f.engine.err = errors.New("provider unavailable")
	if err := f.service.RunLessonGeneration(t.Context(), id); err == nil {
		t.Fatal("expected provider failure")
	}
	var state string
	if err := f.db.Pool.QueryRow(t.Context(), `SELECT j.state FROM credit_jobs j JOIN lesson_generations g ON g.reservation_id=j.id WHERE g.id=$1::uuid`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "released" {
		t.Fatalf("failed generation leaked %s hold", state)
	}
}

func TestReviewPersistenceFailureDoesNotChargeAndResumes(t *testing.T) {
	f := newReviewFixture(t)
	id := f.prepare(t)
	_, err := f.db.Pool.Exec(t.Context(), `CREATE FUNCTION reject_review_content() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'storage unavailable'; END $$; CREATE TRIGGER reject_review_content BEFORE INSERT ON lesson_questions FOR EACH ROW EXECUTE FUNCTION reject_review_content()`)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.RunLessonGeneration(t.Context(), id); err == nil {
		t.Fatal("expected persistence failure")
	}
	var state string
	if err = f.db.Pool.QueryRow(t.Context(), `SELECT j.state FROM credit_jobs j JOIN lesson_generations g ON g.reservation_id=j.id WHERE g.id=$1::uuid`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "settled" {
		t.Error("charged before content committed")
	}
	var generationState string
	var hasCheckpoint bool
	if err = f.db.Pool.QueryRow(t.Context(), `SELECT state,result_checkpoint IS NOT NULL FROM lesson_generations WHERE id=$1::uuid`, id).Scan(&generationState, &hasCheckpoint); err != nil {
		t.Fatal(err)
	}
	if generationState != "validated" || !hasCheckpoint {
		t.Fatalf("missing durable validated checkpoint: %s %v", generationState, hasCheckpoint)
	}

	if _, err = f.db.Pool.Exec(t.Context(), `DROP TRIGGER reject_review_content ON lesson_questions`); err != nil {
		t.Fatal(err)
	}
	var partial int
	if err = f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM generated_lessons WHERE lesson_stub_id=$1::uuid`, f.lesson).Scan(&partial); err != nil {
		t.Fatal(err)
	}
	if partial != 0 {
		t.Fatal("partial lesson persisted after transaction failure")
	}

	// A persisted provider result must survive a worker restart without another paid call.
	f.engine.err = errors.New("provider must not run again")
	resumed := lessontest.New(lessontest.Options{Pool: f.db.Pool, Financial: f.financial, Engine: f.engine})
	if err = resumed.RunLessonGeneration(t.Context(), id); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err = f.db.Pool.QueryRow(t.Context(), `SELECT state FROM lesson_generations WHERE id=$1::uuid`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "completed" {
		t.Fatalf("worker stranded generation in %s", state)
	}
}

func TestReviewPrepareRejectsCurrentLesson(t *testing.T) {
	f := newReviewFixture(t)
	if _, err := f.db.Pool.Exec(t.Context(), `UPDATE course_lesson_stubs SET status='current' WHERE id=$1::uuid`, f.lesson); err != nil {
		t.Fatal(err)
	}
	code, out, raw := f.call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, f.token)
	if code != 201 && code != 200 {
		t.Fatal(code, raw)
	}
	quote := out["id"].(string)
	f.call("POST", "/v1/quotes/"+quote+"/accept", nil, f.token)
	code, _, raw = f.call("POST", "/v1/lessons/"+f.lesson+"/prepare", map[string]any{"quote_id": quote}, f.token)
	if code != 409 {
		t.Fatalf("current lesson regenerated: %d %s", code, raw)
	}
}

func TestReviewPrepareEnqueueFailureRollsBack(t *testing.T) {
	f := newReviewFixture(t)
	if _, err := f.db.Pool.Exec(t.Context(), `CREATE FUNCTION reject_review_queue() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'queue unavailable'; END $$; CREATE TRIGGER reject_review_queue BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION reject_review_queue()`); err != nil {
		t.Fatal(err)
	}
	code, out, raw := f.call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, f.token)
	if code != 201 && code != 200 {
		t.Fatal(code, raw)
	}
	quote := out["id"].(string)
	f.call("POST", "/v1/quotes/"+quote+"/accept", nil, f.token)
	code, _, raw = f.call("POST", "/v1/lessons/"+f.lesson+"/prepare", map[string]any{"quote_id": quote}, f.token)
	if code != 500 {
		t.Errorf("enqueue failure hidden: %d %s", code, raw)
	}
	var count int
	if err := f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM lesson_generations WHERE lesson_id=$1::uuid`, f.lesson).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("orphan generation committed: %d", count)
	}
}

func TestReviewPrepareConcurrentRequests(t *testing.T) {
	f := newReviewFixture(t)
	code, out, raw := f.call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, f.token)
	if code != 201 && code != 200 {
		t.Fatal(code, raw)
	}
	quote := out["id"].(string)
	f.call("POST", "/v1/quotes/"+quote+"/accept", nil, f.token)
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Go(func() {
			c, _, _ := f.call("POST", "/v1/lessons/"+f.lesson+"/prepare", map[string]any{"quote_id": quote}, f.token)
			codes <- c
		})
	}
	wg.Wait()
	close(codes)
	accepted := 0
	for c := range codes {
		if c == 202 {
			accepted++
		} else if c != 409 {
			t.Errorf("unexpected concurrent status: %d", c)
		}
	}
	if accepted != 1 {
		t.Fatalf("accepted %d concurrent generations", accepted)
	}
}

func TestReviewRecapRejectsAnotherLearner(t *testing.T) {
	f := newReviewFixture(t)
	id := f.prepare(t)
	if err := f.service.RunLessonGeneration(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	intruder := f.login("intruder@test.local")
	code, out, raw := f.call("GET", "/v1/lessons/"+f.lesson+"/test/recap?topic_id=top-stars", nil, intruder)
	if code != 200 && code != 404 {
		t.Fatal(code, raw)
	}
	if slides, ok := out["slides"].([]any); ok && len(slides) > 0 {
		t.Fatalf("recap leaked another learner's slides: %s", raw)
	}
}

func TestReviewCreditCleanupFailureIsRetried(t *testing.T) {
	f := newReviewFixture(t)
	id := f.prepare(t)
	_, err := f.db.Pool.Exec(t.Context(), `CREATE FUNCTION reject_review_release() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.state='released' THEN RAISE EXCEPTION 'ledger unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_review_release BEFORE UPDATE ON credit_jobs FOR EACH ROW EXECUTE FUNCTION reject_review_release()`)
	if err != nil {
		t.Fatal(err)
	}
	f.engine.err = errors.New("provider unavailable")
	if err = f.service.RunLessonGeneration(t.Context(), id); err == nil || !strings.Contains(err.Error(), "ledger unavailable") {
		t.Fatalf("cleanup error hidden: %v", err)
	}
	var state string
	if err = f.db.Pool.QueryRow(t.Context(), `SELECT state FROM lesson_generations WHERE id=$1::uuid`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state == "failed" {
		t.Fatal("terminal state before releasing credits")
	}
	if _, err = f.db.Pool.Exec(t.Context(), `DROP TRIGGER reject_review_release ON credit_jobs`); err != nil {
		t.Fatal(err)
	}
	if err = f.service.RunLessonGeneration(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if err = f.db.Pool.QueryRow(t.Context(), `SELECT j.state FROM credit_jobs j JOIN lesson_generations g ON g.reservation_id=j.id WHERE g.id=$1::uuid`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "released" {
		t.Fatalf("cleanup retry leaked %s hold", state)
	}
}
func TestReviewActiveGenerationUniqueConstraint(t *testing.T) {
	f := newReviewFixture(t)
	id := f.prepare(t)
	_, err := f.db.Pool.Exec(t.Context(), `INSERT INTO lesson_generations(learner_id,course_id,lesson_id,quote_id,state) SELECT learner_id,course_id,lesson_id,quote_id,'queued' FROM lesson_generations WHERE id=$1::uuid`, id)
	if err == nil {
		t.Fatal("database allowed duplicate active generation")
	}
}
func TestReviewMissingPoolNeverEchoesSourcePrompt(t *testing.T) {
	f := newReviewFixture(t)
	id := f.prepare(t)
	if err := f.service.RunLessonGeneration(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool.Exec(t.Context(), `DELETE FROM lesson_retry_pools WHERE lesson_stub_id=$1::uuid`, f.lesson); err != nil {
		t.Fatal(err)
	}
	code, out, raw := f.call("GET", "/v1/lessons/"+f.lesson+"/test", nil, f.token)
	if code != 200 {
		t.Fatal(code, raw)
	}
	q := out["questions"].([]any)[0].(map[string]any)
	code, out, raw = f.call("POST", "/v1/lessons/"+f.lesson+"/test/answer", map[string]any{"question_id": q["question_id"], "choice_index": 1}, f.token)
	if code == 200 {
		t.Fatalf("missing approved pool should fail closed, got %v %s", out, raw)
	}
	var count int
	if err := f.db.Pool.QueryRow(t.Context(), `SELECT count(*) FROM lesson_test_answers WHERE learner_id=$1::uuid`, f.owner).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("unanswerable retry consumed the answer")
	}
}
