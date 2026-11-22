//go:build integration

package lessontest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// statementRecorder keeps every statement (with its bound arguments) that the pool runs while on.
type statementRecorder struct {
	mu   sync.Mutex
	on   bool
	seen []recordedStatement
}

type recordedStatement struct {
	sql  string
	args []any
}

func (r *statementRecorder) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.on {
		r.seen = append(r.seen, recordedStatement{sql: data.SQL, args: append([]any(nil), data.Args...)})
	}
	return ctx
}
func (*statementRecorder) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (r *statementRecorder) record(on bool) []recordedStatement {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.on = on
	out := r.seen
	if on {
		r.seen = nil
	}
	return out
}

type planNode struct {
	NodeType string     `json:"Node Type"`
	Relation string     `json:"Relation Name"`
	Plans    []planNode `json:"Plans"`
}

func (n planNode) scans(out map[string][]string) {
	if n.Relation != "" {
		out[n.Relation] = append(out[n.Relation], n.NodeType)
	}
	for _, p := range n.Plans {
		p.scans(out)
	}
}

func explain(t *testing.T, pool *pgxpool.Pool, stmt recordedStatement) map[string][]string {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) "+stmt.sql, stmt.args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN %.120q: %v", stmt.sql, err)
	}
	var plans []struct {
		Plan planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		t.Fatalf("plan %s: %v", raw, err)
	}
	out := map[string][]string{}
	plans[0].Plan.scans(out)
	return out
}

// TestHotPathStatementsUseIndexesIntegration seeds a few thousand learners, drives the Learn home,
// lesson open, lesson test answer and generation stream requests, and EXPLAINs the exact statements
// those requests ran. A `col::text = $1` predicate cannot use the primary-key or foreign-key index,
// so the big table would show a Seq Scan; the planner is left free to choose (ANALYZE only).
func TestHotPathStatementsUseIndexesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("indexuse"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	recorder := new(statementRecorder)
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = recorder
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	// Two units of two lessons, three slides and three questions per lesson, for every learner.
	for _, q := range []string{
		`INSERT INTO learners(display_name) SELECT 'l' || g FROM generate_series(1, 3000) g`,
		`INSERT INTO learner_profiles(learner_id) SELECT id FROM learners`,
		`INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at)
		 SELECT id, 'seed-' || id, (SELECT max(version) FROM pricing_versions), '{}', now(), now() + interval '1 hour' FROM learners`,
		`INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state)
		 SELECT learner_id, gen_random_uuid(), 1, id, '{}', 'enrolled' FROM pricing_quotes`,
		`INSERT INTO courses(learner_id, creation_id, title) SELECT learner_id, id, 'Course' FROM course_creations`,
		`INSERT INTO course_enrollments(learner_id, course_id) SELECT learner_id, id FROM courses`,
		`UPDATE learner_profiles p SET active_course_id = c.id FROM courses c WHERE c.learner_id = p.learner_id`,
		`INSERT INTO course_units(course_id, learner_id, authored_id, position, title) SELECT c.id, c.learner_id, 'u' || p, p, 'Unit' FROM courses c, generate_series(0, 1) p`,
		`INSERT INTO course_lesson_stubs(unit_id, learner_id, authored_id, position, title, goal, status)
		 SELECT u.id, u.learner_id, 'l' || p, p, 'Lesson', 'goal', CASE WHEN u.position = 0 AND p = 0 THEN 'current' ELSE 'locked' END FROM course_units u, generate_series(0, 1) p`,
		`INSERT INTO generated_lessons(lesson_stub_id, course_id, learner_id, title, manifest) SELECT s.id, u.course_id, s.learner_id, 'Lesson', '{}' FROM course_lesson_stubs s JOIN course_units u ON u.id = s.unit_id`,
		`INSERT INTO lesson_slides(lesson_stub_id, learner_id, position, slide_id, html, duration_ms, narration, topic_ids, segments)
		 SELECT s.id, s.learner_id, p, 's' || p, '<p>x</p>', 1000, 'n', ARRAY['t' || p], '[]' FROM course_lesson_stubs s, generate_series(0, 2) p`,
		`INSERT INTO lesson_questions(lesson_stub_id, learner_id, topic_id, position, prompt, choices, answer_index, explanation)
		 SELECT s.id, s.learner_id, 't' || p, p, 'q', '["a","b","c"]', 0, 'e' FROM course_lesson_stubs s, generate_series(0, 2) p`,
		`INSERT INTO lesson_retry_pools(lesson_stub_id, learner_id, topic_id, variants) SELECT s.id, s.learner_id, 't' || p, '[]' FROM course_lesson_stubs s, generate_series(0, 2) p`,
		`INSERT INTO lesson_test_sessions(lesson_stub_id, learner_id, question_queue, resolved_topics, state)
		 SELECT id, learner_id, '[]', '[]', 'passed' FROM course_lesson_stubs WHERE status = 'locked'`,
		`INSERT INTO lesson_generations(learner_id, course_id, lesson_id, quote_id, state)
		 SELECT s.learner_id, u.course_id, s.id, q.id, 'generating' FROM course_lesson_stubs s JOIN course_units u ON u.id = s.unit_id JOIN course_creations cr ON cr.learner_id = s.learner_id JOIN pricing_quotes q ON q.id = cr.quote_id`,
	} {
		if _, err = pool.Exec(ctx, q); err != nil {
			t.Fatalf("seed %.80q: %v", q, err)
		}
	}
	if _, err = pool.Exec(ctx, `ANALYZE`); err != nil {
		t.Fatal(err)
	}

	var learner, course, lesson, generation string
	if err = pool.QueryRow(ctx, `SELECT s.learner_id::text, u.course_id::text, s.id::text, g.id::text
		FROM course_lesson_stubs s JOIN course_units u ON u.id = s.unit_id JOIN lesson_generations g ON g.lesson_id = s.id
		WHERE s.status = 'current' LIMIT 1`).Scan(&learner, &course, &lesson, &generation); err != nil {
		t.Fatal(err)
	}

	courseService := courses.New(courses.Options{Pool: pool})
	lessonService := lessontest.New(lessontest.Options{Pool: pool, Objects: new(playerfixture.MemoryObjects), AssetOrigin: playerfixture.AssetOrigin})
	do := func(handler func(http.ResponseWriter, *http.Request), method, path string, body any, pathValues ...string) *httptest.ResponseRecorder {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = bytes.NewReader(raw)
		}
		req := httptest.NewRequestWithContext(accounts.WithLearnerID(ctx, learner), method, path, reader)
		for i := 0; i+1 < len(pathValues); i += 2 {
			req.SetPathValue(pathValues[i], pathValues[i+1])
		}
		rec := httptest.NewRecorder()
		handler(rec, req)
		return rec
	}

	capture := func(f func()) []recordedStatement {
		recorder.record(true)
		f()
		return recorder.record(false)
	}

	learnHome := capture(func() {
		if rec := do(courseService.ServeCoursesListHTTP, http.MethodGet, "/v1/courses", nil); rec.Code != http.StatusOK {
			t.Fatalf("courses list: %d %s", rec.Code, rec.Body)
		}
		if rec := do(courseService.ServeCourseHTTP, http.MethodGet, "/v1/courses/"+course, nil, "id", course); rec.Code != http.StatusOK {
			t.Fatalf("course path: %d %s", rec.Code, rec.Body)
		}
	})
	lessonOpen := capture(func() {
		if rec := do(lessonService.ServeHTTP, http.MethodGet, "/v1/lessons/"+lesson, nil); rec.Code != http.StatusOK {
			t.Fatalf("lesson open: %d %s", rec.Code, rec.Body)
		}
	})
	var answers []recordedStatement
	var started struct {
		Questions []struct {
			QuestionID string `json:"question_id"`
		} `json:"questions"`
	}
	startCapture := capture(func() {
		rec := do(lessonService.ServeHTTP, http.MethodGet, "/v1/lessons/"+lesson+"/test", nil)
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &started) != nil || len(started.Questions) == 0 {
			t.Fatalf("lesson test: %d %s", rec.Code, rec.Body)
		}
	})
	answers = capture(func() {
		for i, q := range started.Questions {
			rec := do(lessonService.ServeHTTP, http.MethodPost, "/v1/lessons/"+lesson+"/test/answer", map[string]any{"question_id": q.QuestionID, "choice_index": 0, "operation_key": fmt.Sprintf("op-%02d-aaaaaaaa", i)})
			if rec.Code != http.StatusOK {
				t.Fatalf("answer %d: %d %s", i, rec.Code, rec.Body)
			}
		}
	})
	stream := capture(func() {
		sctx, stop := context.WithTimeout(ctx, 700*time.Millisecond)
		defer stop()
		req := httptest.NewRequestWithContext(accounts.WithLearnerID(sctx, learner), http.MethodGet, "/v1/lesson-generations/"+generation+"/events", nil)
		lessonService.ServeHTTP(httptest.NewRecorder(), req)
	})

	probes := []struct {
		name  string
		from  []recordedStatement
		match string // regexp over the statement text
		table string // big table that must not be read with a Seq Scan
	}{
		{"learn home: course list", learnHome, `(?s)FROM course_enrollments e.*e\.learner_id=\$1::uuid|(?s)FROM courses c JOIN course_enrollments e.*e\.learner_id=\$1::uuid`, "course_enrollments"},
		{"learn home: active course", learnHome, `FROM learner_profiles WHERE learner_id=\$1::uuid`, "learner_profiles"},
		{"learn home: course path", learnHome, `jsonb_build_object\('id',c\.id`, "courses"},
		{"lesson open: generated lesson", lessonOpen, `FROM generated_lessons g JOIN course_lesson_stubs cls`, "generated_lessons"},
		{"lesson open: slides", lessonOpen, `FROM lesson_slides\s+WHERE lesson_stub_id=\$1::uuid`, "lesson_slides"},
		{"lesson test start: stub", startCapture, `FROM course_lesson_stubs cls JOIN course_units cu ON cu\.id=cls\.unit_id WHERE cls\.id=\$1::uuid`, "course_lesson_stubs"},
		{"lesson test start: questions", startCapture, `FROM lesson_questions WHERE lesson_stub_id=\$1::uuid`, "lesson_questions"},
		{"lesson test answer: question", answers, `FROM lesson_questions WHERE id=\$1::uuid`, "lesson_questions"},
		{"lesson test start: session", startCapture, `FROM lesson_test_sessions\s+WHERE lesson_stub_id=\$1::uuid`, "lesson_test_sessions"},
		{"lesson test answer: session update", answers, `(?s)UPDATE lesson_test_sessions.*WHERE id=\$1::uuid`, "lesson_test_sessions"},
		{"lesson test answer: lesson done", answers, `UPDATE course_lesson_stubs SET status = 'done' WHERE id=\$1::uuid`, "course_lesson_stubs"},
		{"generation stream: state", stream, `SELECT state FROM lesson_generations\s+WHERE id=\$1::uuid AND learner_id=\$2::uuid`, "lesson_generations"},
	}
	indexed := func(types []string) bool {
		usesIndex := false
		for _, nodeType := range types {
			switch nodeType {
			case "Seq Scan":
				return false
			case "Index Scan", "Index Only Scan", "Bitmap Heap Scan":
				usesIndex = true
			}
		}
		return usesIndex
	}
	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			re := regexp.MustCompile(p.match)
			found := false
			for _, stmt := range p.from {
				if !re.MatchString(stmt.sql) {
					continue
				}
				found = true
				scans := explain(t, pool, stmt)
				types, ok := scans[p.table]
				if !ok {
					t.Fatalf("plan does not read %s: %v\n%s", p.table, scans, stmt.sql)
				}
				if !indexed(types) {
					t.Errorf("%s read with %v, want an index scan; plan %v\n%s", p.table, types, scans, strings.TrimSpace(stmt.sql))
				} else {
					t.Logf("%s: %v", p.table, types)
				}
				break
			}
			if !found {
				heads := make([]string, 0, len(p.from))
				for _, stmt := range p.from {
					heads = append(heads, strings.Join(strings.Fields(stmt.sql), " "))
				}
				t.Fatalf("no statement matching %q ran; statements:\n%s", p.match, strings.Join(heads, "\n"))
			}
		})
	}
}
