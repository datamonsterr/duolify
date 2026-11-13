//go:build integration

package lessontest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type mockLessonEngine struct {
	lesson   *enginev1.Lesson
	attempts []*enginev1.Usage
	err      error
}

func (m *mockLessonEngine) GenerateLesson(_ context.Context, req *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error) {
	if m.err != nil {
		return nil, nil, m.err
	}
	provider := "anthropic"
	model := "sonnet"
	if req != nil && req.Limits != nil {
		if req.Limits.Provider != "" {
			provider = req.Limits.Provider
		}
		if req.Limits.Model != "" {
			model = req.Limits.Model
		}
	}
	attempts := []*enginev1.Usage{
		{
			Provider:     provider,
			Model:        model,
			Attempt:      1,
			InputTokens:  100,
			OutputTokens: 50,
			LatencyMs:    100,
			Delivered:    true,
			Component:    "content",
		},
	}
	return m.lesson, attempts, nil
}

func TestGenerateAndPassFirstGatedLessonIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

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
	defer db.Close()

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
		Accounts:                auth,
		Quotes:                  pricing.NewQuotes(db.Pool),
		CourseBrief:             coursesService,
		Credits:                 financial,
		LessonTestTracerEnabled: true,
	}
	server := httptest.NewServer(httpapi.NewWithLessonTest(deps, lessonService, logger))
	defer server.Close()

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

	// Ineligible lesson (lesson 2) cannot be prepared!
	quoteCode, quoteResp, _ := call("POST", "/v1/quotes", map[string]any{
		"action":               "lesson_content",
		"narration_characters": 100,
	}, token)
	if quoteCode != 201 && quoteCode != 200 {
		t.Fatalf("quote failed: %d %v", quoteCode, quoteResp)
	}
	quoteID := quoteResp["id"].(string)

	// Accept quote
	call("POST", "/v1/quotes/"+quoteID+"/accept", nil, token)

	// Attempt to prepare locked lesson 2 -> must reject with 409
	ineligibleCode, _, _ := call("POST", "/v1/lessons/"+lesson2ID+"/prepare", map[string]any{
		"quote_id": quoteID,
	}, token)
	if ineligibleCode != 409 {
		t.Fatalf("expected 409 for locked lesson 2, got %d", ineligibleCode)
	}

	// Prepare eligible lesson 1 -> 202 Accepted
	prepCode, prepResp, _ := call("POST", "/v1/lessons/"+lesson1ID+"/prepare", map[string]any{
		"quote_id": quoteID,
	}, token)
	if prepCode != 202 {
		t.Fatalf("expected 202 for lesson 1 prepare, got %d %v", prepCode, prepResp)
	}
	genID := prepResp["id"].(string)

	// Execute background generation job
	if err = lessonService.RunLessonGeneration(ctx, genID); err != nil {
		t.Fatalf("generation job failed: %v", err)
	}

	// Verify lesson is now generated
	lessonCode, lessonOut, _ := call("GET", "/v1/lessons/"+lesson1ID, nil, token)
	if lessonCode != 200 {
		t.Fatalf("expected 200 from GET /v1/lessons/%s, got %d %v", lesson1ID, lessonCode, lessonOut)
	}
	slides := lessonOut["slides"].([]any)
	if len(slides) != 1 {
		t.Fatalf("expected 1 slide, got %d", len(slides))
	}
	// Answers must NOT be returned on lesson endpoint
	if lessonOut["test"] != nil || lessonOut["questions"] != nil {
		t.Fatalf("lesson payload exposed test questions or answers")
	}

	// Fetch test questions
	testCode, testOut, testRaw := call("GET", "/v1/lessons/"+lesson1ID+"/test", nil, token)
	if testCode != 200 {
		t.Fatalf("expected 200 from GET /v1/lessons/%s/test, got %d %v", lesson1ID, testCode, testOut)
	}
	questions := testOut["questions"].([]any)
	if len(questions) != 1 {
		t.Fatalf("expected 1 test question, got %d", len(questions))
	}
	q1 := questions[0].(map[string]any)
	q1ID := q1["question_id"].(string)
	if q1["answer_index"] != nil || q1["explanation"] != nil {
		t.Fatalf("GET test exposed answer_index or explanation")
	}
	// Confirm answer key and explanation are NEVER returned before submission
	if strings.Contains(testRaw, "answer_index") || strings.Contains(testRaw, "explanation") {
		t.Fatalf("GET test raw body leaked answer_index or explanation: %s", testRaw)
	}

	// Submit wrong answer
	ans1Code, ans1Out, _ := call("POST", "/v1/lessons/"+lesson1ID+"/test/answer", map[string]any{
		"question_id":  q1ID,
		"choice_index": 1,
	}, token)
	if ans1Code != 200 {
		t.Fatalf("answer failed: %d %v", ans1Code, ans1Out)
	}
	if ans1Out["correct"] != false {
		t.Fatalf("expected correct=false for choice 1")
	}
	if ans1Out["passed"] != false {
		t.Fatalf("expected passed=false")
	}
	if ans1Out["explanation"] == nil || ans1Out["explanation"] == "" {
		t.Fatalf("expected explanation on wrong answer")
	}

	// Verify that reaching slides and failing test cannot bypass grading
	var prePassStatus string
	if err = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", lesson1ID).Scan(&prePassStatus); err != nil {
		t.Fatalf("fetch pre-pass status: %v", err)
	}
	if prePassStatus == "done" {
		t.Fatalf("reaching slides or wrong test answer must not mark lesson done (grading bypassed)")
	}
	var preOutboxCount int
	if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, lesson1ID).Scan(&preOutboxCount); err != nil {
		t.Fatalf("fetch pre-pass outbox: %v", err)
	}
	if preOutboxCount != 0 {
		t.Fatalf("completion outbox must remain empty before passing test, got %d", preOutboxCount)
	}

	// Submit correct answer
	ans2Code, ans2Out, _ := call("POST", "/v1/lessons/"+lesson1ID+"/test/answer", map[string]any{
		"question_id":   ans1Out["retry_variant"].(map[string]any)["question_id"],
		"operation_key": "first-lesson-final",
		"choice_index":  0,
	}, token)
	if ans2Code != 200 {
		t.Fatalf("correct answer failed: %d %v", ans2Code, ans2Out)
	}
	if ans2Out["correct"] != true || ans2Out["passed"] != true {
		t.Fatalf("expected correct=true and passed=true, got %v", ans2Out)
	}

	// E5 consumes this authoritative non-perfect pass, including retries, once.
	var award, configuredBase, awardCount int
	var retainedPerfect bool
	if err = db.Pool.QueryRow(ctx, `SELECT x.amount,c.base_xp,s.first_try_perfect FROM xp_events x JOIN lesson_completion_summaries s USING(learner_id,lesson_id) CROSS JOIN gamification_config c WHERE x.learner_id=$1::uuid AND x.lesson_id=$2::uuid`, learnerID, lesson1ID).Scan(&award, &configuredBase, &retainedPerfect); err != nil || award != configuredBase || retainedPerfect {
		t.Fatalf("non-perfect XP: %d %d %t %v", award, configuredBase, retainedPerfect, err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM xp_events WHERE learner_id=$1::uuid AND lesson_id=$2::uuid`, learnerID, lesson1ID).Scan(&awardCount); err != nil || awardCount != 1 {
		t.Fatal(awardCount, err)
	}

	// Verify completion outbox has 1 row
	var outboxCount int
	err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, lesson1ID).Scan(&outboxCount)
	if err != nil || outboxCount != 1 {
		t.Fatalf("expected 1 completion outbox row, got %d, err %v", outboxCount, err)
	}

	// Verify lesson 1 is done and lesson 2 is unlocked (ready_to_generate)
	var l1Status, l2Status string
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", lesson1ID).Scan(&l1Status)
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", lesson2ID).Scan(&l2Status)
	if l1Status != "done" {
		t.Fatalf("expected lesson 1 status 'done', got %q", l1Status)
	}
	if l2Status != "ready_to_generate" {
		t.Fatalf("expected lesson 2 status 'ready_to_generate', got %q", l2Status)
	}

	// Verify that with flag-off, routes return 404
	disabledServer := httptest.NewServer(httpapi.New(httpapi.Dependencies{
		Accounts:                auth,
		LessonTestTracerEnabled: false,
		LessonTest:              lessonService,
	}, logger))
	defer disabledServer.Close()

	disabledReq, _ := http.NewRequestWithContext(ctx, "GET", disabledServer.URL+"/v1/lessons/"+lesson1ID, nil)
	disabledReq.Header.Set("Authorization", "Bearer "+token)
	disabledResp, err := disabledServer.Client().Do(disabledReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = disabledResp.Body.Close()
	if disabledResp.StatusCode != 404 {
		t.Fatalf("expected 404 from disabled server, got %d", disabledResp.StatusCode)
	}

	// Duplicate pass submission is idempotent
	dupCode, dupOut, _ := call("POST", "/v1/lessons/"+lesson1ID+"/test/answer", map[string]any{
		"question_id":   ans1Out["retry_variant"].(map[string]any)["question_id"],
		"operation_key": "first-lesson-final",
		"choice_index":  0,
	}, token)
	if dupCode != 200 || dupOut["passed"] != true {
		t.Fatalf("duplicate submission failed: %d %v", dupCode, dupOut)
	}
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, lesson1ID).Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("duplicate submission created extra outbox row: %d", outboxCount)
	}
}
