//go:build integration

package courses_test

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
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// TestCourseCreationEventsHeartbeatWhileIdleIntegration keeps a creation in "generating" and
// expects ":" comment lines on the events stream after the single stage event.
func TestCourseCreationEventsHeartbeatWhileIdleIntegration(t *testing.T) {
	t.Setenv("SSE_HEARTBEAT_INTERVAL", "100ms")
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("courses"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	service := courses.New(courses.Options{Pool: db.Pool, Financial: financial})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), CourseBrief: service}, slog.New(slog.NewTextHandler(io.Discard, nil))))
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
	email := "courses-hb@example.test"
	post("/v1/auth/code", map[string]any{"email": email, "age_confirmed": true})
	token := post("/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)})["access_token"].(string)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	var quote, creation string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) VALUES($1::uuid,'seed-ref',$2,'{}',now(),now()+interval '10 minutes') RETURNING id::text`, claims.Subject, cat.Config().Version).Scan(&quote); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','generating') RETURNING id::text`, claims.Subject, quote).Scan(&creation); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/course-creations/"+creation+"/events", nil)
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
		t.Fatal("no heartbeat comment on an idle course creation stream")
	}
}
