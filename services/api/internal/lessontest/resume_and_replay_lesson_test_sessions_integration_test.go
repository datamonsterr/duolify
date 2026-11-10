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
	"reflect"
	"strconv"
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

	events := &captureEvents{}
	ttl := 7 * 24 * time.Hour
	lessonService := lessontest.New(lessontest.Options{
		Pool:                db.Pool,
		Events:              events,
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
		req.Header.Set("X-App-Version", "review.1")
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
	if _, err := db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id, amount, kind, reference) VALUES($1, 100000, 'purchase', 'test-seed')`, learnerID); err != nil {
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

	getTest := func(id string) map[string]any {
		t.Helper()
		code, data, raw := call("GET", "/v1/lessons/"+id+"/test", nil, token)
		if code != 200 {
			t.Fatalf("get test: %d %s", code, raw)
		}
		return data
	}
	questionID := func(data map[string]any, index int) string {
		return data["questions"].([]any)[index].(map[string]any)["question_id"].(string)
	}
	answer := func(id, q string, choice int, key string) (int, map[string]any, string) {
		return call("POST", "/v1/lessons/"+id+"/test/answer", map[string]any{"question_id": q, "choice_index": choice, "operation_key": key}, token)
	}

	t.Run("Review161_OpaqueIDsAndRevealCannotReplayOriginal", func(t *testing.T) {
		id := createAndGenerateLesson("Exact entries")
		data := getTest(id)
		q := questionID(data, 0)
		if q == "q1" || q == "top-sunrise" {
			t.Errorf("predictable queue id: %s", q)
		}
		code, wrong, raw := answer(id, q, 0, "review-wrong")
		if code != 200 || wrong["correct"] != false {
			t.Fatalf("wrong answer: %d %s", code, raw)
		}
		for _, alias := range []string{q, "q1", "top-sunrise", "What makes the Sun rise in the east?"} {
			code, res, raw := answer(id, alias, int(wrong["answer_index"].(float64)), "")
			if code != 400 || res["answer_index"] != nil {
				t.Errorf("consumed/alias %s: %d %s", alias, code, raw)
			}
		}
		code, res, raw := answer(id, questionID(data, 1), 1, "review-other")
		if code != 200 || res["passed"] != false {
			t.Fatalf("must still answer retry: %d %s", code, raw)
		}
		variant := wrong["retry_variant"].(map[string]any)["question_id"].(string)
		code, res, raw = answer(id, variant, 1, "review-variant")
		if code != 200 || res["passed"] != true {
			t.Fatalf("exact variant: %d %s", code, raw)
		}
		passedEvents := events.FindEvents("lesson_test_passed")
		if got := passedEvents[len(passedEvents)-1].Props["score"]; got != 50 {
			t.Errorf("first-try score should be 50, got %v", got)
		}
	})

	t.Run("Review161_GradesExactSnapshotInsteadOfOriginal", func(t *testing.T) {
		id := createAndGenerateLesson("Exact private snapshot")
		if _, err := db.Pool.Exec(ctx, `UPDATE lesson_retry_pools SET variants=jsonb_set(variants,'{0,answer_index}','2') WHERE lesson_stub_id=$1::uuid AND topic_id='top-sunrise'`, id); err != nil {
			t.Fatal(err)
		}
		data := getTest(id)
		code, wrong, raw := answer(id, questionID(data, 0), 0, "review-snapshot-source")
		if code != 200 {
			t.Fatal(code, raw)
		}
		variant := wrong["retry_variant"].(map[string]any)["question_id"].(string)
		code, graded, raw := answer(id, variant, 1, "review-snapshot-variant")
		if code != 200 || graded["correct"] != false || graded["answer_index"] != float64(2) {
			t.Fatalf("variant graded with unrelated source: %d %s", code, raw)
		}
	})

	t.Run("Review161_EachOriginalQuestionMustResolve", func(t *testing.T) {
		id := createAndGenerateLesson("Every question")
		// Legacy content may already contain duplicate topics; runtime resolution still counts questions.
		if _, err := db.Pool.Exec(ctx, `UPDATE lesson_questions SET topic_id='top-sunrise' WHERE lesson_stub_id=$1::uuid`, id); err != nil {
			t.Fatal(err)
		}
		data := getTest(id)
		code, res, raw := answer(id, questionID(data, 0), 1, "review-each-original")
		if code != 200 || res["passed"] != false {
			t.Fatalf("one answer passed multiple originals: %d %s", code, raw)
		}
		remaining := getTest(id)["questions"].([]any)
		if len(remaining) != 1 {
			t.Fatalf("unanswered original removed: %v", remaining)
		}
		code, res, raw = answer(id, questionID(data, 1), 1, "review-each-second")
		if code != 200 || res["passed"] != true {
			t.Fatalf("both originals must pass: %d %s", code, raw)
		}
	})

	t.Run("Review161_OperationKeyBindsQuestionChoiceLessonAndSession", func(t *testing.T) {
		id := createAndGenerateLesson("Operation binding")
		data := getTest(id)
		q := questionID(data, 0)
		if code, _, raw := answer(id, q, 1, "review-binding"); code != 200 {
			t.Fatal(code, raw)
		}
		for _, submission := range []struct {
			lesson, question string
			choice           int
		}{
			{id, questionID(data, 1), 1}, {id, q, 0}, {lessonID, q, 1},
		} {
			if code, _, raw := answer(submission.lesson, submission.question, submission.choice, "review-binding"); code != 400 {
				t.Errorf("mismatched operation accepted: %d %s", code, raw)
			}
		}
		if _, err := db.Pool.Exec(ctx, `UPDATE lesson_test_sessions SET created_at=now()-interval '8 days', updated_at=now()-interval '8 days' WHERE id=$1::uuid`, data["session_id"]); err != nil {
			t.Fatal(err)
		}
		getTest(id)
		if code, _, raw := answer(id, q, 1, "review-binding"); code != 400 {
			t.Errorf("stale-session operation accepted: %d %s", code, raw)
		}
	})

	t.Run("Review161_SameKeyConcurrentWrongSubmit", func(t *testing.T) {
		id := createAndGenerateLesson("Same key race")
		data := getTest(id)
		q := questionID(data, 0)
		var wg sync.WaitGroup
		var codes [2]int
		var payloads [2]string
		start := make(chan struct{})
		for i := range codes {
			wg.Add(1)
			go func(i int) { defer wg.Done(); <-start; codes[i], _, payloads[i] = answer(id, q, 0, "review-same-key") }(i)
		}
		close(start)
		wg.Wait()
		var first, second any
		_ = json.Unmarshal([]byte(payloads[0]), &first)
		_ = json.Unmarshal([]byte(payloads[1]), &second)
		if codes[0] != 200 || codes[1] != 200 || !reflect.DeepEqual(first, second) {
			t.Fatalf("concurrent retry: %v %v", codes, payloads)
		}
		var count int
		if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM lesson_test_answers WHERE session_id=$1::uuid`, data["session_id"]).Scan(&count); err != nil || count != 1 {
			t.Fatalf("answer count=%d err=%v", count, err)
		}
	})

	t.Run("Review161_WriteFailureIs500AndRollsBack", func(t *testing.T) {
		id := createAndGenerateLesson("Write failure")
		data := getTest(id)
		var originalQueue []byte
		if err := db.Pool.QueryRow(ctx, `SELECT question_queue FROM lesson_test_sessions WHERE id=$1::uuid`, data["session_id"]).Scan(&originalQueue); err != nil {
			t.Fatal(err)
		}
		for _, table := range []string{"lesson_test_answers", "lesson_test_variant_snapshots", "lesson_test_sessions", "course_lesson_stubs", "lesson_completion_outbox", "lesson_completion_summaries", "lesson_test_answer_operations"} {
			t.Run(table, func(t *testing.T) {
				if _, err := db.Pool.Exec(ctx, `UPDATE lesson_test_sessions SET question_queue=$2, resolved_topics='[]' WHERE id=$1::uuid`, data["session_id"], originalQueue); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Pool.Exec(ctx, `CREATE OR REPLACE FUNCTION review_reject_write() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'test write failure'; END $$`); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Pool.Exec(ctx, "CREATE TRIGGER review_reject BEFORE INSERT OR UPDATE ON "+table+" FOR EACH ROW EXECUTE FUNCTION review_reject_write()"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, err := db.Pool.Exec(ctx, "DROP TRIGGER review_reject ON "+table); err != nil {
						t.Fatal(err)
					}
				}()
				choice := 1
				if table == "lesson_test_variant_snapshots" {
					choice = 0
				}
				// Final-answer failures exercise completion writes.
				if table == "course_lesson_stubs" || table == "lesson_completion_outbox" || table == "lesson_completion_summaries" {
					if _, err := db.Pool.Exec(ctx, `UPDATE lesson_test_sessions SET resolved_topics=jsonb_build_array(question_queue->1->>'original_id'), question_queue=jsonb_build_array(question_queue->0) WHERE id=$1::uuid`, data["session_id"]); err != nil {
						t.Fatal(err)
					}
				}
				code, _, raw := answer(id, questionID(data, 0), choice, "review-write-failure")
				if code != 500 {
					t.Fatalf("write failure status=%d %s", code, raw)
				}
				var count int
				if err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM lesson_test_answers WHERE session_id=$1::uuid`, data["session_id"]).Scan(&count); err != nil || count != 0 {
					t.Fatalf("rollback count=%d err=%v", count, err)
				}
			})
		}
	})

	t.Run("Review161_ConcurrentStartsAndReplayPreserveCompletion", func(t *testing.T) {
		id := createAndGenerateLesson("Replay completion")
		var wg sync.WaitGroup
		var sessions [2]string
		for i := range sessions {
			wg.Add(1)
			go func(i int) { defer wg.Done(); sessions[i] = getTest(id)["session_id"].(string) }(i)
		}
		wg.Wait()
		if sessions[0] != sessions[1] {
			t.Errorf("duplicate in-progress sessions: %v", sessions)
		}
		data := getTest(id)
		var initialQueue []byte
		if err := db.Pool.QueryRow(ctx, `SELECT question_queue FROM lesson_test_sessions WHERE id=$1::uuid`, data["session_id"]).Scan(&initialQueue); err != nil {
			t.Fatal(err)
		}
		before := len(events.FindEvents("lesson_test_passed"))
		if _, err := db.Pool.Exec(ctx, `UPDATE lesson_test_sessions SET created_at=now()-interval '2 minutes' WHERE id=$1::uuid`, data["session_id"]); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if code, _, raw := answer(id, questionID(data, i), 1, fmt.Sprintf("review-pass-%d", i)); code != 200 {
				t.Fatal(code, raw)
			}
		}
		passed := events.FindEvents("lesson_test_passed")
		if len(passed) != before+1 {
			t.Fatalf("pass event count: %d", len(passed))
		}
		props := passed[len(passed)-1].Props
		duration, err := strconv.ParseInt(fmt.Sprint(props["duration_ms"]), 10, 64)
		if err != nil || duration < 120000 || props["app_version"] != "review.1" || props["score"] != 100 {
			t.Errorf("fabricated analytics: %v", props)
		}
		var summary string
		if err := db.Pool.QueryRow(ctx, `SELECT summary FROM lesson_completion_summaries WHERE lesson_id=$1::uuid`, id).Scan(&summary); err != nil {
			t.Fatal(err)
		}
		replay := getTest(id)
		if replay["session_id"] != data["session_id"] {
			t.Errorf("GET after pass created session: %v", replay)
		}
		var after string
		if err := db.Pool.QueryRow(ctx, `SELECT summary FROM lesson_completion_summaries WHERE lesson_id=$1::uuid`, id).Scan(&after); err != nil || after != summary {
			t.Fatal("completion summary changed", err)
		}

		// A legacy replay session must not replace the original summary or emit another pass.
		if _, err := db.Pool.Exec(ctx, `UPDATE lesson_test_sessions SET state='abandoned' WHERE id=$1::uuid`, data["session_id"]); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Pool.Exec(ctx, `INSERT INTO lesson_test_sessions (learner_id,lesson_stub_id,question_queue,resolved_topics,first_try_correct) VALUES($1::uuid,$2::uuid,$3,'[]',42)`, learnerID, id, initialQueue); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if code, _, raw := answer(id, questionID(data, i), 1, fmt.Sprintf("review-legacy-replay-%d", i)); code != 200 {
				t.Fatal(code, raw)
			}
		}
		if len(events.FindEvents("lesson_test_passed")) != before+1 {
			t.Fatal("legacy replay emitted a duplicate pass")
		}
		if err := db.Pool.QueryRow(ctx, `SELECT summary FROM lesson_completion_summaries WHERE lesson_id=$1::uuid`, id).Scan(&after); err != nil || after != summary {
			t.Fatal("legacy replay overwrote completion summary", err)
		}
	})
	baseTest := getTest(lessonID)
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
			"question_id":   questionID(baseTest, 0),
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
			"question_id":   questionID(baseTest, 1),
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

		if code != http.StatusBadRequest {
			t.Fatalf("expected 400 for unserved question, got %d (%s)", code, raw)
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
		concurrentTest := getTest(lessonConcurID)

		var wg sync.WaitGroup
		results := make([]int, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				op := fmt.Sprintf("op-concur-%d", idx)
				c, _, _ := call("POST", fmt.Sprintf("/v1/lessons/%s/test/answer", lessonConcurID), map[string]any{
					"question_id":   questionID(concurrentTest, 0),
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
