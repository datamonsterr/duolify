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

type testEventsDouble struct {
	mu     sync.Mutex
	events []struct {
		LearnerID  string
		Event      string
		Properties map[string]any
	}
}

func (d *testEventsDouble) Capture(learnerID, event string, properties map[string]any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, struct {
		LearnerID  string
		Event      string
		Properties map[string]any
	}{learnerID, event, properties})
	return nil
}

func (d *testEventsDouble) Close() error { return nil }

func TestSwitchCoursesAndOverrideCourseLevelIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("courses_e203"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	quotes := pricing.NewQuotes(db.Pool)
	events := &testEventsDouble{}
	courseService := courses.New(courses.Options{Pool: db.Pool, Financial: financial, Events: events})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := httpapi.New(httpapi.Dependencies{
		Accounts:    auth,
		CourseBrief: courseService,
		Credits:     financial,
		Quotes:      quotes,
		Postgres:    func(context.Context) error { return nil },
	}, logger)
	server := httptest.NewServer(handler)
	defer server.Close()

	call := func(method, path string, body any, token string) (int, map[string]any) {
		var rdr io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			rdr = bytes.NewReader(raw)
		}
		req, _ := http.NewRequestWithContext(ctx, method, server.URL+path, rdr)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
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

	// Create Learner A and Learner B
	learnerA, tokenA := login("learnera@example.test")
	learnerB, tokenB := login("learnerb@example.test")
	_ = learnerB

	// Set global profiles for Learner A: level = "beginner"
	if _, err = db.Pool.Exec(ctx, `UPDATE learner_profiles SET level='beginner', goal='Learn basics', background='High school' WHERE learner_id=$1::uuid`, learnerA); err != nil {
		t.Fatal(err)
	}

	// Seed Course 1 for Learner A
	var course1ID string
	var creation1ID string
	var quoteID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'ref1', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerA, cat.Config().Version).Scan(&quoteID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerA, quoteID).Scan(&creation1ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title, description) VALUES($1::uuid, $2::uuid, 'Astronomy 101', 'Intro to the sky') RETURNING id::text`, learnerA, creation1ID).Scan(&course1ID); err != nil {
		t.Fatal(err)
	}
	var unit1ID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_units(learner_id, course_id, authored_id, position, title) VALUES($1::uuid, $2::uuid, 'u1', 0, 'Sky Basics') RETURNING id::text`, learnerA, course1ID).Scan(&unit1ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1::uuid, $2::uuid, 'l1', 0, 'Stars', 'Observe the stars', 'ready_to_generate')`, learnerA, unit1ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id, course_id) VALUES($1::uuid, $2::uuid)`, learnerA, course1ID); err != nil {
		t.Fatal(err)
	}

	// Seed Course 2 for Learner A
	var creation2ID, quote2ID, course2ID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'ref2', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerA, cat.Config().Version).Scan(&quote2ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerA, quote2ID).Scan(&creation2ID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title, description) VALUES($1::uuid, $2::uuid, 'Plant Biology', 'Botany basics') RETURNING id::text`, learnerA, creation2ID).Scan(&course2ID); err != nil {
		t.Fatal(err)
	}
	var unit2ID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_units(learner_id, course_id, authored_id, position, title) VALUES($1::uuid, $2::uuid, 'u2', 0, 'Roots') RETURNING id::text`, learnerA, course2ID).Scan(&unit2ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_lesson_stubs(learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1::uuid, $2::uuid, 'l2', 0, 'Shoots', 'Explore root structures', 'ready_to_generate')`, learnerA, unit2ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id, course_id) VALUES($1::uuid, $2::uuid)`, learnerA, course2ID); err != nil {
		t.Fatal(err)
	}

	// 1. Membership check: Learner A gets 2 courses
	code, resp := call("GET", "/v1/courses", nil, tokenA)
	if code != 200 {
		t.Fatalf("expected 200 from GET /v1/courses, got %d", code)
	}
	coursesRaw, ok := resp["courses"].([]any)
	if !ok || len(coursesRaw) != 2 {
		t.Fatalf("expected 2 courses for learner A, got %v", resp)
	}

	// Membership check: Learner B gets 0 courses
	code, respB := call("GET", "/v1/courses", nil, tokenB)
	if code != 200 {
		t.Fatalf("expected 200 from GET /v1/courses for learner B, got %d", code)
	}
	coursesBRaw, ok := respB["courses"].([]any)
	if !ok || len(coursesBRaw) != 0 {
		t.Fatalf("expected 0 courses for learner B, got %v", respB)
	}

	// Neutral foreign access: Learner B trying to access Learner A's course receives 404
	code, _ = call("GET", "/v1/courses/"+course1ID, nil, tokenB)
	if code != 404 {
		t.Fatalf("expected neutral 404 for foreign course access, got %d", code)
	}
	code, _ = call("POST", "/v1/courses/"+course1ID+"/select", nil, tokenB)
	if code != 404 {
		t.Fatalf("expected neutral 404 for foreign course select, got %d", code)
	}
	code, _ = call("POST", "/v1/courses/"+course1ID+"/level", map[string]string{"level": "advanced"}, tokenB)
	if code != 404 {
		t.Fatalf("expected neutral 404 for foreign course level set, got %d", code)
	}

	// 2. Active-course setting
	code, selectResp := call("POST", "/v1/courses/"+course2ID+"/select", nil, tokenA)
	if code != 200 {
		t.Fatalf("expected 200 from POST /v1/courses/{id}/select, got %d", code)
	}
	if selectResp["active_course_id"] != course2ID {
		t.Fatalf("expected active_course_id %s, got %v", course2ID, selectResp["active_course_id"])
	}

	// Verify persistence in Postgres
	var activeCourseInDB string
	if err = db.Pool.QueryRow(ctx, `SELECT COALESCE(active_course_id::text,'') FROM learner_profiles WHERE learner_id=$1::uuid`, learnerA).Scan(&activeCourseInDB); err != nil {
		t.Fatal(err)
	}
	if activeCourseInDB != course2ID {
		t.Fatalf("expected active_course_id in DB to be %s, got %s", course2ID, activeCourseInDB)
	}

	// Verify GET /v1/courses returns active_course_id
	_, respA2 := call("GET", "/v1/courses", nil, tokenA)
	if respA2["active_course_id"] != course2ID {
		t.Fatalf("expected active_course_id %s in GET /v1/courses, got %v", course2ID, respA2["active_course_id"])
	}

	// 3. Level override & reset
	// Set course 1 level override to "advanced"
	code, lvlResp := call("POST", "/v1/courses/"+course1ID+"/level", map[string]string{"level": "advanced"}, tokenA)
	if code != 200 {
		t.Fatalf("expected 200 from setting course level, got %d (%v)", code, lvlResp)
	}
	if lvlResp["level_override"] != "advanced" {
		t.Fatalf("expected level_override 'advanced', got %v", lvlResp["level_override"])
	}

	// Verify Postgres: course_enrollments has level_override = 'advanced'
	var dbOverride string
	if err = db.Pool.QueryRow(ctx, `SELECT level_override FROM course_enrollments WHERE learner_id=$1::uuid AND course_id=$2::uuid`, learnerA, course1ID).Scan(&dbOverride); err != nil {
		t.Fatal(err)
	}
	if dbOverride != "advanced" {
		t.Fatalf("expected db level_override to be 'advanced', got %s", dbOverride)
	}

	// Verify global learner_profiles.level is UNCHANGED ("beginner")
	var globalLevel string
	if err = db.Pool.QueryRow(ctx, `SELECT level FROM learner_profiles WHERE learner_id=$1::uuid`, learnerA).Scan(&globalLevel); err != nil {
		t.Fatal(err)
	}
	if globalLevel != "beginner" {
		t.Fatalf("expected global profile level to remain 'beginner', got %s", globalLevel)
	}

	// Verify analytics event learner_profile_updated was captured
	events.mu.Lock()
	foundEvent := false
	for _, ev := range events.events {
		if ev.Event == "learner_profile_updated" && ev.LearnerID == learnerA {
			foundEvent = true
			if fields, ok := ev.Properties["fields_changed"].([]string); !ok || len(fields) == 0 {
				t.Fatalf("expected fields_changed in learner_profile_updated, got %v", ev.Properties)
			}
		}
	}
	events.mu.Unlock()
	if !foundEvent {
		t.Fatalf("expected learner_profile_updated event to be emitted on level override")
	}

	// 4. Effective Profile Projection
	// For Course 1: level should be "advanced" (overridden)
	effProfile1, err := courseService.EffectiveProfile(ctx, learnerA, course1ID)
	if err != nil {
		t.Fatalf("failed to build effective profile for course 1: %v", err)
	}
	if effProfile1.GetLevel() != "advanced" {
		t.Fatalf("expected effective level 'advanced' for course 1, got %s", effProfile1.GetLevel())
	}
	if effProfile1.GetGoals() != "Learn basics" {
		t.Fatalf("expected global goal preserved, got %s", effProfile1.GetGoals())
	}

	// For Course 2: level should be "beginner" (global, no override)
	effProfile2, err := courseService.EffectiveProfile(ctx, learnerA, course2ID)
	if err != nil {
		t.Fatalf("failed to build effective profile for course 2: %v", err)
	}
	if effProfile2.GetLevel() != "beginner" {
		t.Fatalf("expected effective level 'beginner' for course 2, got %s", effProfile2.GetLevel())
	}

	// Reset course 1 level override back to global
	code, resetResp := call("POST", "/v1/courses/"+course1ID+"/level", map[string]string{"level": ""}, tokenA)
	if code != 200 {
		t.Fatalf("expected 200 from resetting course level, got %d (%v)", code, resetResp)
	}
	if resetResp["level_override"] != "" {
		t.Fatalf("expected level_override empty string on reset, got %v", resetResp["level_override"])
	}

	// After reset, course 1 effective profile should project global "beginner"
	effProfile1Reset, err := courseService.EffectiveProfile(ctx, learnerA, course1ID)
	if err != nil {
		t.Fatalf("failed to build effective profile after reset: %v", err)
	}
	if effProfile1Reset.GetLevel() != "beginner" {
		t.Fatalf("expected effective level 'beginner' after reset, got %s", effProfile1Reset.GetLevel())
	}

	// Verify global level still remains "beginner"
	if err = db.Pool.QueryRow(ctx, `SELECT level FROM learner_profiles WHERE learner_id=$1::uuid`, learnerA).Scan(&globalLevel); err != nil {
		t.Fatal(err)
	}
	if globalLevel != "beginner" {
		t.Fatalf("expected global profile level to remain 'beginner', got %s", globalLevel)
	}
}
