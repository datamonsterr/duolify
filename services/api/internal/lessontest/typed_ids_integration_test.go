//go:build integration

package lessontest_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func typedIDsDB(t *testing.T, recorder *statementRecorder) (*pgxpool.Pool, string) {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("typedids"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = recorder
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var learner string
	if err = pool.QueryRow(ctx, `INSERT INTO learners(display_name) VALUES('typedids') RETURNING id::text`).Scan(&learner); err != nil {
		t.Fatal(err)
	}
	return pool, learner
}

// Ids are bound as `$n::uuid`, which Postgres rejects with 22P02 when malformed. Every lesson and
// generation route answers a malformed id with a 404 and never with a database error.
func TestMalformedIDsAreNotFoundIntegration(t *testing.T) {
	ctx := t.Context()
	recorder := new(statementRecorder)
	pool, learner := typedIDsDB(t, recorder)
	svc := lessontest.New(lessontest.Options{Pool: pool, Objects: new(playerfixture.MemoryObjects), AssetOrigin: playerfixture.AssetOrigin})
	const unknown = "00000000-0000-4000-8000-000000000000" // body of the prepare route only
	routes := []struct{ name, method, path, body string }{
		{"lesson open", http.MethodGet, "/v1/lessons/{id}", ""},
		{"lesson test", http.MethodGet, "/v1/lessons/{id}/test", ""},
		{"lesson test answer", http.MethodPost, "/v1/lessons/{id}/test/answer", `{"question_id":"q","choice_index":0}`},
		{"lesson recap", http.MethodGet, "/v1/lessons/{id}/test/recap?topic_id=t", ""},
		{"lesson prepare", http.MethodPost, "/v1/lessons/{id}/prepare", `{"quote_id":"` + unknown + `"}`},
		{"generation status", http.MethodGet, "/v1/lesson-generations/{id}", ""},
		{"generation events", http.MethodGet, "/v1/lesson-generations/{id}/events", ""},
	}
	for _, route := range routes {
		call := func(id string) (int, string) {
			req := httptest.NewRequestWithContext(accounts.WithLearnerID(ctx, learner), route.method, strings.ReplaceAll(route.path, "{id}", id), strings.NewReader(route.body))
			rec := httptest.NewRecorder()
			svc.ServeHTTP(rec, req)
			return rec.Code, rec.Body.String()
		}
		for _, malformed := range []string{"not-a-uuid", "astronomy", "demo", "123", "0000000000000000000000000000000z"} {
			t.Run(route.name+"/"+malformed, func(t *testing.T) {
				got, gotBody := call(malformed)
				if got != http.StatusNotFound {
					t.Fatalf("malformed id %q: status %d %s, want 404", malformed, got, gotBody)
				}
			})
		}
	}
}

// The generation stream backs off while the state is unchanged instead of polling every 200 ms,
// and still ends with the terminal event.
func TestGenerationEventsPollBacksOffIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	recorder := new(statementRecorder)
	pool, learner := typedIDsDB(t, recorder)
	var generation string
	if err := pool.QueryRow(ctx, `WITH q AS (INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'seed-ref',(SELECT max(version) FROM pricing_versions),'{}',now(),now()+interval '10 minutes') RETURNING id),
	 cr AS (INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) SELECT $1::uuid,gen_random_uuid(),1,id,'{}','enrolled' FROM q RETURNING id),
	 c AS (INSERT INTO courses(learner_id,creation_id,title) SELECT $1::uuid,id,'Go' FROM cr RETURNING id),
	 u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,id,'u',0,'Basics' FROM c RETURNING id,course_id),
	 l AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1::uuid,id,'l',0,'Functions','Learn Go','generating' FROM u RETURNING id),
	 j AS (INSERT INTO lesson_generations(learner_id,course_id,lesson_id,quote_id,state) SELECT $1::uuid,u.course_id,l.id,q.id,'generating' FROM u,l,q RETURNING id)
	 SELECT id::text FROM j`, learner).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	svc := lessontest.New(lessontest.Options{Pool: pool, Objects: new(playerfixture.MemoryObjects), AssetOrigin: playerfixture.AssetOrigin})

	recorder.record(true)
	sctx, stop := context.WithTimeout(accounts.WithLearnerID(ctx, learner), 2500*time.Millisecond)
	defer stop()
	req := httptest.NewRequestWithContext(sctx, http.MethodGet, "/v1/lesson-generations/"+generation+"/events", nil)
	rec := httptest.NewRecorder()
	svc.ServeHTTP(rec, req)
	polls := len(recorder.record(false))
	// A fixed 200 ms poll reads about 13 times in 2.5 s; 200, 400, 800, 1600 ms backing off reads 5 times.
	if polls > 7 {
		t.Fatalf("%d statements in 2.5 s of an unchanged stream, want the poll to back off", polls)
	}
	if !strings.Contains(rec.Body.String(), `"state":"generating"`) {
		t.Fatalf("first stage event missing: %q", rec.Body.String())
	}

	// A state change is delivered, and a terminal state ends the stream.
	if _, err := pool.Exec(ctx, `UPDATE lesson_generations SET state='failed' WHERE id=$1::uuid`, generation); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	svc.ServeHTTP(rec, httptest.NewRequestWithContext(accounts.WithLearnerID(ctx, learner), http.MethodGet, "/v1/lesson-generations/"+generation+"/events", nil))
	if !strings.Contains(rec.Body.String(), `"state":"failed"`) {
		t.Fatalf("terminal event missing: %q", rec.Body.String())
	}
}
