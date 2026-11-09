//go:build integration

package courses_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/riverqueue/river"
	"sync/atomic"

	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestGenerateOutlineEnrollOnceIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("outline"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	fixture := new(creationEngine)
	service := courses.New(courses.Options{Pool: db.Pool, OutlineEngine: fixture, Financial: financial})
	queue, err := service.CreationQueue(logger)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), CourseBrief: service}, logger))
	defer server.Close()
	call := func(method, path string, body any, token string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	login := func(email string) string {
		if code, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); code != 202 {
			t.Fatal(code)
		}
		code, out := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
		if code != 200 {
			t.Fatal(code, out)
		}
		return out["access_token"].(string)
	}
	token := login("outline@example.test")

	code, brief := call("POST", "/v1/course-brief/confirm", map[string]any{"confirmed": true, "brief": map[string]string{"topic": "Sky"}}, token)
	if code != 201 {
		t.Fatal(code, brief)
	}
	outline := []any{map[string]any{"id": "u1", "title": "Moon", "lessons": []any{map[string]string{"id": "l1", "title": "Phases", "goal": "Explain phases"}}}}
	code, draft := call("POST", "/v1/outline-drafts", map[string]any{"brief_revision": brief["id"], "outline": outline}, token)
	if code != 201 {
		t.Fatalf("create draft: %d %v", code, draft)
	}
	var owner string
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email='outline@example.test'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,kind,amount,reference,pricing_version,provenance) VALUES($1,'purchase',1000,'creation-fixture',$2,'{}')`, owner, cat.Config().Version); err != nil {
		t.Fatal(err)
	}
	path := "/v1/outline-drafts/" + draft["id"].(string)
	if c, _ := call("POST", path+"/confirm", map[string]any{"expected_revision": 1}, token); c != 200 {
		t.Fatal(c)
	}
	in := map[string]any{"draft_id": draft["id"], "expected_revision": 1}
	c, q := call("POST", "/v1/course-creations/quote", in, token)
	if c != 201 {
		t.Fatal(c, q)
	}
	if c, _ = call("POST", "/v1/quotes/"+q["id"].(string)+"/accept", map[string]any{}, token); c != 200 {
		t.Fatal(c)
	}
	in["quote_id"] = q["id"]
	c, creation := call("POST", "/v1/course-creations", in, token)
	if c != 202 {
		t.Fatal(c, creation)
	}
	// Queue is still stopped: HTTP must not call the engine.
	if fixture.calls.Load() != 0 {
		t.Fatal("provider on request path")
	}
	c, duplicate := call("POST", "/v1/course-creations", in, token)
	if c != 202 || duplicate["id"] != creation["id"] {
		t.Fatal(c, duplicate)
	}
	if err = queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	var completed map[string]any
	for time.Now().Before(deadline) {
		_, completed = call("GET", "/v1/course-creations/"+creation["id"].(string), nil, token)
		if completed["state"] == "enrolled" || completed["state"] == "failed" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err = queue.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if completed["state"] != "enrolled" {
		t.Fatal(completed)
	}
	courseID := completed["course_id"].(string)
	if c, path := call("GET", "/v1/courses/"+courseID, nil, token); c != 200 || path["title"] != "Sky" {
		t.Fatal(c, path)
	}
	// A worker replay after enrollment cannot call provider or create another record.
	if err = service.RunCreation(ctx, creation["id"].(string)); err != nil {
		t.Fatal(err)
	}
	if fixture.calls.Load() != 1 {
		t.Fatal("duplicate provider call")
	}
	for _, table := range []string{"courses", "course_units", "course_lesson_stubs", "course_enrollments", "course_creations", "credit_jobs"} {
		var count int
		if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 1 {
			t.Fatal(table, count, err)
		}
	}
	_ = river.QueueDefault
}

type creationEngine struct {
	calls   atomic.Int32
	failErr error
	resp    *enginev1.GenerateCourseOutlineResponse
}

func (e *creationEngine) GenerateCourseOutline(_ context.Context, r *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error) {
	e.calls.Add(1)
	if e.failErr != nil {
		return nil, e.failErr
	}
	if e.resp != nil {
		return e.resp, nil
	}
	return &enginev1.GenerateCourseOutlineResponse{
		Outline:         r.EditedOutline,
		OutlineRevision: r.OutlineRevision,
		Attempts: []*enginev1.Usage{{
			Provider:     r.Limits.Provider,
			Model:        r.Limits.Model,
			Attempt:      1,
			Component:    "content",
			Delivered:    true,
			InputTokens:  100,
			OutputTokens: 20,
		}},
	}, nil
}

func TestTerminalEngineFailureReleasesReservationWithNoPartialEnrollment(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("outline_fail"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	failingFixture := &creationEngine{
		failErr: errors.New("terminal engine provider failure"),
	}
	service := courses.New(courses.Options{Pool: db.Pool, OutlineEngine: failingFixture, Financial: financial})
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Quotes: pricing.NewQuotes(db.Pool), CourseBrief: service}, logger))
	defer server.Close()

	call := func(method, path string, body any, token string) (int, map[string]any) {
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	login := func(email string) string {
		if code, _ := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true}, ""); code != 202 {
			t.Fatal(code)
		}
		code, out := call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": mail.Code(email)}, "")
		if code != 200 {
			t.Fatal(code, out)
		}
		return out["access_token"].(string)
	}
	token := login("fail@example.test")

	code, brief := call("POST", "/v1/course-brief/confirm", map[string]any{"confirmed": true, "brief": map[string]string{"topic": "Terminal Fail Sky"}}, token)
	if code != 201 {
		t.Fatal(code, brief)
	}
	outline := []any{map[string]any{"id": "u1", "title": "Moon", "lessons": []any{map[string]string{"id": "l1", "title": "Phases", "goal": "Explain phases"}}}}
	code, draft := call("POST", "/v1/outline-drafts", map[string]any{"brief_revision": brief["id"], "outline": outline}, token)
	if code != 201 {
		t.Fatalf("create draft: %d %v", code, draft)
	}
	var owner string
	if err = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email='fail@example.test'").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO credit_entries(learner_id,kind,amount,reference,pricing_version,provenance) VALUES($1,'purchase',500,'fail-creation-fixture',$2,'{}')`, owner, cat.Config().Version); err != nil {
		t.Fatal(err)
	}
	path := "/v1/outline-drafts/" + draft["id"].(string)
	if c, _ := call("POST", path+"/confirm", map[string]any{"expected_revision": 1}, token); c != 200 {
		t.Fatal(c)
	}
	in := map[string]any{"draft_id": draft["id"], "expected_revision": 1}
	c, q := call("POST", "/v1/course-creations/quote", in, token)
	if c != 201 {
		t.Fatal(c, q)
	}
	if c, _ = call("POST", "/v1/quotes/"+q["id"].(string)+"/accept", map[string]any{}, token); c != 200 {
		t.Fatal(c)
	}
	in["quote_id"] = q["id"]
	c, creation := call("POST", "/v1/course-creations", in, token)
	if c != 202 {
		t.Fatal(c, creation)
	}

	// Run creation worker job, which hits the terminal engine error.
	creationID := creation["id"].(string)
	if err = service.RunCreation(ctx, creationID); err != nil {
		t.Fatal(err)
	}

	// Verify creation status is "failed"
	c, state := call("GET", "/v1/course-creations/"+creationID, nil, token)
	if c != 200 || state["state"] != "failed" {
		t.Fatalf("expected state=failed, got %d %+v", c, state)
	}

	// Verify reservation was released in credit_jobs
	var jobState string
	if err = db.Pool.QueryRow(ctx, `SELECT state FROM credit_jobs WHERE id=(SELECT reservation_id FROM course_creations WHERE id::text=$1)`, creationID).Scan(&jobState); err != nil {
		t.Fatal(err)
	}
	if jobState != "released" {
		t.Fatalf("expected credit_job state=released, got %s", jobState)
	}

	// Verify wallet has 0 reserved credits
	wallet, err := financial.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Reserved != 0 {
		t.Fatalf("expected reserved=0, got %d", wallet.Reserved)
	}

	// Verify NO course was created and NO enrollment exists (no partial enrollment)
	for _, table := range []string{"courses", "course_units", "course_lesson_stubs", "course_enrollments"} {
		var count int
		if err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE learner_id::text=$1", owner).Scan(&count); err != nil {
			t.Fatal(table, err)
		}
		if count != 0 {
			t.Fatalf("expected 0 rows in %s, got %d", table, count)
		}
	}

	// Verify retry/replay on failed job does not double-spend or create partial enrollment
	if err = service.RunCreation(ctx, creationID); err != nil {
		t.Fatal(err)
	}
	walletAfterReplay, err := financial.GetWallet(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if walletAfterReplay.Reserved != 0 || walletAfterReplay.Available != wallet.Available {
		t.Fatalf("replay mutated wallet: %+v vs %+v", walletAfterReplay, wallet)
	}
}
