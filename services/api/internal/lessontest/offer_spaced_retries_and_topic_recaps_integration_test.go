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

func TestOfferSpacedRetriesAndTopicRecapsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("spacedretries"),
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
				{
					SlideId:    "s3",
					Html:       `<div id="root"><h1>Gravity Wells</h1><p>Mass curves local spacetime.</p></div>`,
					DurationMs: 3000,
					Narration:  "Mass curves spacetime.",
					TopicIds:   []string{"top-gravity"},
				},
			},
			Test: []*enginev1.Question{
				{
					QuestionId:  "q1",
					TopicId:     "top-sunrise",
					Prompt:      "Why does sunrise appear in the east?",
					Choices:     []string{"Earth rotates eastward", "Sun circles west to east", "Reflected light"},
					AnswerIndex: 0,
					Explanation: "Earth spins west to east.",
				},
				{
					QuestionId:  "q2",
					TopicId:     "top-orbits",
					Prompt:      "What geometric shape are planetary orbits?",
					Choices:     []string{"Perfect spheres", "Ellipses", "Straight vectors"},
					AnswerIndex: 1,
					Explanation: "Planetary orbits are ellipses.",
				},
				{
					QuestionId:  "q3",
					TopicId:     "top-gravity",
					Prompt:      "How does mass produce gravitational effects?",
					Choices:     []string{"Curving spacetime", "Spinning charges", "Magnetic poles"},
					AnswerIndex: 0,
					Explanation: "Mass curves spacetime.",
				},
			},
			VariantPool: []*enginev1.TopicVariants{
				{
					TopicId: "top-sunrise",
					Variants: []*enginev1.Question{
						{
							QuestionId:  "v1-sunrise",
							TopicId:     "top-sunrise",
							VariantOf:   "q1",
							Prompt:      "Dawn light breaks over the eastern rim because:",
							Choices:     []string{"Observer location enters daylight", "Sun moves fast", "Sky mirrors light"},
							AnswerIndex: 0,
							Explanation: "Planetary rotation moves observer into sunlight.",
						},
						{
							QuestionId:  "v2-sunrise",
							TopicId:     "top-sunrise",
							VariantOf:   "q1",
							Prompt:      "East-facing beach mornings show sunrise due to:",
							Choices:     []string{"Sun climbing", "Our planet side turning to the star", "Atmospheric shift"},
							AnswerIndex: 1,
							Explanation: "Planet rotation brings us to the Sun.",
						},
						{
							QuestionId:  "v3-sunrise",
							TopicId:     "top-sunrise",
							VariantOf:   "q1",
							Prompt:      "Why is sunrise east rather than west?",
							Choices:     []string{"Eastward rotational direction", "Sun solar drift", "Tidal friction"},
							AnswerIndex: 0,
							Explanation: "Earth spins toward the east.",
						},
						{
							QuestionId:  "v4-sunrise",
							TopicId:     "top-sunrise",
							VariantOf:   "q1",
							Prompt:      "The apparent celestial progression of the morning Sun is driven by:",
							Choices:     []string{"Earth rotational momentum", "Solar orbital sweep", "Axis wobble"},
							AnswerIndex: 0,
							Explanation: "Rotational momentum toward the east.",
						},
					},
				},
			},
		},
	}

	lessonService := lessontest.New(lessontest.Options{
		Pool:                db.Pool,
		Engine:              mockEngine,
		Financial:           financial,
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

	token := login("spaced-retries@example.com")
	var learnerID string
	if err := db.Pool.QueryRow(ctx, "SELECT id::text FROM learners LIMIT 1").Scan(&learnerID); err != nil {
		t.Fatal(err)
	}

	// Seed credits
	if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1, 1000, 'purchase', 'test-seed')`, learnerID); err != nil {
		t.Fatal(err)
	}

	// Prepare course and lesson
	var creationQuoteID, creationID string
	if err := db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'seed-ref', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerID, cat.Config().Version).Scan(&creationQuoteID); err != nil {
		t.Fatal(err)
	}
	if err := db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, creationQuoteID).Scan(&creationID); err != nil {
		t.Fatal(err)
	}

	var courseID, unitID, lessonID string
	err = db.Pool.QueryRow(ctx, `
		WITH c AS (
			INSERT INTO courses(learner_id, creation_id, title)
			VALUES($1, $2::uuid, 'Astrophysics')
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
		l1 AS (
			INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status)
			SELECT $1, u.id, 'l1', 0, 'Solar and Planetary Dynamics', 'Dynamics', 'ready_to_generate' FROM u
			RETURNING id
		)
		SELECT (SELECT id::text FROM c), (SELECT id::text FROM u), (SELECT id::text FROM l1);
	`, learnerID, creationID).Scan(&courseID, &unitID, &lessonID)
	if err != nil {
		t.Fatal(err)
	}

	// 1. Prepare and generate lesson
	code, quoteOut, _ := call("POST", "/v1/quotes", map[string]any{
		"action":               "lesson_content",
		"narration_characters": 100,
	}, token)
	if code != 201 && code != 200 {
		t.Fatalf("quote code %d", code)
	}
	quoteID := quoteOut["id"].(string)
	call("POST", "/v1/quotes/"+quoteID+"/accept", nil, token)

	code, prepOut, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/prepare", lessonID), map[string]any{"quote_id": quoteID}, token)
	if code != 202 {
		t.Fatalf("prepare code %d", code)
	}
	genID := prepOut["id"].(string)

	if err := lessonService.RunLessonGeneration(ctx, genID); err != nil {
		t.Fatalf("run lesson generation: %v", err)
	}

	// 2. Fetch test session: returns 3 initial questions
	code, getTestOut, _ := call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonID), nil, token)
	if code != 200 {
		t.Fatalf("get test code %d", code)
	}
	questionsRaw := getTestOut["questions"].([]any)
	if len(questionsRaw) != 3 {
		t.Fatalf("expected 3 questions, got %d", len(questionsRaw))
	}

	q1ID := questionsRaw[0].(map[string]any)["question_id"]
	q2ID := questionsRaw[1].(map[string]any)["question_id"]
	q3ID := questionsRaw[2].(map[string]any)["question_id"]

	// 3. Spaced retry persistence:
	// Answer Q1 (top-sunrise) wrongly.
	// Remaining questions were Q2, Q3 (count = 2). min_gap = 2.
	// So variant for top-sunrise MUST be inserted after Q2 and Q3 (at position 2 of remaining).
	code, ans1, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  q1ID,
		"choice_index": 1, // Wrong
	}, token)
	if code != 200 {
		t.Fatalf("answer 1 code %d: %v", code, ans1)
	}
	if ans1["correct"] != false {
		t.Fatalf("expected correct false")
	}

	// Verify session question_queue in Postgres: queue must persist [Q2, Q3, Variant1]
	var queueJSON []byte
	if err := db.Pool.QueryRow(ctx, `SELECT question_queue FROM lesson_test_sessions WHERE lesson_stub_id=$1::uuid AND state='in_progress'`, lessonID).Scan(&queueJSON); err != nil {
		t.Fatalf("query question_queue: %v", err)
	}
	var persistedQueue []map[string]any
	if err := json.Unmarshal(queueJSON, &persistedQueue); err != nil {
		t.Fatalf("unmarshal persistedQueue: %v", err)
	}
	if len(persistedQueue) != 3 {
		t.Fatalf("expected 3 items in persistedQueue, got %d", len(persistedQueue))
	}
	if persistedQueue[0]["question_id"] != q2ID || persistedQueue[1]["question_id"] != q3ID {
		t.Fatalf("expected Q2 and Q3 before variant, got %v, %v", persistedQueue[0]["question_id"], persistedQueue[1]["question_id"])
	}
	if persistedQueue[2]["is_variant"] != true || persistedQueue[2]["topic_id"] != "top-sunrise" {
		t.Fatalf("expected variant at index 2, got %v", persistedQueue[2])
	}

	// Verify resuming session via GET /v1/lessons/{id}/test returns the persisted spaced queue!
	code, resumeTestOut, _ := call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonID), nil, token)
	if code != 200 {
		t.Fatalf("resume test code %d", code)
	}
	resumedQuestions := resumeTestOut["questions"].([]any)
	if len(resumedQuestions) != 3 || resumedQuestions[0].(map[string]any)["question_id"] != q2ID {
		t.Fatalf("resumed test queue does not match persisted queue: %v", resumedQuestions)
	}

	// 4. Concurrent submissions test:
	// Two concurrent submissions answering Q2 must be safely serialized by the server
	var wg sync.WaitGroup
	var concurrentStatusCodes [2]int
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c, _, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
				"question_id":  q2ID,
				"choice_index": 1, // Correct answer
			}, token)
			concurrentStatusCodes[idx] = c
		}(i)
	}
	wg.Wait()

	// At least one succeeded, neither corrupted the database
	hasSuccess := false
	for _, code := range concurrentStatusCodes {
		if code == 200 {
			hasSuccess = true
		}
	}
	if !hasSuccess {
		t.Fatalf("expected at least one 200 from concurrent submissions, got %v", concurrentStatusCodes)
	}

	// 5. Answer Q3 correctly
	code, ans3, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  q3ID,
		"choice_index": 0, // Correct
	}, token)
	if code != 200 || ans3["correct"] != true {
		t.Fatalf("expected Q3 correct, got %d, %v", code, ans3)
	}

	// Now only top-sunrise is unresolved.
	// Miss variant 1
	code, ansV1, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  ans1["retry_variant"].(map[string]any)["question_id"],
		"choice_index": 1, // Wrong
	}, token)
	if code != 200 || ansV1["correct"] != false {
		t.Fatalf("expected V1 wrong, got %d, %v", code, ansV1)
	}

	// Miss variant 2
	code, ansV2, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  ansV1["retry_variant"].(map[string]any)["question_id"],
		"choice_index": 0, // Wrong
	}, token)
	if code != 200 || ansV2["correct"] != false {
		t.Fatalf("expected V2 wrong, got %d, %v", code, ansV2)
	}

	// Miss variant 3 (this is the 3rd consecutive variant wrong answer on top-sunrise)
	// Must offer taught slide recap!
	code, ansV3, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  ansV2["retry_variant"].(map[string]any)["question_id"],
		"choice_index": 1, // Wrong
	}, token)
	if code != 200 {
		t.Fatalf("ansV3 code %d", code)
	}
	if ansV3["offer_recap"] != true {
		t.Fatalf("expected offer_recap=true after max_variants_per_topic=3 wrong variants, got %v", ansV3["offer_recap"])
	}
	recapSlides, ok := ansV3["recap_slides"].([]any)
	if !ok || len(recapSlides) == 0 {
		t.Fatalf("expected recap_slides to contain taught slides for top-sunrise, got %v", ansV3["recap_slides"])
	}

	// Recap obligation: recap does NOT resolve the topic!
	if ansV3["passed"] == true {
		t.Fatalf("recap must not pass or complete the test")
	}

	// Verify topic remains unresolved
	var resolvedJSON []byte
	_ = db.Pool.QueryRow(ctx, `SELECT resolved_topics FROM lesson_test_sessions WHERE lesson_stub_id=$1::uuid`, lessonID).Scan(&resolvedJSON)
	var currentResolved []string
	_ = json.Unmarshal(resolvedJSON, &currentResolved)
	for _, top := range currentResolved {
		if top == "top-sunrise" {
			t.Fatalf("top-sunrise was incorrectly marked resolved after recap")
		}
	}

	// Now learner answers the final variant correctly
	code, ansV4, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  ansV3["retry_variant"].(map[string]any)["question_id"],
		"choice_index": 0, // Correct
	}, token)
	if code != 200 {
		t.Fatalf("ansV4 code %d", code)
	}
	if ansV4["correct"] != true || ansV4["passed"] != true {
		t.Fatalf("expected final answer correct and passed=true, got %v", ansV4)
	}
}
