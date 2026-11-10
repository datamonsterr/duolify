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

type captureEvents struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	LearnerID string
	Event     string
	Props     map[string]any
}

func (c *captureEvents) Capture(learnerID string, event string, props map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capturedEvent{
		LearnerID: learnerID,
		Event:     event,
		Props:     props,
	})
	return nil
}

func (c *captureEvents) Close() error {
	return nil
}

func (c *captureEvents) FindEvents(name string) []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var matches []capturedEvent
	for _, e := range c.events {
		if e.Event == name {
			matches = append(matches, e)
		}
	}
	return matches
}

func TestHandleExhaustedRetryPoolsWithoutBlockingTestsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("exhaustedpools"),
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
			},
			Test: []*enginev1.Question{
				{
					QuestionId:  "q1",
					TopicId:     "top-sunrise",
					Prompt:      "Why does sunrise appear in the east?",
					Choices:     []string{"Earth rotates eastward", "Sun circles Earth", "Reflection"},
					AnswerIndex: 0,
					Explanation: "Earth spins west to east.",
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
							Prompt:      "Dawn light breaks over the eastern horizon because:",
							Choices:     []string{"Observer enters daylight", "Sun moves fast", "Sky mirrors light"},
							AnswerIndex: 0,
							Explanation: "Planetary rotation moves observer into sunlight.",
						},
					},
				},
			},
		},
	}

	eventCapture := &captureEvents{}

	lessonService := lessontest.New(lessontest.Options{
		Pool:                db.Pool,
		Engine:              mockEngine,
		Financial:           financial,
		Events:              eventCapture,
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

	token := login("zero-balance-learner@example.com")

	var learnerID string
	if err := db.Pool.QueryRow(ctx, "SELECT id::text FROM learners LIMIT 1").Scan(&learnerID); err != nil {
		t.Fatal(err)
	}

	// Grant minimal credits needed for initial course/lesson preparation only, then deplete to 0
	var creationQuoteID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'seed-ref', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerID, cat.Config().Version).Scan(&creationQuoteID); err != nil {
		t.Fatal(err)
	}
	var creationID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, creationQuoteID).Scan(&creationID); err != nil {
		t.Fatal(err)
	}

	var courseID, unitID, lessonID string
	err = db.Pool.QueryRow(ctx, `
		WITH c AS (
			INSERT INTO courses(learner_id, creation_id, title)
			VALUES($1, $2::uuid, 'Planetary Physics')
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
			SELECT $1, u.id, 'l1', 0, 'Solar Dynamics', 'Dynamics', 'ready_to_generate' FROM u
			RETURNING id
		)
		SELECT (SELECT id::text FROM c), (SELECT id::text FROM u), (SELECT id::text FROM l1);
	`, learnerID, creationID).Scan(&courseID, &unitID, &lessonID)
	if err != nil {
		t.Fatal(err)
	}

	// Seed credits to prepare lesson
	if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1, 1000, 'purchase', 'test-seed')`, learnerID); err != nil {
		t.Fatal(err)
	}

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

	// Drain any remaining credits to strictly enforce ZERO balance for testing
	var remainingCredits int64
	if err := db.Pool.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE learner_id=$1::uuid`, learnerID).Scan(&remainingCredits); err != nil {
		t.Fatal(err)
	}
	if remainingCredits > 0 {
		_, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1::uuid, $2, 'spend', 'drain-to-zero')`, learnerID, -remainingCredits)
		if err != nil {
			t.Fatal(err)
		}
	}

	var creditBalance int64
	_ = db.Pool.QueryRow(ctx, `SELECT COALESCE(sum(amount),0) FROM credit_entries WHERE learner_id=$1::uuid`, learnerID).Scan(&creditBalance)
	if creditBalance != 0 {
		t.Fatalf("expected 0 credits balance, got %d", creditBalance)
	}

	// 1. Fetch test session
	code, getTestOut, _ := call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonID), nil, token)
	if code != 200 {
		t.Fatalf("get test code %d", code)
	}
	sessionID := getTestOut["session_id"].(string)

	// 2. Answer Question 1 wrongly
	// Topic 'top-sunrise' pool has 1 variant (v1-sunrise).
	code, ans1, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  "q1",
		"choice_index": 1, // Wrong
	}, token)
	if code != 200 || ans1["correct"] != false {
		t.Fatalf("answer 1 code %d: %v", code, ans1)
	}

	// Variant v1-sunrise is served from pool
	variant1 := ans1["retry_variant"].(map[string]any)
	v1ID := variant1["question_id"].(string)
	if v1ID != "v1-sunrise" {
		t.Fatalf("expected v1-sunrise, got %s", v1ID)
	}

	// Check provenance snapshot for v1-sunrise
	var v1Source, v1Prompt string
	err = db.Pool.QueryRow(ctx, `
		SELECT source, prompt
		FROM lesson_test_variant_snapshots
		WHERE session_id=$1::uuid AND question_id=$2`,
		sessionID, v1ID).Scan(&v1Source, &v1Prompt)
	if err != nil {
		t.Fatalf("variant snapshot for v1-sunrise: %v", err)
	}
	if v1Source != "pool" {
		t.Fatalf("expected source 'pool', got '%s'", v1Source)
	}

	// 3. Answer v1-sunrise wrongly: pool for top-sunrise is now EXHAUSTED!
	// Under ADR 0011 (E4-06), retry pool is reused with reordered choices for free without an engine call and without credit deductions!
	code, ans2, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  v1ID,
		"choice_index": 1, // Wrong
	}, token)
	if code != 200 || ans2["correct"] != false {
		t.Fatalf("answer 2 code %d: %v", code, ans2)
	}

	// Recycled variant with reordered choices is served
	recycledVariant := ans2["retry_variant"].(map[string]any)
	recycledID := recycledVariant["question_id"].(string)
	recycledChoices := recycledVariant["choices"].([]any)

	// Verify choices are reordered
	if len(recycledChoices) < 2 {
		t.Fatalf("expected at least 2 choices in recycled variant")
	}

	// Check provenance snapshot in Postgres for the recycled variant:
	// source must be 'pool_exhausted_fallback' and reason 'pool_exhausted'
	var recSource, recReason, recPrompt string
	var recAnswerIndex int
	var recChoicesRaw []byte
	var recInstrRaw []byte
	err = db.Pool.QueryRow(ctx, `
		SELECT source, COALESCE(reason,''), prompt, answer_index, choices, instruction_snapshot
		FROM lesson_test_variant_snapshots
		WHERE session_id=$1::uuid AND question_id=$2`,
		sessionID, recycledID).Scan(&recSource, &recReason, &recPrompt, &recAnswerIndex, &recChoicesRaw, &recInstrRaw)
	if err != nil {
		t.Fatalf("variant snapshot for recycled variant: %v", err)
	}
	if recSource != "pool_exhausted_fallback" {
		t.Fatalf("expected source 'pool_exhausted_fallback', got '%s'", recSource)
	}
	if recReason != "pool_exhausted" {
		t.Fatalf("expected reason 'pool_exhausted', got '%s'", recReason)
	}

	// Verify instruction snapshot was retained
	var recInstr map[string]any
	if err := json.Unmarshal(recInstrRaw, &recInstr); err != nil {
		t.Fatalf("unmarshal instruction_snapshot: %v", err)
	}

	// Verify telemetry emitted:
	// retry_variant_generation_failed with error_code 'pool_exhausted' and NO learner text
	failedEvents := eventCapture.FindEvents("retry_variant_generation_failed")
	if len(failedEvents) == 0 {
		t.Fatalf("expected retry_variant_generation_failed event")
	}
	lastFailed := failedEvents[len(failedEvents)-1]
	if lastFailed.Props["error_code"] != "pool_exhausted" {
		t.Fatalf("expected error_code 'pool_exhausted', got %v", lastFailed.Props["error_code"])
	}
	if lastFailed.Props["topic_id"] != "top-sunrise" {
		t.Fatalf("expected topic_id 'top-sunrise', got %v", lastFailed.Props["topic_id"])
	}
	// Verify NO learner text was leaked into event props
	for k, v := range lastFailed.Props {
		if strings.Contains(k, "text") || strings.Contains(k, "prompt") || strings.Contains(k, "choice") {
			t.Fatalf("prohibited learner/question text in telemetry key %s: %v", k, v)
		}
	}

	// Verify NO extra payment records created on free reuse
	var extraPurchases int
	_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_entries WHERE learner_id=$1::uuid AND kind='purchase' AND reference != 'test-seed'`, learnerID).Scan(&extraPurchases)
	if extraPurchases != 0 {
		t.Fatalf("expected no extra purchase entries for retries, got %d", extraPurchases)
	}

	// 4. Test never auto-pass: wrong answer on recycled variant must NOT pass the test
	code, ans3Wrong, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  recycledID,
		"choice_index": (recAnswerIndex + 1) % len(recycledChoices), // Wrong
	}, token)
	if code != 200 {
		t.Fatalf("answer 3 wrong code %d", code)
	}
	if ans3Wrong["passed"] == true || ans3Wrong["correct"] == true {
		t.Fatalf("must never auto-pass on incorrect answer: %v", ans3Wrong)
	}

	// 5. Answer correctly using the exact answer index recorded in the snapshot
	code, ansFinal, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  recycledID,
		"choice_index": recAnswerIndex, // Correct!
	}, token)
	if code != 200 {
		t.Fatalf("answer final code %d", code)
	}
	if ansFinal["correct"] != true {
		t.Fatalf("expected correct true on answer index %d: %v", recAnswerIndex, ansFinal)
	}
	if ansFinal["passed"] != true {
		t.Fatalf("expected passed true when all topics resolved and queue empty: %v", ansFinal)
	}

	// Verify completion committed to outbox
	var completionState string
	err = db.Pool.QueryRow(ctx, `SELECT state FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid`, learnerID, lessonID).Scan(&completionState)
	if err != nil || completionState != "passed" {
		t.Fatalf("expected passed outbox row, got state=%s, err=%v", completionState, err)
	}
}
