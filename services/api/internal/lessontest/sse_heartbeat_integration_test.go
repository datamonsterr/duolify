//go:build integration

package lessontest_test

import (
	"bufio"
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

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/player/playerfixture"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestLessonGenerationEventsHeartbeatWhileIdleIntegration keeps a generation in "generating" and
// expects ":" comment lines on the events stream.
func TestLessonGenerationEventsHeartbeatWhileIdleIntegration(t *testing.T) {
	t.Setenv("SSE_HEARTBEAT_INTERVAL", "100ms")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("lessontest"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(db.Pool, nil, cat, nil)
	lessonService := lessontest.New(lessontest.Options{Objects: new(playerfixture.MemoryObjects), AssetOrigin: playerfixture.AssetOrigin, Pool: db.Pool, Financial: financial})
	deps := httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), CourseBrief: courses.New(courses.Options{Pool: db.Pool, Financial: financial}), Credits: financial, LessonTestTracerEnabled: true}
	server := httptest.NewServer(httpapi.NewWithLessonTest(deps, lessonService, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()

	post := func(path string, body any) map[string]any {
		raw, _ := json.Marshal(body)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		resp, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer func() { _ = resp.Body.Close() }()
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		return m
	}
	email := "lessontest-hb@example.test"
	post("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true})
	token := post("/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)})["access_token"].(string)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	var job string
	if err = db.Pool.QueryRow(ctx, `WITH q AS (INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'seed-ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id),
	 cr AS (INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) SELECT $1::uuid,gen_random_uuid(),1,id,'{}','enrolled' FROM q RETURNING id),
	 c AS (INSERT INTO courses(learner_id,creation_id,title) SELECT $1::uuid,id,'Go' FROM cr RETURNING id),
	 u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,id,'u',0,'Basics' FROM c RETURNING id,course_id),
	 l AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1::uuid,id,'l',0,'Functions','Learn Go','generating' FROM u RETURNING id),
	 j AS (INSERT INTO lesson_generations(learner_id,course_id,lesson_id,quote_id,state) SELECT $1::uuid,u.course_id,l.id,q.id,'generating' FROM u,l,q RETURNING id)
	 SELECT id::text FROM j`, claims.Subject, cat.Config().Version).Scan(&job); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/lesson-generations/"+job+"/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	seen := make(chan bool, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), ":") {
				seen <- true
				return
			}
		}
		seen <- false
	}()
	select {
	case ok := <-seen:
		if !ok {
			t.Fatal("stream ended without a heartbeat comment")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no heartbeat comment on an idle lesson generation stream")
	}
}
