//go:build integration

package courses_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type recoveryMockEngine struct {
	mu           sync.Mutex
	shouldCancel bool
	shouldFail   bool
	calls        int
	// entered is signalled once a cancelling call is blocked in the engine,
	// i.e. after the creation is generating and holds its reservation.
	entered chan struct{}
}

func (m *recoveryMockEngine) GenerateCourseOutline(ctx context.Context, req *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error) {
	m.mu.Lock()
	m.calls++
	cancel := m.shouldCancel
	fail := m.shouldFail
	m.mu.Unlock()

	if cancel {
		if m.entered != nil {
			m.entered <- struct{}{}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if fail {
		return &enginev1.GenerateCourseOutlineResponse{Unavailable: true}, nil
	}
	provider := "anthropic"
	model := "claude-3-5-sonnet"
	if req.Limits != nil && req.Limits.Provider != "" {
		provider = req.Limits.Provider
		model = req.Limits.Model
	}
	return &enginev1.GenerateCourseOutlineResponse{
		Outline:         req.EditedOutline,
		OutlineRevision: req.OutlineRevision,
		Attempts: []*enginev1.Usage{{
			Attempt:      1,
			Component:    "content",
			Delivered:    true,
			Provider:     provider,
			Model:        model,
			InputTokens:  100,
			OutputTokens: 20,
			LatencyMs:    50,
		}},
	}, nil
}

func TestRecoverGenerationAndRemoveCoursesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("courses_e204"),
		postgres.WithUsername("fixture"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies())
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
	quotes := pricing.NewQuotes(db.Pool)
	events := &testEventsDouble{}
	mockEngine := &recoveryMockEngine{}

	service := courses.New(courses.Options{
		Pool:          db.Pool,
		OutlineEngine: mockEngine,
		Financial:     financial,
		Events:        events,
	})

	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{
		Accounts:    auth,
		Quotes:      quotes,
		CourseBrief: service,
		Credits:     financial,
		Postgres:    func(context.Context) error { return nil },
	}, logger))
	defer server.Close()

	call := func(method, path string, body any, token string) (int, map[string]any) {
		var bodyReader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			bodyReader = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, server.URL+path, bodyReader)
		if err != nil {
			t.Fatal(err)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}

	login := func(email string) (string, string) {
		code, out := call("POST", "/v1/auth/code", map[string]any{"email": email, "age_confirmed": true, "locale": "en", "time_zone": "UTC"}, "")
		if code != 202 {
			t.Fatalf("auth/code failed: %d %v", code, out)
		}
		sentCode := mail.Code(email)
		code, out = call("POST", "/v1/auth/verify", map[string]any{"email": email, "age_confirmed": true, "code": sentCode}, "")
		if code != 200 {
			t.Fatalf("auth/verify failed with code=%q: %d %v", sentCode, code, out)
		}
		token := out["access_token"].(string)
		var learnerID string
		if err := db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID); err != nil {
			t.Fatal(err)
		}
		return learnerID, token
	}

	learnerID, token := login("learner1@test.com")
	_, foreignToken := login("learner2@test.com")
	if _, err := financial.EnsureDailyGrant(ctx, learnerID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Pool.Exec(ctx, "INSERT INTO credit_entries(learner_id, kind, amount, reference) VALUES($1::uuid, 'purchase', 500, 'test-ref')", learnerID); err != nil {
		t.Fatal(err)
	}

	var initialBalance int64
	if err := db.Pool.QueryRow(ctx, "SELECT COALESCE(SUM(amount), 0) FROM credit_entries WHERE learner_id=$1::uuid", learnerID).Scan(&initialBalance); err != nil {
		t.Fatal(err)
	}

	// Create confirmed course brief
	code, briefOut := call("POST", "/v1/course-brief/confirm", map[string]any{
		"confirmed": true,
		"brief": map[string]string{
			"topic":              "Planets",
			"include_topics":     "Mars",
			"skip_topics":        "Maths",
			"level":              "beginner",
			"viewing_conditions": "Dark sky",
			"lesson_length":      "5m",
			"language":           "en",
		},
	}, token)
	if code != 201 {
		t.Fatalf("confirm brief code=%d out=%v", code, briefOut)
	}
	briefRev := briefOut["id"].(string)

	// Create outline draft with manual edits
	manualOutline := courses.Outline{
		courses.OutlineUnit{
			ID:    "unit-1",
			Title: "Planets of the Sun",
			Lessons: []courses.OutlineLesson{
				{ID: "lesson-1", Title: "Mars Exploration", Goal: "Identify Mars rovers"},
			},
		},
	}
	code, draftOut := call("POST", "/v1/outline-drafts", map[string]any{
		"brief_revision": briefRev,
		"outline":        manualOutline,
		"history":        []any{},
	}, token)
	if code != 201 {
		t.Fatalf("create draft code=%d out=%v", code, draftOut)
	}
	draftID := draftOut["id"].(string)
	rev := int64(draftOut["revision"].(float64))

	// Confirm outline
	code, confOut := call("POST", "/v1/outline-drafts/"+draftID+"/confirm", map[string]any{
		"expected_revision": rev,
	}, token)
	if code != 200 {
		t.Fatalf("confirm outline code=%d out=%v", code, confOut)
	}

	// 1. River cancellation & terminal failure releases reservation
	mockEngine.mu.Lock()
	mockEngine.shouldCancel = true
	mockEngine.mu.Unlock()

	// Quote creation
	code, qOut := call("POST", "/v1/course-creations/quote", map[string]any{
		"draft_id":          draftID,
		"expected_revision": rev,
	}, token)
	if code != 201 {
		t.Fatalf("quote code=%d out=%v", code, qOut)
	}
	quoteID := qOut["id"].(string)

	// Accept quote
	code, _ = call("POST", "/v1/quotes/"+quoteID+"/accept", map[string]any{}, token)
	if code != 200 {
		t.Fatalf("accept quote code=%d", code)
	}

	// Submit creation
	code, createOut := call("POST", "/v1/course-creations", map[string]any{
		"draft_id":          draftID,
		"expected_revision": rev,
		"quote_id":          quoteID,
	}, token)
	if code != 202 {
		t.Fatalf("submit creation code=%d out=%v", code, createOut)
	}
	creationID := createOut["id"].(string)

	// Run creation job with cancellation to simulate River worker cancellation
	// Cancel only once the job is blocked in the engine call. A timer races
	// the pre-engine steps (connect, lock, reserve) under load; cancelling
	// before the reservation is held leaves the job queued for River retry.
	jobCtx, jobCancel := context.WithCancel(ctx)
	defer jobCancel()
	mockEngine.mu.Lock()
	mockEngine.entered = make(chan struct{}, 1)
	entered := mockEngine.entered
	mockEngine.mu.Unlock()
	go func() {
		select {
		case <-entered:
			jobCancel()
		case <-ctx.Done():
		}
	}()
	_ = service.RunCreation(jobCtx, creationID)

	// Check that creation transitioned to failed and released reservation
	code, statusOut := call("GET", "/v1/course-creations/"+creationID, nil, token)
	if code != 200 || statusOut["state"] != "failed" {
		t.Fatalf("expected state=failed after cancellation, got code=%d out=%v", code, statusOut)
	}

	// Check that wallet was not charged (credits released)
	var balance int64
	if err := db.Pool.QueryRow(ctx, "SELECT COALESCE(SUM(amount), 0) FROM credit_entries WHERE learner_id=$1::uuid", learnerID).Scan(&balance); err != nil || balance != initialBalance {
		t.Fatalf("expected balance=%d after release, got %d err=%v", initialBalance, balance, err)
	}

	// 2. Retry with fresh quote and fresh consent
	mockEngine.mu.Lock()
	mockEngine.shouldCancel = false
	mockEngine.shouldFail = false
	mockEngine.mu.Unlock()

	code, retryQ := call("POST", "/v1/course-creations/quote", map[string]any{
		"draft_id":          draftID,
		"expected_revision": rev,
	}, token)
	if code != 201 {
		t.Fatalf("retry quote code=%d out=%v", code, retryQ)
	}
	retryQuoteID := retryQ["id"].(string)

	code, _ = call("POST", "/v1/quotes/"+retryQuoteID+"/accept", map[string]any{}, token)
	if code != 200 {
		t.Fatalf("accept retry quote code=%d", code)
	}

	// Submit retry
	code, retryCreate := call("POST", "/v1/course-creations", map[string]any{
		"draft_id":          draftID,
		"expected_revision": rev,
		"quote_id":          retryQuoteID,
	}, token)
	if code != 202 {
		t.Fatalf("retry creation code=%d out=%v", code, retryCreate)
	}

	// Cancellation before the engine call (worker shutdown, load) is not
	// terminal: the job stays queued with no hold or charge so River can retry.
	preCancelled, preCancel := context.WithCancel(ctx)
	preCancel()
	if err := service.RunCreation(preCancelled, creationID); err == nil {
		t.Fatalf("pre-cancelled RunCreation returned nil, want ctx error for River retry")
	}
	code, queuedOut := call("GET", "/v1/course-creations/"+creationID, nil, token)
	if code != 200 || queuedOut["state"] != "queued" {
		t.Fatalf("expected state=queued after pre-engine cancellation, got code=%d out=%v", code, queuedOut)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT COALESCE(SUM(amount), 0) FROM credit_entries WHERE learner_id=$1::uuid", learnerID).Scan(&balance); err != nil || balance != initialBalance {
		t.Fatalf("expected balance=%d after pre-engine cancel, got %d err=%v", initialBalance, balance, err)
	}

	// Run creation job to completion
	if err := service.RunCreation(ctx, creationID); err != nil {
		t.Fatalf("RunCreation failed: %v", err)
	}

	code, readyOut := call("GET", "/v1/course-creations/"+creationID, nil, token)
	if code != 200 || readyOut["state"] != "enrolled" {
		t.Fatalf("expected enrolled, got %v", readyOut)
	}
	courseID := readyOut["course_id"].(string)

	// 3. Duplicate delivery safety: calling RunCreation again does not duplicate or double charge
	callsBefore := mockEngine.calls
	if err := service.RunCreation(ctx, creationID); err != nil {
		t.Fatalf("duplicate RunCreation error: %v", err)
	}
	if mockEngine.calls != callsBefore {
		t.Fatalf("duplicate RunCreation called engine again")
	}

	// Verify only 1 course and 1 enrollment exist
	var courseCount, enrollmentCount int
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM courses WHERE id=$1::uuid", courseID).Scan(&courseCount); err != nil || courseCount != 1 {
		t.Fatalf("courseCount=%d err=%v", courseCount, err)
	}
	if err := db.Pool.QueryRow(ctx, "SELECT count(*) FROM course_enrollments WHERE course_id=$1::uuid", courseID).Scan(&enrollmentCount); err != nil || enrollmentCount != 1 {
		t.Fatalf("enrollmentCount=%d err=%v", enrollmentCount, err)
	}

	// Set as active course
	_, _ = call("POST", "/v1/courses/"+courseID+"/select", nil, token)

	// 4. Ownership and Course Deletion:
	// Foreign user cannot delete course (404 neutral)
	foreignCode, _ := call("DELETE", "/v1/courses/"+courseID, nil, foreignToken)
	if foreignCode != 404 {
		t.Fatalf("expected 404 for foreign delete, got %d", foreignCode)
	}

	// Owner deletes course
	deleteCode, deleteOut := call("DELETE", "/v1/courses/"+courseID, nil, token)
	if deleteCode != 200 || deleteOut["deleted"] != true {
		t.Fatalf("owner delete code=%d out=%v", deleteCode, deleteOut)
	}

	// Course immediately hidden from GET /v1/courses and GET /v1/courses/{id} returns 404
	listCode, listOut := call("GET", "/v1/courses", nil, token)
	if listCode != 200 {
		t.Fatalf("list code=%d", listCode)
	}
	coursesList := listOut["courses"].([]any)
	for _, c := range coursesList {
		if c.(map[string]any)["id"] == courseID {
			t.Fatalf("deleted course still in course list: %v", c)
		}
	}

	readCode, _ := call("GET", "/v1/courses/"+courseID, nil, token)
	if readCode != 404 {
		t.Fatalf("expected 404 for soft-deleted course, got %d", readCode)
	}

	// Active course was cleared
	var activeCourse *string
	if err := db.Pool.QueryRow(ctx, "SELECT active_course_id::text FROM learner_profiles WHERE learner_id=$1::uuid", learnerID).Scan(&activeCourse); err != nil {
		t.Fatal(err)
	}
	if activeCourse != nil && *activeCourse == courseID {
		t.Fatalf("active course was not cleared upon deletion")
	}

	// 5. Purge grace:
	// A course deleted just now is within the 30-day grace, so it remains in DB with deleted_at set
	var softDeleted bool
	if err := db.Pool.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM courses WHERE id=$1::uuid", courseID).Scan(&softDeleted); err != nil || !softDeleted {
		t.Fatalf("expected deleted_at to be set, err=%v", err)
	}

	// Purge with cutoff 30 days ago does NOT purge recently deleted course
	purgedCount, err := service.PurgeDeletedCourses(ctx, time.Now().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("PurgeDeletedCourses err: %v", err)
	}
	if purgedCount != 0 {
		t.Fatalf("expected 0 purged, got %d", purgedCount)
	}

	// Purge with future cutoff (simulating 30 days passed) permanently purges the course and its cascade
	purgedCount, err = service.PurgeDeletedCourses(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("PurgeDeletedCourses err: %v", err)
	}
	if purgedCount != 1 {
		t.Fatalf("expected 1 purged, got %d", purgedCount)
	}

	// Course row is completely gone from courses
	var exists bool
	if err := db.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM courses WHERE id=$1::uuid)", courseID).Scan(&exists); err != nil || exists {
		t.Fatalf("expected course to be purged, exists=%v err=%v", exists, err)
	}
}
