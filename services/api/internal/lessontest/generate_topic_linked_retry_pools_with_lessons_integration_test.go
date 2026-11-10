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

func TestGenerateTopicLinkedRetryPoolsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("retrypools"),
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
					Html:       `<div id="root"><h1>Rising Sun</h1><p>Earth rotates toward the east.</p></div>`,
					DurationMs: 3000,
					Narration:  "Earth rotates toward the east.",
					TopicIds:   []string{"top-sunrise"},
				},
				{
					SlideId:    "s2",
					Html:       `<div id="root"><h1>Orbits</h1><p>Planets orbit in ellipses.</p></div>`,
					DurationMs: 3000,
					Narration:  "Planets orbit in ellipses around the sun.",
					TopicIds:   []string{"top-orbits"},
				},
			},
			Test: []*enginev1.Question{
				{
					QuestionId:  "q1",
					TopicId:     "top-sunrise",
					Prompt:      "Why does the Sun rise in the east?",
					Choices:     []string{"Sun moves east", "Earth rotates east", "Earth reverses direction"},
					AnswerIndex: 1,
					Explanation: "Earth rotates east on its axis.",
				},
				{
					QuestionId:  "q2",
					TopicId:     "top-orbits",
					Prompt:      "What shape are planetary orbits?",
					Choices:     []string{"Circles", "Ellipses", "Squares"},
					AnswerIndex: 1,
					Explanation: "Planetary orbits form ellipses.",
				},
			},
			VariantPool: []*enginev1.TopicVariants{
				{
					TopicId: "top-sunrise",
					Variants: []*enginev1.Question{
						{
							QuestionId:  "v1-q1",
							TopicId:     "top-sunrise",
							VariantOf:   "q1",
							Prompt:      "You watch sunrise from a beach. What is happening?",
							Choices:     []string{"Sun climbs up", "Your side of Earth turns to Sun", "Sun accelerates"},
							AnswerIndex: 1,
							Explanation: "Earth rotation brings sunrise.",
						},
						{
							QuestionId:  "v2-q1",
							TopicId:     "top-sunrise",
							VariantOf:   "q1",
							Prompt:      "What motion produces the sunrise?",
							Choices:     []string{"Lunar shadow", "Earth spinning east", "Atmospheric bending"},
							AnswerIndex: 1,
							Explanation: "Eastward planetary spin causes apparent solar movement.",
						},
					},
				},
				{
					TopicId: "top-orbits",
					Variants: []*enginev1.Question{
						{
							QuestionId:  "v1-q2",
							TopicId:     "top-orbits",
							VariantOf:   "q2",
							Prompt:      "Kepler's first law describes orbit shapes as:",
							Choices:     []string{"Hexagons", "Elliptical paths", "Perfect spirals"},
							AnswerIndex: 1,
							Explanation: "Orbits follow Keplerian ellipses.",
						},
						{
							QuestionId:  "v2-q2",
							TopicId:     "top-orbits",
							VariantOf:   "q2",
							Prompt:      "How would you geometrically classify planetary paths?",
							Choices:     []string{"Ellipses", "Triangles", "Concentric spheres"},
							AnswerIndex: 0,
							Explanation: "Planets follow elliptical trajectories.",
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

	token := login("astronomer@example.com")
	var learnerID string
	if err := db.Pool.QueryRow(ctx, "SELECT id::text FROM learners LIMIT 1").Scan(&learnerID); err != nil {
		t.Fatal(err)
	}

	// Seed credits: 500
	if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1, 500, 'purchase', 'test-seed')`, learnerID); err != nil {
		t.Fatal(err)
	}

	// Create course with a lesson stub
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
			VALUES($1, $2::uuid, 'Astronomy')
			RETURNING id
		),
		u AS (
			INSERT INTO course_units(learner_id, course_id, authored_id, position, title)
			SELECT $1, c.id, 'u1', 0, 'Sky' FROM c
			RETURNING id, course_id
		),
		e AS (
			INSERT INTO course_enrollments(learner_id, course_id)
			SELECT $1, c.id FROM c
		),
		l1 AS (
			INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status)
			SELECT $1, u.id, 'l1', 0, 'Sun and Earth', 'Rising Sun', 'ready_to_generate' FROM u
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
		t.Fatalf("quote code %d: %v", code, quoteOut)
	}
	quoteID := quoteOut["id"].(string)

	// Accept quote
	call("POST", "/v1/quotes/"+quoteID+"/accept", nil, token)

	code, prepOut, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/prepare", lessonID), map[string]any{"quote_id": quoteID}, token)
	if code != 202 {
		t.Fatalf("prepare code %d: %v", code, prepOut)
	}
	genID := prepOut["id"].(string)

	// Run background generation job directly
	if err := lessonService.RunLessonGeneration(ctx, genID); err != nil {
		t.Fatalf("run lesson generation: %v", err)
	}

	// 2. Verify Postgres private pools are stored
	var poolCount int
	if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM lesson_retry_pools WHERE lesson_stub_id = $1::uuid`, lessonID).Scan(&poolCount); err != nil {
		t.Fatalf("query retry pools count: %v", err)
	}
	if poolCount != 2 {
		t.Fatalf("expected 2 topic retry pools in postgres, got %d", poolCount)
	}

	// 3. GET /v1/lessons/{id}/test does NOT leak private answers or explanations
	code, publicTest, rawJSON := call("GET", fmt.Sprintf("/v1/lessons/%s/test", lessonID), nil, token)
	if code != 200 {
		t.Fatalf("get test code %d", code)
	}
	if strings.Contains(rawJSON, "answer_index") || strings.Contains(rawJSON, "explanation") {
		t.Fatalf("get test response leaks private answer/explanation: %s", rawJSON)
	}

	// 4. Submit wrong answer for Q1 -> returns correction and queues retry variant
	code, answerOut, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonID), map[string]any{
		"question_id":  publicTest["questions"].([]any)[0].(map[string]any)["question_id"],
		"choice_index": 0, // Wrong choice
	}, token)
	if code != 200 {
		t.Fatalf("answer code %d: %v", code, answerOut)
	}
	if answerOut["correct"] != false {
		t.Fatalf("expected correct=false, got %v", answerOut["correct"])
	}
	if answerOut["explanation"] == "" || answerOut["explanation"] == nil {
		t.Fatalf("expected explanation on wrong answer")
	}

	// 5. Test rejection of malformed engine output (un-paraphrased variant or untaught topic)
	badLessonUntaught := &enginev1.Lesson{
		Slides: []*enginev1.Slide{
			{SlideId: "s1", Html: `<div id="root">Sky</div>`, DurationMs: 2000, Narration: "Narration text", TopicIds: []string{"topic-a"}},
		},
		Test: []*enginev1.Question{
			{QuestionId: "q1", TopicId: "topic-b", Prompt: "Prompt?", Choices: []string{"A", "B"}, AnswerIndex: 0, Explanation: "Exp"},
		},
	}
	if err := lessontest.ValidateGeneratedLesson(badLessonUntaught); err == nil {
		t.Fatalf("expected validation error for question on untaught topic")
	}
}
