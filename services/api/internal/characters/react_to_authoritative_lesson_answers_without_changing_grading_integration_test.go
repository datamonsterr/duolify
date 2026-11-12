//go:build integration

package characters_test

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

type reactionEngine struct{}

func (reactionEngine) GenerateLesson(context.Context, *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error) {
	q := func(id, variantOf, prompt string) *enginev1.Question {
		return &enginev1.Question{QuestionId: id, TopicId: "top-stars", VariantOf: variantOf, Prompt: prompt, Choices: []string{"Stars", "Sun", "Clouds"}, AnswerIndex: 0, Explanation: "Stars shine."}
	}
	return &enginev1.Lesson{
		Slides:      []*enginev1.Slide{{SlideId: "s1", Html: `<div id="root"><h1>Sky</h1></div>`, DurationMs: 3000, Narration: "Stars tonight.", TopicIds: []string{"top-stars"}}},
		Test:        []*enginev1.Question{q("q1", "", "What shines?")},
		VariantPool: []*enginev1.TopicVariants{{TopicId: "top-stars", Variants: []*enginev1.Question{q("v1", "q1", "What shines at night?"), q("v2", "q1", "Which are suns?")}}},
	}, []*enginev1.Usage{{Provider: "mock", Model: "claude-sonnet-5-5", InputTokens: 10, OutputTokens: 10, LatencyMs: 1, Delivered: true, Component: "content"}}, nil
}

func TestReactionsProjectAuthoritativeResultsWithoutChangingGradingIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("reactions"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	lessons := lessontest.New(lessontest.Options{Pool: db.Pool, Engine: reactionEngine{}, Financial: financial})
	deps := httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), CourseBrief: courses.New(courses.Options{Pool: db.Pool, Financial: financial}), Credits: financial, LessonTestTracerEnabled: true}
	server := httptest.NewServer(httpapi.NewWithLessonTest(deps, lessons, logger))
	defer server.Close()

	call := func(method, path string, body any, token string) (int, map[string]any, string) {
		var r io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			r = bytes.NewReader(raw)
		}
		req, _ := http.NewRequestWithContext(ctx, method, server.URL+path, r)
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out, string(raw)
	}
	email := "reactions@test.local"
	call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, "")
	_, tok, _ := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
	token := tok["access_token"].(string)
	var learnerID string
	_ = db.Pool.QueryRow(ctx, "SELECT id::text FROM learners LIMIT 1").Scan(&learnerID)
	if _, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1, 500, 'purchase', 'seed')`, learnerID); err != nil {
		t.Fatal(err)
	}
	var quoteSeed, creationID, lessonID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid,'seed-ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id::text`, learnerID, cat.Config().Version).Scan(&quoteSeed); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, quoteSeed).Scan(&creationID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `
		WITH c AS (INSERT INTO courses(learner_id, creation_id, title) VALUES($1,$2::uuid,'Astronomy') RETURNING id),
		u AS (INSERT INTO course_units(learner_id, course_id, authored_id, position, title) SELECT $1,c.id,'u1',0,'Night Sky' FROM c RETURNING id),
		e AS (INSERT INTO course_enrollments(learner_id, course_id) SELECT $1,c.id FROM c),
		l AS (INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status) SELECT $1,u.id,'l1',0,'Stars','Learn stars','ready_to_generate' FROM u RETURNING id)
		SELECT id::text FROM l`, learnerID, creationID).Scan(&lessonID); err != nil {
		t.Fatal(err)
	}
	_, q, _ := call("POST", "/v1/quotes", map[string]any{"action": "lesson_content", "narration_characters": 100}, token)
	quoteID := q["id"].(string)
	call("POST", "/v1/quotes/"+quoteID+"/accept", nil, token)
	code, prep, _ := call("POST", "/v1/lessons/"+lessonID+"/prepare", map[string]any{"quote_id": quoteID}, token)
	if code != 202 {
		t.Fatal(code, prep)
	}
	if err = lessons.RunLessonGeneration(ctx, prep["id"].(string)); err != nil {
		t.Fatal(err)
	}
	_, testOut, testRaw := call("GET", "/v1/lessons/"+lessonID+"/test", nil, token)
	if strings.Contains(testRaw, "reaction") || strings.Contains(testRaw, "answer_index") {
		t.Fatalf("pre-submission payload exposes answer or reaction: %s", testRaw)
	}
	q1 := testOut["questions"].([]any)[0].(map[string]any)["question_id"]
	approved := func(out map[string]any, trigger, expression string) {
		t.Helper()
		r, ok := out["reaction"].(map[string]any)
		if !ok || r["trigger"] != trigger || r["expression"] != expression || r["copy"] == "" {
			t.Fatalf("reaction %v want %s/%s", out["reaction"], trigger, expression)
		}
		if len(r) != 3 {
			t.Fatalf("reaction has extra keys: %v", r)
		}
	}
	answer := func(qid any, choice int, op string) (map[string]any, string) {
		code, out, raw := call("POST", "/v1/lessons/"+lessonID+"/test/answer", map[string]any{"question_id": qid, "choice_index": choice, "operation_key": op}, token)
		if code != 200 {
			t.Fatal(code, raw)
		}
		return out, raw
	}
	wrong, wrongRaw := answer(q1, 1, "wrong-1")
	if wrong["correct"] != false || wrong["passed"] != false || wrong["retry_variant"] == nil || wrong["explanation"] == "" {
		t.Fatalf("grading changed: %v", wrong)
	}
	approved(wrong, "answer_incorrect", "thinking")
	again, againRaw := answer(q1, 1, "wrong-1")
	if againRaw != wrongRaw && !jsonEqual(again, wrong) {
		t.Fatalf("replay changed result: %s vs %s", againRaw, wrongRaw)
	}
	var status string
	_ = db.Pool.QueryRow(ctx, "SELECT status FROM course_lesson_stubs WHERE id=$1::uuid", lessonID).Scan(&status)
	if status == "done" {
		t.Fatal("wrong answer completed the lesson")
	}
	variant := wrong["retry_variant"].(map[string]any)["question_id"]
	pass, _ := answer(variant, 0, "pass-1")
	if pass["correct"] != true || pass["passed"] != true {
		t.Fatalf("pass: %v", pass)
	}
	approved(pass, "test_passed", "celebrate")
	pass2, _ := answer(variant, 0, "pass-1")
	if !jsonEqual(pass, pass2) {
		t.Fatalf("duplicate completion not idempotent: %v %v", pass, pass2)
	}
	var xp, outbox int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM xp_events WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, lessonID).Scan(&xp)
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM lesson_completion_outbox WHERE learner_id=$1::uuid AND lesson_id=$2::uuid", learnerID, lessonID).Scan(&outbox)
	if xp != 1 || outbox != 1 {
		t.Fatalf("xp=%d outbox=%d want 1/1", xp, outbox)
	}
	var stored string
	if err = db.Pool.QueryRow(ctx, `SELECT response_payload::text FROM lesson_test_answer_operations WHERE operation_key='wrong-1'`).Scan(&stored); err != nil || !strings.Contains(stored, `"answer_incorrect"`) {
		t.Fatalf("restored attempt payload lost reaction: %q %v", stored, err)
	}
}

func jsonEqual(a, b map[string]any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
