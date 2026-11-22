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
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestLessonGenerationUsesCourseLevelOverrideIntegration(t *testing.T) {
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
	lessonService := lessontest.New(lessontest.Options{Objects: new(playerfixture.MemoryObjects), AssetOrigin: playerfixture.AssetOrigin,
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

	if _, err = db.Pool.Exec(ctx, `INSERT INTO learner_profiles(learner_id, level) VALUES($1::uuid, 'beginner') ON CONFLICT (learner_id) DO UPDATE SET level='beginner'`, learnerID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE course_enrollments SET level_override='advanced' WHERE learner_id=$1::uuid AND course_id=$2::uuid`, learnerID, courseID); err != nil {
		t.Fatal(err)
	}

	quoteCode, quoteResp, _ := call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, token)
	if quoteCode != 201 && quoteCode != 200 {
		t.Fatalf("quote failed: %d %v", quoteCode, quoteResp)
	}
	quoteID := quoteResp["id"].(string)
	call("POST", "/v1/quotes/"+quoteID+"/accept", nil, token)
	prepCode, prepResp, _ := call("POST", "/v1/lessons/"+lesson1ID+"/prepare", map[string]any{"quote_id": quoteID}, token)
	if prepCode != 202 {
		t.Fatalf("prepare: %d %v", prepCode, prepResp)
	}
	if err = lessonService.RunLessonGeneration(ctx, prepResp["id"].(string)); err != nil {
		t.Fatalf("generation job failed: %v", err)
	}
	if mockEngine.request == nil || mockEngine.request.Learner == nil {
		t.Fatal("engine request carried no learner profile")
	}
	if got := mockEngine.request.Learner.Level; got != "advanced" {
		t.Fatalf("engine request level = %q, want per-course override %q", got, "advanced")
	}
	_ = unitID
	_ = lesson2ID
}
