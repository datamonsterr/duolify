//go:build integration

package courses_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// statementCounter counts every statement the pool runs once enabled.
type statementCounter struct {
	on atomic.Bool
	n  atomic.Int64
}

func (c *statementCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	if c.on.Load() {
		c.n.Add(1)
	}
	return ctx
}
func (*statementCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestCourseListIsASummaryWithConstantQueryCount(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("course_list"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	counter := new(statementCounter)
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := courses.New(courses.Options{Pool: pool})

	newLearner := func(name string) string {
		var id string
		if err := pool.QueryRow(ctx, "INSERT INTO learners(display_name) VALUES($1) RETURNING id::text", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	// seed adds one enrolled course with unit sizes given; statuses[i] applies to lesson i of the
	// course counted across units.
	seq := 0
	seed := func(learner, title string, unitSizes []int, status func(i int) string) string {
		seq++
		var quote, creation, course string
		if err := pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, $2, (SELECT version FROM pricing_versions ORDER BY effective_from LIMIT 1), '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learner, fmt.Sprint("ref", seq)).Scan(&quote); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learner, quote).Scan(&creation); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title, description) VALUES($1::uuid, $2::uuid, $3, 'about') RETURNING id::text`, learner, creation, title).Scan(&course); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id, course_id) VALUES($1::uuid, $2::uuid)`, learner, course); err != nil {
			t.Fatal(err)
		}
		n := 0
		for u, size := range unitSizes {
			var unit string
			if err := pool.QueryRow(ctx, `INSERT INTO course_units(learner_id, course_id, authored_id, position, title) VALUES($1::uuid, $2::uuid, $3, $4, $5) RETURNING id::text`, learner, course, fmt.Sprint("u", u), u, fmt.Sprint("Unit ", u)).Scan(&unit); err != nil {
				t.Fatal(err)
			}
			for l := range size {
				if _, err := pool.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1::uuid, $2::uuid, $3, $4, $5, 'goal', $6)`, learner, unit, fmt.Sprint("l", n), l, fmt.Sprint("Lesson ", n), status(n)); err != nil {
					t.Fatal(err)
				}
				n++
			}
		}
		return course
	}
	list := func(learner, etag string) (*httptest.ResponseRecorder, int64) {
		req := httptest.NewRequestWithContext(accounts.WithLearnerID(ctx, learner), http.MethodGet, "/v1/courses", nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		counter.n.Store(0)
		counter.on.Store(true)
		service.ServeCoursesListHTTP(w, req)
		counter.on.Store(false)
		return w, counter.n.Load()
	}

	// Progress maths: two units of 3 and 2 lessons, two done, the third one current.
	probe := newLearner("probe")
	seed(probe, "Astronomy", []int{3, 2}, func(i int) string {
		return []string{"done", "done", "current", "locked", "locked"}[i]
	})
	w, _ := list(probe, "")
	var out struct {
		Courses []map[string]any `json:"courses"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Courses) != 1 {
		t.Fatalf("status=%d body=%s err=%v", w.Code, w.Body, err)
	}
	course := out.Courses[0]
	if _, tree := course["units"]; tree {
		t.Fatalf("the list must not carry the unit/lesson tree: %s", w.Body)
	}
	for _, k := range []string{"id", "title", "description", "level_override", "pinned_version", "moderation_blocked", "copy_origin"} {
		if _, ok := course[k]; !ok {
			t.Errorf("summary lost field %q: %v", k, course)
		}
	}
	progress, _ := json.Marshal(course["progress"])
	if string(progress) != `{"current":{"lesson_index":2,"unit_index":0,"unit_lessons":3},"lessons_done":2,"lessons_total":5}` {
		t.Fatalf("progress=%s", progress)
	}

	// Query count and body size do not depend on lessons per course.
	var counts [2]int64
	var sizes [2]int
	for i, lessons := range []int{3, 20} {
		learner := newLearner(fmt.Sprint("learner", lessons))
		for c := range 5 {
			seed(learner, fmt.Sprint("Course ", c), []int{lessons / 2, lessons - lessons/2}, func(int) string { return "ready_to_generate" })
		}
		w, n := list(learner, "")
		if w.Code != http.StatusOK {
			t.Fatalf("status=%d", w.Code)
		}
		counts[i], sizes[i] = n, w.Body.Len()
		t.Logf("5 courses x %d lessons: %d statements, %d bytes", lessons, n, w.Body.Len())
	}
	if counts[0] != counts[1] || counts[0] > 3 {
		t.Fatalf("statements per list request must be constant and small: %v", counts)
	}
	if sizes[1] > sizes[0]+200 {
		t.Fatalf("body grew with lessons per course: %v", sizes)
	}

	// Conditional GET: unchanged progress is a bodyless 304; progress change is a new ETag.
	first, _ := list(probe, "")
	etag := first.Header().Get("ETag")
	if cc := first.Header().Get("Cache-Control"); etag == "" || cc != "private, no-cache" {
		t.Fatalf("ETag=%q Cache-Control=%q", etag, cc)
	}
	if again, _ := list(probe, etag); again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("unchanged list: status=%d body=%d", again.Code, again.Body.Len())
	}
	if _, err := pool.Exec(ctx, `UPDATE course_lesson_stubs SET status='done' WHERE learner_id=$1::uuid AND authored_id='l2'`, probe); err != nil {
		t.Fatal(err)
	}
	if changed, _ := list(probe, etag); changed.Code != http.StatusOK || changed.Header().Get("ETag") == etag {
		t.Fatalf("changed list: status=%d etag=%q", changed.Code, changed.Header().Get("ETag"))
	}
	// Another learner never matches this learner's validator.
	if other, _ := list(newLearner("someone else"), etag); other.Code != http.StatusOK {
		t.Fatalf("foreign validator status=%d", other.Code)
	}
}
