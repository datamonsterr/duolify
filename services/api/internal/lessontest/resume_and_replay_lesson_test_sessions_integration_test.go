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

func TestResumeAndReplayLessonTestSessionsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("resumereplay"),
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
					Html:       `<div id="root"><h1>Eastward Spin</h1><p>Earth rotates eastward into sunlight.</p></div>`,
					DurationMs: 3000,
					Narration:  "Earth rotates eastward into sunlight.",
					TopicIds:   []string{"top-sunrise"},
				},
				{
					SlideId:    "s2",
					Html:       `<div id="root"><h1>Kepler Orbits</h1><p>Planets orbit in elliptical paths.</p></div>`,
					DurationMs: 3000,
					Narration:  "Planets orbit in elliptical paths.",
					TopicIds:   []string{"top-orbits"},
				},
			},
			Test: []*enginev1.Question{
				{
					QuestionId:  "q1",
					TopicId:     "top-sunrise",
					Prompt:      "What makes the Sun rise in the east?",
					Choices:     []string{"The Sun orbits Earth daily", "Earth spins west to east", "The Moon pushes it up"},
					AnswerIndex: 1,
					Explanation: "Earth spins west to east.",
				},
				{
					QuestionId:  "q2",
					TopicId:     "top-orbits",
					Prompt:      "What shape are planetary orbits?",
					Choices:     []string{"Perfect circles", "Ellipses", "Straight lines"},
					AnswerIndex: 1,
					Explanation: "Planetary orbits are ellipses.",
				},
			},
			VariantPool: []*enginev1.TopicVariants{
				{TopicId: "top-orbits", Variants: []*enginev1.Question{{QuestionId: "v-orbits", TopicId: "top-orbits", VariantOf: "q2", Prompt: "Which curve describes a planet's path around its star?", Choices: []string{"Ellipses", "Straight lines", "Perfect circles"}, AnswerIndex: 0, Explanation: "Planetary paths are ellipses."}}},
				{
					TopicId: "top-sunrise",
					Variants: []*enginev1.Question{
						{
							QuestionId:  "v1-sunrise",
							TopicId:     "top-sunrise",
							VariantOf:   "q1",
							Prompt:      "You watch a sunrise from a beach facing east. What is really happening?",
							Choices:     []string{"The Sun is climbing out of the sea", "Your side of Earth is turning toward the Sun", "The Sun speeds up each morning"},
							AnswerIndex: 1,
							Explanation: "Your side of Earth is turning toward the Sun.",
						},
					},
				},
			},
		},
	}

	ttl := 7 * 24 * time.Hour
	lessonService := lessontest.New(lessontest.Options{
		Pool:                db.Pool,
		Engine:              mockEngine,
		Financial:           financial,
		SessionTTL:          ttl,
		MinGap:              2,
		MaxVariantsPerTopic: 3,
		MaxVariantsPerTest:  12,
	})

	coursesService := courses.New(courses.Options{Pool: db.Pool, Financial: financial})
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
			buf, _ := json.Marshal(body)
			r = bytes.NewReader(buf)
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

	token := login("learner1@example.com")
	var learnerID string
	if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email='learner1@example.com'").Scan(&learnerID); err != nil {
		t.Fatal(err)
	}

	token2 := login("learner2@example.com")
	var learner2ID string
	if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email='learner2@example.com'").Scan(&learner2ID); err != nil {
		t.Fatal(err)
	}

	// Seed credits
	if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1, 1000, 'purchase', 'test-seed')`, learnerID); err != nil {
		t.Fatal(err)
	}

	// Create course, unit, lesson stub
	var creationQuoteID, creationID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'seed-ref', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerID, cat.Config().Version).Scan(&creationQuoteID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, creationQuoteID).Scan(&creationID); err != nil {
		t.Fatal(err)
	}

	var courseID, unitID, lessonID string
	err = db.Pool.QueryRow(ctx, `
		WITH c AS (
			INSERT INTO courses(learner_id, creation_id, title)
			VALUES($1, $2::uuid, 'Astronomy')
			RETURNING id
		),
		u AS (
			INSERT INTO course_units(learner_id, course_id, authored_id, position, title)
			SELECT $1, c.id, 'u1', 0, 'Mechanics' FROM c
			RETURNING id, course_id
		),
		e AS (
			INSERT INTO course_enrollments(learner_id, course_id)
			SELECT $1, c.id FROM c
		),
		l AS (
			INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status)
			SELECT $1, u.id, 'l1', 0, 'Sun and Earth', 'Learn sun and earth', 'ready_to_generate' FROM u
			RETURNING id
		)
		SELECT (SELECT id::text FROM c), (SELECT id::text FROM u), (SELECT id::text FROM l);
	`, learnerID, creationID).Scan(&courseID, &unitID, &lessonID)
	if err != nil {
		t.Fatal(err)
	}

	// Prepare lesson
	quoteCode, quoteResp, _ := call("POST", "/v1/quotes", map[string]any{
		"action":               "lesson_content",
		"narration_characters": 100,
	}, token)
	if quoteCode != 201 && quoteCode != 200 {
		t.Fatalf("quote failed: %d %v", quoteCode, quoteResp)
	}
	quoteID := quoteResp["id"].(string)
	call("POST", "/v1/quotes/"+quoteID+"/accept", nil, token)

	prepCode, prepResp, _ := call("POST", "/v1/lessons/"+lessonID+"/prepare", map[string]any{
		"quote_id": quoteID,
	}, token)
	if prepCode != 202 {
		t.Fatalf("expected 202 for lesson prepare, got %d %v", prepCode, prepResp)
	}
	genID := prepResp["id"].(string)

	if err = lessonService.RunLessonGeneration(ctx, genID); err != nil {
		t.Fatalf("generation job failed: %v", err)
	}

	courseCount := 0
	createAndGenerateLesson := func(title string) string {
		courseCount++
		var courseQuoteID, cCreationID, cID, uID, lID string
		if err := db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, $2, $3, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerID, fmt.Sprintf("ref-%d", courseCount), cat.Config().Version).Scan(&courseQuoteID); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, courseQuoteID).Scan(&cCreationID); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `
			INSERT INTO courses(learner_id, creation_id, title)
			VALUES($1, $2::uuid, $3)
			RETURNING id::text`, learnerID, cCreationID, title).Scan(&cID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `
			INSERT INTO course_enrollments(learner_id, course_id)
			VALUES($1, $2::uuid)`, learnerID, cID); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `
			INSERT INTO course_units(learner_id, course_id, authored_id, position, title)
			VALUES($1, $2::uuid, 'u1', 0, 'Unit 1')
			RETURNING id::text`, learnerID, cID).Scan(&uID); err != nil {
			t.Fatal(err)
		}
		if err := db.Pool.QueryRow(ctx, `
			INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status)
			VALUES($1, $2::uuid, 'l1', 0, $3, 'Goal', 'ready_to_generate')
			RETURNING id::text`, learnerID, uID, title).Scan(&lID); err != nil {
			t.Fatal(err)
		}
		qCode, qResp, _ := call("POST", "/v1/quotes", map[string]any{
			"action":               "lesson_content",
			"narration_characters": 100,
		}, token)
		if qCode != 200 && qCode != 201 {
			t.Fatalf("quote failed: %d %v", qCode, qResp)
		}
		qID := qResp["id"].(string)
		call("POST", "/v1/quotes/"+qID+"/accept", nil, token)
		pCode, pResp, _ := call("POST", "/v1/lessons/"+lID+"/prepare", map[string]any{
			"quote_id": qID,
		}, token)
		if pCode != 202 {
			t.Fatalf("prepare failed: %d %v", pCode, pResp)
		}
		genID := pResp["id"].(string)
		if err := lessonService.RunLessonGeneration(ctx, genID); err != nil {
			t.Fatalf("generation failed: %v", err)
		}
		return lID
	}

	t.Run("PostgresAnswerOperationKeys_IdempotencyAndForeignRejection", func(t *testing.T) {
		// 1. Fetch test
		code, testData, raw := call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonID), nil, token)
		if code != 200 {
			t.Fatalf("get test failed: %d, %s", code, raw)
		}
		questions := testData["questions"].([]any)
		if len(questions) != 2 {
			t.Fatalf("expected 2 questions, got %d", len(questions))
		}

		opKey1 := "op-test-key-001"
		// 2. Submit answer to Q1 with operation_key
		ansBody := map[string]any{
			"question_id":   "q1",
			"choice_index":  1, // correct
			"operation_key": opKey1,
		}
		code, res1, raw := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), ansBody, token)
		if code != 200 {
			t.Fatalf("answer failed: %d, %s", code, raw)
		}
		if res1["correct"] != true {
			t.Fatalf("expected correct=true, got %v", res1["correct"])
		}

		// Verify row in lesson_test_answer_operations
		var savedOpKey string
		var savedPayload []byte
		err := db.Pool.QueryRow(ctx, `SELECT operation_key, response_payload FROM lesson_test_answer_operations WHERE operation_key=$1`, opKey1).Scan(&savedOpKey, &savedPayload)
		if err != nil {
			t.Fatalf("expected operation row saved in lesson_test_answer_operations, got %v", err)
		}

		// 3. Replay with exact same operation_key
		code, res2, raw := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), ansBody, token)
		if code != 200 {
			t.Fatalf("replay failed: %d, %s", code, raw)
		}
		if res2["correct"] != true {
			t.Fatalf("expected replay correct=true, got %v", res2["correct"])
		}

		// Verify answer row count did not duplicate
		var ansCount int
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM lesson_test_answers WHERE session_id=$1::uuid`, testData["session_id"].(string)).Scan(&ansCount)
		if ansCount != 1 {
			t.Fatalf("expected 1 answer row, got %d", ansCount)
		}

		// 4. Foreign operation key check: learner2 tries to reuse opKey1
		code, _, _ = call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), ansBody, token2)
		if code != http.StatusBadRequest {
			t.Fatalf("expected 400 for foreign operation key, got %d", code)
		}
	})

	t.Run("DuplicateFinalAnswer_RecordsOneCompletion", func(t *testing.T) {
		opKeyFinal := "op-test-final-002"
		finalBody := map[string]any{
			"question_id":   "q2",
			"choice_index":  1, // correct
			"operation_key": opKeyFinal,
		}

		// First final answer submission
		code, res1, raw := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), finalBody, token)
		if code != 200 {
			t.Fatalf("final answer failed: %d, %s", code, raw)
		}
		if res1["passed"] != true {
			t.Fatalf("expected passed=true, got %v", res1["passed"])
		}

		// Duplicate final answer submission with same operation key
		code, res2, raw := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), finalBody, token)
		if code != 200 {
			t.Fatalf("duplicate final answer failed: %d, %s", code, raw)
		}
		if res2["passed"] != true {
			t.Fatalf("expected duplicate passed=true, got %v", res2["passed"])
		}

		// Assert exactly one row in lesson_completion_outbox
		var outboxCount int
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM lesson_completion_outbox WHERE lesson_id=$1::uuid`, lessonID).Scan(&outboxCount)
		if outboxCount != 1 {
			t.Fatalf("expected 1 completion outbox row, got %d", outboxCount)
		}
	})

	t.Run("UnservedKeyRejection", func(t *testing.T) {
		lessonUnservedID := createAndGenerateLesson("Unserved Test")

		// Start session
		call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonUnservedID), nil, token)

		// Attempt to answer unserved question
		code, resp, raw := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonUnservedID), map[string]any{
			"question_id":  "unserved-question-999",
			"choice_index": 0,
		}, token)

		if code != http.StatusBadRequest && code != http.StatusNotFound {
			t.Fatalf("expected 400 or 404 for unserved question, got %d (%s)", code, raw)
		}
		// Crucial security / product invariant: MUST NOT leak answer_index or explanation
		if resp["answer_index"] != nil || resp["explanation"] != nil {
			t.Fatalf("security violation: unserved key leaked answer_index or explanation: %v", resp)
		}
	})

	t.Run("SessionTTL_TimeTravel_AbandonsAndReordersWithoutCompletion", func(t *testing.T) {
		lessonTTLID := createAndGenerateLesson("TTL Test")

		// 1. Initial test fetch creates first session
		code, initData, raw := call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonTTLID), nil, token)
		if code != 200 {
			t.Fatalf("get test failed: %d, %s", code, raw)
		}
		sessID1 := initData["session_id"].(string)
		initialQuestions := initData["questions"].([]any)
		firstQ1 := initialQuestions[0].(map[string]any)["question_id"].(string)

		// 2. Simulate time travel: 8 days pass (> 7 days TTL)
		_, err := db.Pool.Exec(ctx, `
			UPDATE lesson_test_sessions
			SET created_at = now() - interval '8 days', updated_at = now() - interval '8 days'
			WHERE id = $1::uuid`, sessID1)
		if err != nil {
			t.Fatal(err)
		}

		// 3. Learner returns next day / after TTL
		code, renewedData, raw := call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonTTLID), nil, token)
		if code != 200 {
			t.Fatalf("get test after TTL failed: %d, %s", code, raw)
		}
		sessID2 := renewedData["session_id"].(string)
		if sessID1 == sessID2 {
			t.Fatalf("expected new session after TTL expiry, but got same session_id %s", sessID1)
		}

		// Verify old session is marked abandoned
		var oldState string
		_ = db.Pool.QueryRow(ctx, `SELECT state FROM lesson_test_sessions WHERE id = $1::uuid`, sessID1).Scan(&oldState)
		if oldState != "abandoned" {
			t.Fatalf("expected old session state='abandoned', got %q", oldState)
		}

		// Verify new session is in_progress
		var newState string
		_ = db.Pool.QueryRow(ctx, `SELECT state FROM lesson_test_sessions WHERE id = $1::uuid`, sessID2).Scan(&newState)
		if newState != "in_progress" {
			t.Fatalf("expected new session state='in_progress', got %q", newState)
		}

		// Verify new session questions are reordered original questions
		newQuestions := renewedData["questions"].([]any)
		if len(newQuestions) != len(initialQuestions) {
			t.Fatalf("expected same question count %d, got %d", len(initialQuestions), len(newQuestions))
		}
		// Order must be different
		firstQ2 := newQuestions[0].(map[string]any)["question_id"].(string)
		if firstQ1 == firstQ2 && len(newQuestions) > 1 {
			t.Fatalf("expected reordered questions in new session, but first question is unchanged (%s)", firstQ1)
		}

		// Verify completion was NOT granted
		var completionExists bool
		_ = db.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lesson_completion_outbox WHERE lesson_id=$1::uuid)`, lessonTTLID).Scan(&completionExists)
		if completionExists {
			t.Fatalf("abandoned session should NOT grant completion")
		}
	})

	t.Run("MultiDeviceConcurrentSubmissions_SerializeCleanly", func(t *testing.T) {
		lessonConcurID := createAndGenerateLesson("Concur Test")
		call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonConcurID), nil, token)

		var wg sync.WaitGroup
		results := make([]int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				op := fmt.Sprintf("op-concur-%d", idx)
				c, _, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonConcurID), map[string]any{
					"question_id":   "q1",
					"choice_index":  1,
					"operation_key": op,
				}, token)
				results[idx] = c
			}(i)
		}
		wg.Wait()

		for i, c := range results {
			if c != 200 && c != 400 {
				t.Errorf("device %d returned unexpected status: %d", i, c)
			}
		}
	})
}
