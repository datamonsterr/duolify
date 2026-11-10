//go:build integration

package lessontest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type capturingLessonEngine struct {
	mu              sync.Mutex
	capturedRequest *enginev1.GenerateLessonRequest
	calls           int
	lesson          *enginev1.Lesson
	err             error
}

func (m *capturingLessonEngine) GenerateLesson(_ context.Context, req *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.capturedRequest = req
	if m.err != nil {
		return nil, nil, m.err
	}
	provider := "anthropic"
	model := "claude-sonnet-5-5"
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
			InputTokens:  120,
			OutputTokens: 60,
			LatencyMs:    150,
			Delivered:    true,
			Component:    "content",
		},
	}
	return m.lesson, attempts, nil
}

func (m *capturingLessonEngine) getCaptured() *enginev1.GenerateLessonRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.capturedRequest
}

func (m *capturingLessonEngine) getCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

func TestPrepareOnlyTheNextLessonAfterAPassIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("lessontest_e405"),
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

	mockEngine := &capturingLessonEngine{
		lesson: &enginev1.Lesson{
			Slides: []*enginev1.Slide{
				{
					SlideId:    "s-n1",
					Html:       `<div id="root"><h1>Polaris Navigation</h1><p>Find True North.</p></div>`,
					DurationMs: 4000,
					Narration:  "Finding true north with Polaris.",
					TopicIds:   []string{"top-polaris"},
				},
			},
			Test: []*enginev1.Question{
				{
					QuestionId:  "q-n1",
					TopicId:     "top-polaris",
					Prompt:      "Which constellation points to Polaris?",
					Choices:     []string{"Ursa Major", "Orion", "Cassiopeia"},
					AnswerIndex: 0,
					Explanation: "The pointer stars of Ursa Major line up directly with Polaris.",
				},
			},
			VariantPool: []*enginev1.TopicVariants{
				{
					TopicId: "top-polaris",
					Variants: []*enginev1.Question{
						{
							QuestionId:  "v1-q-n1",
							TopicId:     "top-polaris",
							VariantOf:   "q-n1",
							Prompt:      "What group of stars helps locate the North Star?",
							Choices:     []string{"The Big Dipper / Ursa Major", "The Southern Cross", "Canis Major"},
							AnswerIndex: 0,
							Explanation: "The Big Dipper points toward Polaris.",
						},
					},
				},
			},
		},
	}

	lessonService := lessontest.New(lessontest.Options{
		Pool:      db.Pool,
		Engine:    mockEngine,
		Financial: financial,
		Events:    nil,
	})

	coursesService := courses.New(courses.Options{Pool: db.Pool, Financial: financial})

	deps := httpapi.Dependencies{
		Accounts:                auth,
		LessonTestTracerEnabled: true,
		Quotes:                  pricing.NewQuotes(db.Pool),
		CourseBrief:             coursesService,
		Credits:                 financial,
		Pricing:                 pricing.NewRuntimeCatalogue(cat, db.Queries, db.Pool, nil),
	}
	server := httptest.NewServer(httpapi.NewWithLessonTest(deps, lessonService, logger))
	defer server.Close()

	// Helper for HTTP calls
	call := func(method, path string, body any, token string) (int, map[string]any, error) {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, e := http.NewRequestWithContext(ctx, method, server.URL+path, r)
		if e != nil {
			return 0, nil, e
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, e := server.Client().Do(req)
		if e != nil {
			return 0, nil, e
		}
		defer func() { _ = resp.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return resp.StatusCode, m, nil
	}

	// 1. Setup authenticated learner
	email := "e405-learner@example.com"
	_, _, _ = call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, "")
	verifyCode := mail.Code(email)
	_, authOut, _ := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": verifyCode}, "")
	token := authOut["access_token"].(string)

	var learnerID string
	err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID)
	if err != nil {
		t.Fatal(err)
	}

	// Set learner profile with explicit background, level, etc.
	_, err = db.Pool.Exec(ctx, `
		UPDATE learner_profiles
		SET background = 'Amateur astronomer',
		    goal = 'Learn celestial navigation',
		    level = 'beginner',
		    locale = 'en-US',
		    topic = 'astronomy'
		WHERE learner_id = $1::uuid`, learnerID)
	if err != nil {
		t.Fatal(err)
	}

	// Grant credits to learner wallet
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version)
		VALUES ($1::uuid, 1000, 'purchase', 'e405-fixture', $2)`, learnerID, cat.Config().Version)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create Course with 3 Lessons: Lesson 1 (current), Lesson 2 (locked), Lesson 3 (locked)
	var creationQuoteID, creationID string
	if err = db.Pool.QueryRow(ctx, `
		INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at)
		VALUES($1::uuid, 'seed-ref', $2, '{}', now(), now()+interval '10 minutes')
		RETURNING id::text`, learnerID, cat.Config().Version).Scan(&creationQuoteID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `
		INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state)
		VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled')
		RETURNING id::text`, learnerID, creationQuoteID).Scan(&creationID); err != nil {
		t.Fatal(err)
	}

	var courseID, unitID, l1ID, l2ID, l3ID string
	err = db.Pool.QueryRow(ctx, `
		INSERT INTO courses (learner_id, creation_id, title, description)
		VALUES ($1::uuid, $2::uuid, 'Astronomy Path', 'Celestial coordinates course')
		RETURNING id::text`, learnerID, creationID).Scan(&courseID)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = db.Pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id, course_id) VALUES($1::uuid, $2::uuid)`, learnerID, courseID)

	err = db.Pool.QueryRow(ctx, `
		INSERT INTO course_units (learner_id, course_id, authored_id, position, title)
		VALUES ($1::uuid, $2::uuid, 'u1', 0, 'Orientation')
		RETURNING id::text`, learnerID, courseID).Scan(&unitID)
	if err != nil {
		t.Fatal(err)
	}

	_ = db.Pool.QueryRow(ctx, `
		INSERT INTO course_lesson_stubs (unit_id, learner_id, authored_id, position, title, goal, status)
		VALUES ($1::uuid, $2::uuid, 'l1', 0, 'Sky Coordinates', 'Learn coordinates', 'current')
		RETURNING id::text`, unitID, learnerID).Scan(&l1ID)

	_ = db.Pool.QueryRow(ctx, `
		INSERT INTO course_lesson_stubs (unit_id, learner_id, authored_id, position, title, goal, status)
		VALUES ($1::uuid, $2::uuid, 'l2', 1, 'Finding Polaris', 'Navigate by North Star', 'locked')
		RETURNING id::text`, unitID, learnerID).Scan(&l2ID)

	_ = db.Pool.QueryRow(ctx, `
		INSERT INTO course_lesson_stubs (unit_id, learner_id, authored_id, position, title, goal, status)
		VALUES ($1::uuid, $2::uuid, 'l3', 2, 'Deep Sky', 'Locate galaxies', 'locked')
		RETURNING id::text`, unitID, learnerID).Scan(&l3ID)

	// Set up lesson 1 slides, question, and session
	_, _ = db.Pool.Exec(ctx, `
		INSERT INTO generated_lessons (lesson_stub_id, course_id, learner_id, title, manifest)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'Sky Coordinates', '{"slide_count":1,"question_count":1}')`,
		l1ID, courseID, learnerID)

	_, _ = db.Pool.Exec(ctx, `
		INSERT INTO lesson_slides (lesson_stub_id, learner_id, position, slide_id, html, duration_ms, narration, topic_ids)
		VALUES ($1::uuid, $2::uuid, 0, 's1', '<div id="root">Coordinates</div>', 3000, 'Narration', ARRAY['top-coords'])`,
		l1ID, learnerID)

	var q1ID string
	_ = db.Pool.QueryRow(ctx, `
		INSERT INTO lesson_questions (lesson_stub_id, learner_id, topic_id, position, prompt, choices, answer_index, explanation)
		VALUES ($1::uuid, $2::uuid, 'top-coords', 0, 'What is altitude?', '["Horizon","Zenith"]'::jsonb, 0, 'Altitude is from horizon.')
		RETURNING id::text`, l1ID, learnerID).Scan(&q1ID)

	// Create test session
	var sessionID string
	_ = db.Pool.QueryRow(ctx, `
		INSERT INTO lesson_test_sessions (lesson_stub_id, learner_id, state, question_queue, resolved_topics)
		VALUES ($1::uuid, $2::uuid, 'in_progress', $3::jsonb, '[]'::jsonb)
		RETURNING id::text`, l1ID, learnerID, fmt.Sprintf(`["%s"]`, q1ID)).Scan(&sessionID)

	// First submit WRONG answer to create a weak topic on 'top-coords'
	code1, _, _ := call("POST", "/v1/lessons/"+l1ID+"/test/answer", map[string]any{
		"question_id":  q1ID,
		"choice_index": 1,
	}, token)
	if code1 != 200 {
		t.Fatalf("expected 200 for wrong answer, got %d", code1)
	}

	// Now submit CORRECT answer to pass lesson 1
	code2, out2, _ := call("POST", "/v1/lessons/"+l1ID+"/test/answer", map[string]any{
		"question_id":  q1ID,
		"choice_index": 0,
	}, token)
	if code2 != 200 || out2["passed"] != true {
		t.Fatalf("expected 200 and passed=true, got %d %v", code2, out2)
	}

	// TEST A: Atomic completion, outbox, and summary with weak topics
	var outboxCount, summaryCount int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, l1ID).Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("expected 1 outbox record, got %d", outboxCount)
	}

	var weakTopicsRaw []byte
	var summaryText string
	err = db.Pool.QueryRow(ctx, "SELECT summary, weak_topics FROM lesson_completion_summaries WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, l1ID).Scan(&summaryText, &weakTopicsRaw)
	if err != nil {
		t.Fatalf("expected completed lesson summary to be persisted: %v", err)
	}
	var weakTopics []string
	_ = json.Unmarshal(weakTopicsRaw, &weakTopics)
	foundWeak := false
	for _, wt := range weakTopics {
		if wt == "top-coords" {
			foundWeak = true
			break
		}
	}
	if !foundWeak {
		t.Fatalf("expected weak topics to include 'top-coords', got %v", weakTopics)
	}

	// Verify Lesson 1 is 'done', Lesson 2 is 'ready_to_generate', Lesson 3 is still 'locked'
	var s1, s2, s3 string
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", l1ID).Scan(&s1)
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", l2ID).Scan(&s2)
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", l3ID).Scan(&s3)
	if s1 != "done" {
		t.Fatalf("expected lesson 1 status 'done', got %q", s1)
	}
	if s2 != "ready_to_generate" {
		t.Fatalf("expected lesson 2 status 'ready_to_generate', got %q", s2)
	}
	if s3 != "locked" {
		t.Fatalf("expected lesson 3 status 'locked', got %q", s3)
	}

	// TEST B: Missing predecessor pass refuses before any engine call
	// Attempting to prepare Lesson 3 (predecessor Lesson 2 is not passed) must return 409 Conflict
	_, quote3Out, _ := call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, token)
	quote3ID := quote3Out["id"].(string)
	_, _, _ = call("POST", "/v1/quotes/"+quote3ID+"/accept", nil, token) // accept quote

	prep3Code, prep3Out, _ := call("POST", "/v1/lessons/"+l3ID+"/prepare", map[string]any{
		"quote_id": quote3ID,
	}, token)
	if prep3Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict when predecessor not passed, got %d: %v", prep3Code, prep3Out)
	}
	if mockEngine.getCalls() != 0 {
		t.Fatalf("engine was called despite missing predecessor pass: %d calls", mockEngine.getCalls())
	}

	// TEST C: Duplicate pass creates no second job or charge
	dupCode, _, _ := call("POST", "/v1/lessons/"+l1ID+"/test/answer", map[string]any{
		"question_id":  q1ID,
		"choice_index": 0,
	}, token)
	if dupCode != 200 {
		t.Fatalf("expected 200 on duplicate pass submission, got %d", dupCode)
	}
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, l1ID).Scan(&outboxCount)
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM lesson_completion_summaries WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, l1ID).Scan(&summaryCount)
	if outboxCount != 1 || summaryCount != 1 {
		t.Fatalf("duplicate pass created extra outbox (%d) or summary (%d)", outboxCount, summaryCount)
	}

	// TEST D: Prepare Lesson 2 (N+1) and observe history and profile passed to stateless engine
	_, quote2Out, _ := call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, token)
	quote2ID := quote2Out["id"].(string)
	_, _, _ = call("POST", "/v1/quotes/"+quote2ID+"/accept", nil, token) // accept quote

	prep2Code, prep2Out, _ := call("POST", "/v1/lessons/"+l2ID+"/prepare", map[string]any{
		"quote_id": quote2ID,
	}, token)
	if prep2Code != http.StatusAccepted {
		t.Fatalf("expected 202 accepted for eligible lesson 2, got %d: %v", prep2Code, prep2Out)
	}
	genID := prep2Out["id"].(string)

	// Execute River worker job synchronously
	if err = lessonService.RunLessonGeneration(ctx, genID); err != nil {
		t.Fatalf("RunLessonGeneration failed: %v", err)
	}

	captured := mockEngine.getCaptured()
	if captured == nil {
		t.Fatal("engine did not receive GenerateLessonRequest")
	}

	// Verify CourseOutline passed
	if captured.Outline == nil || captured.Outline.Title != "Astronomy Path" {
		t.Fatalf("expected CourseOutline in engine request, got %v", captured.Outline)
	}

	// Verify LearnerProfile passed
	if captured.Learner == nil || captured.Learner.Background != "Amateur astronomer" {
		t.Fatalf("expected LearnerProfile in engine request, got %v", captured.Learner)
	}

	// Verify History contains completed lesson summaries oldest first
	if len(captured.History) == 0 {
		t.Fatal("expected non-empty history in engine request")
	}
	if !strings.Contains(captured.History[0], "Sky Coordinates") && !strings.Contains(captured.History[0], "top-coords") {
		t.Fatalf("history does not contain lesson 1 summary and weak topics: %v", captured.History)
	}

	// Verify Lesson 2 is now 'current'
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", l2ID).Scan(&s2)
	if s2 != "current" {
		t.Fatalf("expected lesson 2 status 'current' after generation, got %q", s2)
	}

	// TEST E: Next generation failure leaves completed lesson intact
	// Set mock engine error, prepare a failing generation for Lesson 3
	// (after marking Lesson 2 done and in outbox)
	_, _ = db.Pool.Exec(ctx, "UPDATE course_lesson_stubs SET status = 'done' WHERE id=$1::uuid", l2ID)
	_, _ = db.Pool.Exec(ctx, "INSERT INTO lesson_completion_outbox (learner_id, course_id, lesson_id, passed_at, state) VALUES ($1::uuid, $2::uuid, $3::uuid, now(), 'passed')", learnerID, courseID, l2ID)
	_, _ = db.Pool.Exec(ctx, "UPDATE course_lesson_stubs SET status = 'ready_to_generate' WHERE id=$1::uuid", l3ID)

	_, quote3FailOut, _ := call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, token)
	quote3FailID := quote3FailOut["id"].(string)
	_, _, _ = call("POST", "/v1/quotes/"+quote3FailID+"/accept", nil, token)

	prep3FailCode, prep3FailOut, _ := call("POST", "/v1/lessons/"+l3ID+"/prepare", map[string]any{
		"quote_id": quote3FailID,
	}, token)
	if prep3FailCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %v", prep3FailCode, prep3FailOut)
	}
	gen3ID := prep3FailOut["id"].(string)

	mockEngine.err = fmt.Errorf("simulated provider outage")
	_ = lessonService.RunLessonGeneration(ctx, gen3ID)

	// Verify Lesson 3 generation failed and returned to ready_to_generate
	var l3GenState string
	_ = db.Pool.QueryRow(ctx, "SELECT state FROM lesson_generations WHERE id=$1::uuid", gen3ID).Scan(&l3GenState)
	if l3GenState != "failed" {
		t.Fatalf("expected generation state 'failed', got %q", l3GenState)
	}

	// Most importantly: Lesson 1 and Lesson 2 STILL have status 'done'
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", l1ID).Scan(&s1)
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", l2ID).Scan(&s2)
	if s1 != "done" || s2 != "done" {
		t.Fatalf("next lesson generation failure undid completed lessons: l1=%s, l2=%s", s1, s2)
	}
}
