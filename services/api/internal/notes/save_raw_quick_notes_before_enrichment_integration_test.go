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

type testEventsDouble struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	LearnerID  string
	Event      string
	Properties map[string]any
}

func (d *testEventsDouble) Capture(learnerID, event string, properties map[string]any) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, capturedEvent{learnerID, event, properties})
	return nil
}

func (d *testEventsDouble) Close() error { return nil }

func TestSaveRawQuickNotesBeforeEnrichmentIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("notes_e901"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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

	events := &testEventsDouble{}
	notesService := notes.NewService(db.Pool, events)

	handler := httpapi.New(httpapi.Dependencies{
		Accounts: auth,
		Notes:    notesService,
		Postgres: db.Ping,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	server := httptest.NewServer(handler)
	defer server.Close()

	// Helper to login and get token + learnerID
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

	// Create a course, unit, and lesson stub for Learner A
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

	// 1. Save raw quick note with authorized lesson and slide
	rawNoteA := map[string]any{
		"action_key":  "action-key-1",
		"course_id":   courseAID,
		"lesson_id":   lessonAID,
		"slide_id":    1,
		"slide_title": "Slide 1 · Why the sky changes",
		"source":      "slide",
		"raw_text":    "Stars move because Earth spins west to east.",
	}
	body, _ := json.Marshal(rawNoteA)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokenA)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("save note request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 200 or 201, got %d", resp.StatusCode)
	}
	var createdNote map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&createdNote)
	noteID, ok := createdNote["id"].(string)
	if !ok || noteID == "" {
		t.Fatalf("expected note id in response, got %v", createdNote)
	}
	if createdNote["status"] != "enrichment_pending" {
		t.Fatalf("expected status enrichment_pending, got %v", createdNote["status"])
	}

	// 2. Idempotent sync: submit identical note with action_key again
	req2, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes", bytes.NewReader(body))
	req2.Header.Set("Authorization", "Bearer "+tokenA)
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("second save request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()
	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusCreated {
		t.Fatalf("expected 200 or 201 on idempotent retry, got %d", resp2.StatusCode)
	}
	var note2 map[string]any
	_ = json.NewDecoder(resp2.Body).Decode(&note2)
	if note2["id"] != noteID {
		t.Fatalf("expected same note id %q, got %q", noteID, note2["id"])
	}

	// Verify only 1 row exists in Postgres for action-key-1
	var count int
	err = db.Pool.QueryRow(ctx, "SELECT count(*) FROM quick_notes WHERE learner_id=$1 AND action_key=$2", learnerA, "action-key-1").Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("expected count 1 in DB, got count=%d, err=%v", count, err)
	}

	// 3. Postgres Note Ownership: Learner B cannot view Learner A's note
	reqGetB, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes/"+noteID, nil)
	reqGetB.Header.Set("Authorization", "Bearer "+tokenB)
	respGetB, err := http.DefaultClient.Do(reqGetB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = respGetB.Body.Close() }()
	if respGetB.StatusCode != http.StatusNotFound && respGetB.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 404 or 403 when Learner B accesses Learner A's note, got %d", respGetB.StatusCode)
	}

	// 4. Source validation: Learner B cannot save a note referencing Learner A's unauthorized lesson
	unauthNote := map[string]any{
		"action_key": "action-key-unauth",
		"course_id":  courseAID,
		"lesson_id":  lessonAID,
		"slide_id":   1,
		"source":     "slide",
		"raw_text":   "Attempting to attach note to unauthorized lesson",
	}
	unauthBody, _ := json.Marshal(unauthNote)
	reqUnauth, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes", bytes.NewReader(unauthBody))
	reqUnauth.Header.Set("Authorization", "Bearer "+tokenB)
	reqUnauth.Header.Set("Content-Type", "application/json")
	respUnauth, err := http.DefaultClient.Do(reqUnauth)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = respUnauth.Body.Close() }()
	if respUnauth.StatusCode != http.StatusForbidden && respUnauth.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 403 or 400 for unauthorized lesson source, got %d", respUnauth.StatusCode)
	}

	// 5. Input boundary validation: text over 2000 chars rejected
	tooLongNote := map[string]any{
		"action_key": "action-key-toolong",
		"source":     "chat",
		"raw_text":   strings.Repeat("a", 2001),
	}
	tooLongBody, _ := json.Marshal(tooLongNote)
	reqTooLong, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes", bytes.NewReader(tooLongBody))
	reqTooLong.Header.Set("Authorization", "Bearer "+tokenA)
	reqTooLong.Header.Set("Content-Type", "application/json")
	respTooLong, err := http.DefaultClient.Do(reqTooLong)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = respTooLong.Body.Close() }()
	if respTooLong.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for text > 2000 chars, got %d", respTooLong.StatusCode)
	}
}
