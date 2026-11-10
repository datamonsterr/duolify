//go:build integration

package notes_test

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
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type bookmarksTestEventsDouble struct {
	mu     sync.Mutex
	events []capturedEvent
}

func (d *bookmarksTestEventsDouble) Capture(learnerID, event string, properties map[string]any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, capturedEvent{learnerID, event, properties})
	return nil
}

func (d *bookmarksTestEventsDouble) Close() error { return nil }

func TestBookmarkLessonsAndExactSlidesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("bookmarks_e905"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	auth, err := accounts.New(db.Pool, strings.Repeat("test-key-32-chars-long-minimum!", 2), mail)
	if err != nil {
		t.Fatal(err)
	}

	events := &bookmarksTestEventsDouble{}
	notesService := notes.NewService(db.Pool, events)

	handler := httpapi.New(httpapi.Dependencies{
		Accounts: auth,
		Notes:    notesService,
		Postgres: db.Ping,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	server := httptest.NewServer(handler)
	defer server.Close()

	login := func(email string) (string, string) {
		codeReq, _ := json.Marshal(map[string]any{"email": email, "age_confirmed": true})
		reqCode, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/auth/code", bytes.NewReader(codeReq))
		if err != nil {
			t.Fatalf("new code req: %v", err)
		}
		reqCode.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(reqCode)
		if err != nil || res.StatusCode != 202 {
			t.Fatalf("auth code failed: %v, code: %d", err, res.StatusCode)
		}
		_ = res.Body.Close()
		verifyReq, _ := json.Marshal(map[string]any{"email": email, "code": mail.Code(email), "age_confirmed": true})
		reqVerify, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/auth/verify", bytes.NewReader(verifyReq))
		if err != nil {
			t.Fatalf("new verify req: %v", err)
		}
		reqVerify.Header.Set("Content-Type", "application/json")
		res, err = server.Client().Do(reqVerify)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("auth verify failed: %v, code: %d", err, res.StatusCode)
		}
		defer func() { _ = res.Body.Close() }()
		var tokens accounts.Tokens
		_ = json.NewDecoder(res.Body).Decode(&tokens)
		var learnerID string
		_ = db.Pool.QueryRow(ctx, "SELECT learner_id::text FROM auth_identities WHERE email=$1", email).Scan(&learnerID)
		return tokens.AccessToken, learnerID
	}

	tokenA, learnerA := login("learner-a@example.test")
	tokenB, _ := login("learner-b@example.test")

	cat, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}

	// Create course, unit, and lesson stub for Learner A
	var quoteID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'ref1', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerA, cat.Config().Version).Scan(&quoteID)
	if err != nil {
		t.Fatalf("insert quote: %v", err)
	}
	var creationAID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerA, quoteID).Scan(&creationAID)
	if err != nil {
		t.Fatalf("insert creation: %v", err)
	}
	var courseAID string
	err = db.Pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title) VALUES($1::uuid, $2::uuid, 'Astronomy') RETURNING id::text`, learnerA, creationAID).Scan(&courseAID)
	if err != nil {
		t.Fatalf("insert course: %v", err)
	}
	unitAID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `INSERT INTO course_units(id, learner_id, course_id, authored_id, position, title) VALUES($1, $2, $3, 'u1', 0, 'Sky')`, unitAID, learnerA, courseAID)
	if err != nil {
		t.Fatalf("insert unit: %v", err)
	}
	lessonAID := uuid.NewString()
	_, err = db.Pool.Exec(ctx, `INSERT INTO course_lesson_stubs(id, learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1, $2, $3, 'l1', 0, 'Stars', 'Goal', 'ready_to_generate')`, lessonAID, learnerA, unitAID)
	if err != nil {
		t.Fatalf("insert lesson: %v", err)
	}

	// Helper for authenticated API calls
	apiCall := func(method, path string, body any, token string) (int, map[string]any, []map[string]any) {
		var r io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			r = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, server.URL+path, r)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := server.Client().Do(req)
		if err != nil {
			t.Fatalf("apiCall %s %s: %v", method, path, err)
		}
		defer func() { _ = res.Body.Close() }()
		respBytes, _ := io.ReadAll(res.Body)
		var single map[string]any
		var list []map[string]any
		_ = json.Unmarshal(respBytes, &single)
		_ = json.Unmarshal(respBytes, &list)
		return res.StatusCode, single, list
	}

	// 1. Missing-target authorization:
	// Learner B attempts to bookmark Learner A's lesson
	status, _, _ := apiCall(http.MethodPost, "/v1/bookmarks", map[string]any{
		"action_key":  "act-b-unauth",
		"lesson_id":   lessonAID,
		"slide_id":    1,
		"target_type": "slide",
	}, tokenB)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for unauthorized lesson, got %d", status)
	}

	// Non-existent lesson UUID
	status, _, _ = apiCall(http.MethodPost, "/v1/bookmarks", map[string]any{
		"action_key":  "act-b-nonexistent",
		"lesson_id":   uuid.NewString(),
		"slide_id":    0,
		"target_type": "slide",
	}, tokenB)
	if status != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden for non-existent lesson, got %d", status)
	}

	// 2. Postgres target uniqueness & offline queue replay:
	// Learner A creates a bookmark for slide 1
	status, bm1, _ := apiCall(http.MethodPost, "/v1/bookmarks", map[string]any{
		"action_key":  "act-a-1",
		"course_id":   courseAID,
		"lesson_id":   lessonAID,
		"slide_id":    1,
		"target_type": "slide",
		"slide_title": "Slide 2 · Stars",
	}, tokenA)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("expected 200/201 on save bookmark, got %d: %v", status, bm1)
	}
	bm1ID, ok := bm1["id"].(string)
	if !ok || bm1ID == "" {
		t.Fatalf("expected bookmark id, got %v", bm1)
	}

	// Duplicate action_key replay (offline queue reconnect retry)
	status, bmReplay, _ := apiCall(http.MethodPost, "/v1/bookmarks", map[string]any{
		"action_key":  "act-a-1",
		"course_id":   courseAID,
		"lesson_id":   lessonAID,
		"slide_id":    1,
		"target_type": "slide",
	}, tokenA)
	if status != http.StatusOK {
		t.Fatalf("expected 200 on action_key replay, got %d", status)
	}
	if bmReplay["id"] != bm1ID {
		t.Fatalf("expected replaying action_key to yield same bookmark, got %v vs %v", bmReplay["id"], bm1ID)
	}

	// Same target with different action_key (target uniqueness)
	status, bmDupTarget, _ := apiCall(http.MethodPost, "/v1/bookmarks", map[string]any{
		"action_key":  "act-a-2-different-key",
		"course_id":   courseAID,
		"lesson_id":   lessonAID,
		"slide_id":    1,
		"target_type": "slide",
	}, tokenA)
	if status != http.StatusOK {
		t.Fatalf("expected 200 on duplicate target, got %d", status)
	}
	if bmDupTarget["id"] != bm1ID {
		t.Fatalf("expected duplicate target to yield same bookmark %v, got %v", bm1ID, bmDupTarget["id"])
	}

	// Verify only 1 row exists in DB for this target
	var count int
	err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM bookmarks WHERE learner_id=$1::uuid AND lesson_id=$2::uuid AND slide_id=1", learnerA, lessonAID).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected exactly 1 bookmark in DB for target, count=%d, err=%v", count, err)
	}

	// Bookmark a different slide (slide 2) - should create distinct bookmark
	status, bmSlide2, _ := apiCall(http.MethodPost, "/v1/bookmarks", map[string]any{
		"action_key":  "act-a-slide2",
		"course_id":   courseAID,
		"lesson_id":   lessonAID,
		"slide_id":    2,
		"target_type": "slide",
	}, tokenA)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("expected 200/201 on slide 2, got %d", status)
	}
	if bmSlide2["id"] == bm1ID {
		t.Fatal("expected distinct bookmark id for slide 2")
	}

	// Bookmark lesson-level (target_type = "lesson")
	status, bmLesson, _ := apiCall(http.MethodPost, "/v1/bookmarks", map[string]any{
		"action_key":  "act-a-lesson",
		"course_id":   courseAID,
		"lesson_id":   lessonAID,
		"target_type": "lesson",
	}, tokenA)
	if status != http.StatusOK && status != http.StatusCreated {
		t.Fatalf("expected 200/201 on lesson target, got %d", status)
	}
	if bmLesson["id"] == bm1ID || bmLesson["id"] == bmSlide2["id"] {
		t.Fatal("expected distinct bookmark id for lesson target")
	}

	// 3. Course filtering:
	// GET /v1/bookmarks without filter returns all 3 bookmarks
	status, _, allList := apiCall(http.MethodGet, "/v1/bookmarks", nil, tokenA)
	if status != http.StatusOK || len(allList) != 3 {
		t.Fatalf("expected 3 bookmarks in list, got %d (code %d)", len(allList), status)
	}

	// Filter by courseAID
	status, _, filteredList := apiCall(http.MethodGet, "/v1/bookmarks?course_id="+courseAID, nil, tokenA)
	if status != http.StatusOK || len(filteredList) != 3 {
		t.Fatalf("expected 3 bookmarks matching courseAID, got %d", len(filteredList))
	}

	// Filter by random other course
	status, _, emptyList := apiCall(http.MethodGet, "/v1/bookmarks?course_id="+uuid.NewString(), nil, tokenA)
	if status != http.StatusOK || len(emptyList) != 0 {
		t.Fatalf("expected 0 bookmarks for other course, got %d", len(emptyList))
	}

	// 4. Missing / deleted target labelled unavailable:
	// Delete the lesson stub
	_, err = db.Pool.Exec(ctx, "DELETE FROM course_lesson_stubs WHERE id=$1", lessonAID)
	if err != nil {
		t.Fatalf("delete lesson stub: %v", err)
	}

	// Query bookmarks list again - targets must be marked available: false
	status, _, updatedList := apiCall(http.MethodGet, "/v1/bookmarks", nil, tokenA)
	if status != http.StatusOK {
		t.Fatalf("expected 200 on list after lesson deletion, got %d", status)
	}
	for _, item := range updatedList {
		if avail, ok := item["available"].(bool); !ok || avail {
			t.Fatalf("expected bookmark for deleted lesson to be unavailable, got: %v", item)
		}
	}

	// Unavailable bookmark remains removable
	status, _, _ = apiCall(http.MethodDelete, "/v1/bookmarks/"+bm1ID, nil, tokenA)
	if status != http.StatusOK && status != http.StatusNoContent {
		t.Fatalf("expected 200/204 on delete bookmark, got %d", status)
	}

	// Verify removal from DB
	err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM bookmarks WHERE id=$1::uuid", bm1ID).Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("expected bookmark to be deleted from DB, count=%d", count)
	}

	// 5. Verify analytics events recorded
	events.mu.Lock()
	captured := events.events
	events.mu.Unlock()

	var addedCount, removedCount int
	for _, ev := range captured {
		switch ev.Event {
		case "bookmark_added":
			addedCount++
			if ev.Properties["target_type"] == nil {
				t.Fatalf("missing target_type in bookmark_added: %v", ev.Properties)
			}
		case "bookmark_removed":
			removedCount++
			if ev.Properties["target_type"] == nil {
				t.Fatalf("missing target_type in bookmark_removed: %v", ev.Properties)
			}
		}
	}
	if addedCount < 3 {
		t.Fatalf("expected at least 3 bookmark_added events, got %d", addedCount)
	}
	if removedCount < 1 {
		t.Fatalf("expected at least 1 bookmark_removed event, got %d", removedCount)
	}
}
