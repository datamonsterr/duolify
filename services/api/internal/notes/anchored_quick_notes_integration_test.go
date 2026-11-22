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
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestAnchoredQuickNotesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("notes_anchor"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	if err = worker.Migrate(ctx, db.Pool, slog.Default()); err != nil {
		t.Fatal(err)
	}

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

	post := func(token string, payload map[string]any) (int, map[string]any) {
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	list := func(token string) []map[string]any {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var out []map[string]any
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			var wrapped struct {
				Notes []map[string]any `json:"notes"`
			}
			t.Fatalf("decode list: %v %v", err, wrapped)
		}
		return out
	}
	base := func(key string, anchor map[string]any) map[string]any {
		return map[string]any{"action_key": key, "course_id": courseAID, "lesson_id": lessonAID, "slide_id": 2, "slide_title": "Slide 3", "source": "slide", "raw_text": "Why does it turn orange?", "anchor": anchor}
	}

	// A typed note on a region saves with its anchor and the analytics event carries no text.
	code, created := post(tokenA, base("anchored-1", map[string]any{"playback_ms": 12500, "input": "typed", "rect": map[string]any{"x": 0.1, "y": 0.2, "w": 0.4, "h": 0.3}}))
	if code != http.StatusOK {
		t.Fatalf("save anchored note: %d %v", code, created)
	}
	anchor, _ := created["anchor"].(map[string]any)
	if anchor["playback_ms"] != 12500.0 || anchor["input"] != "typed" || anchor["source_state"] != "available" {
		t.Fatalf("anchor not returned: %v", created)
	}
	noteID := created["id"].(string)

	// Retrying the same action key (offline queue replay) keeps one note and one anchor.
	code, again := post(tokenA, base("anchored-1", map[string]any{"playback_ms": 99, "input": "voice"}))
	if code != http.StatusOK || again["id"] != noteID {
		t.Fatalf("replay: %d %v", code, again)
	}
	var anchors int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM quick_note_anchors WHERE note_id=$1::uuid", noteID).Scan(&anchors)
	if anchors != 1 {
		t.Fatalf("want 1 anchor row, got %d", anchors)
	}
	var ms int
	_ = db.Pool.QueryRow(ctx, "SELECT playback_ms FROM quick_note_anchors WHERE note_id=$1::uuid", noteID).Scan(&ms)
	if ms != 12500 {
		t.Fatalf("replay overwrote the anchor: %d", ms)
	}
	events.mu.Lock()
	var anchored []capturedEvent
	for _, e := range events.events {
		if e.Event == "lesson_anchored_note_created" {
			anchored = append(anchored, e)
		}
	}
	events.mu.Unlock()
	if len(anchored) != 1 {
		t.Fatalf("want one lesson_anchored_note_created, got %d", len(anchored))
	}
	for k, v := range anchored[0].Properties {
		if s, ok := v.(string); ok && strings.Contains(s, "orange") {
			t.Fatalf("event %s carries note text", k)
		}
	}
	if anchored[0].Properties["has_region"] != true || anchored[0].Properties["entry_point"] != "typed" || anchored[0].Properties["slide_index"] != 2 {
		t.Fatalf("event properties: %v", anchored[0].Properties)
	}

	// Invalid anchors are rejected without a note.
	for name, a := range map[string]map[string]any{
		"outside stage": {"playback_ms": 1, "input": "typed", "rect": map[string]any{"x": 0.9, "y": 0, "w": 0.5, "h": 0.5}},
		"bad input":     {"playback_ms": 1, "input": "telepathy"},
		"negative time": {"playback_ms": -5, "input": "typed"},
	} {
		if code, out := post(tokenA, base("bad-"+name, a)); code != http.StatusBadRequest {
			t.Fatalf("%s: want 400, got %d %v", name, code, out)
		}
	}
	chat := base("bad-chat", map[string]any{"playback_ms": 1, "input": "typed"})
	chat["source"] = "chat"
	delete(chat, "slide_id")
	if code, _ := post(tokenA, chat); code != http.StatusBadRequest {
		t.Fatalf("anchor on a chat note: want 400, got %d", code)
	}
	// Another Learner cannot anchor to this Lesson.
	if code, _ := post(tokenB, base("foreign", map[string]any{"playback_ms": 1, "input": "typed"})); code != http.StatusForbidden {
		t.Fatalf("foreign Lesson: want 403, got %d", code)
	}
	var total int
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM quick_notes WHERE learner_id=$1::uuid", learnerA).Scan(&total)
	if total != 1 {
		t.Fatalf("rejected anchors must not create notes, have %d", total)
	}

	// The list carries the anchor; a removed Lesson leaves a readable source-unavailable note.
	notesList := list(tokenA)
	if len(notesList) != 1 || notesList[0]["anchor"] == nil {
		t.Fatalf("list lacks anchor: %v", notesList)
	}
	if _, err = db.Pool.Exec(ctx, "DELETE FROM course_lesson_stubs WHERE id=$1::uuid", lessonAID); err != nil {
		t.Fatal(err)
	}
	notesList = list(tokenA)
	gone, _ := notesList[0]["anchor"].(map[string]any)
	if len(notesList) != 1 || gone["source_state"] != "unavailable" || notesList[0]["raw_text"] != "Why does it turn orange?" || notesList[0]["lesson_id"] != nil {
		t.Fatalf("removed lesson: %v", notesList)
	}

	// Erasing the note erases its anchor.
	if _, err = db.Pool.Exec(ctx, "DELETE FROM quick_notes WHERE id=$1::uuid", noteID); err != nil {
		t.Fatal(err)
	}
	_ = db.Pool.QueryRow(ctx, "SELECT count(*) FROM quick_note_anchors WHERE learner_id=$1::uuid", learnerA).Scan(&anchors)
	if anchors != 0 {
		t.Fatalf("anchor outlived its note")
	}
}
