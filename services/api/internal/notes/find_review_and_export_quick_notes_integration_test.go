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
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestFindReviewAndExportQuickNotesIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	container, err := postgres.Run(ctx, "postgres:18",
		postgres.WithDatabase("notes_e904"),
		postgres.WithUsername("fixture"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
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

	tokenA, learnerA := login("learner-a-e904@example.test")
	tokenB, _ := login("learner-b-e904@example.test")

	// 1. Create notes for Learner A
	createNote := func(token, actionKey, rawText, source string) notes.NoteDTO {
		body, _ := json.Marshal(notes.SaveNoteInput{
			ActionKey: actionKey,
			Source:    source,
			RawText:   rawText,
		})
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		res, err := server.Client().Do(req)
		if err != nil || res.StatusCode != 200 {
			t.Fatalf("save note failed: %v, status: %d", err, res.StatusCode)
		}
		defer func() { _ = res.Body.Close() }()
		var note notes.NoteDTO
		_ = json.NewDecoder(res.Body).Decode(&note)
		return note
	}

	noteA1 := createNote(tokenA, "key-a-1", "Stars move because Earth spins west to east.", "lesson")
	noteA2 := createNote(tokenA, "key-a-2", "Watering schedules depend on seasonal temperature.", "chat")
	_ = createNote(tokenB, "key-b-1", "Learner B secret note.", "chat")

	// Add enrichment to noteA1
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO quick_note_enrichments(note_id, learner_id, summary, tags, links, version)
		VALUES($1::uuid, $2::uuid, 'Earth rotation causes apparent star motion.', ARRAY['rotation', 'stars'], ARRAY['lesson-sky-1'], 1)
	`, noteA1.ID, learnerA)
	if err != nil {
		t.Fatalf("insert enrichment: %v", err)
	}

	// 2. Scoped Search & Filter
	// Search by text query "Earth"
	reqSearch, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes?q=Earth", nil)
	reqSearch.Header.Set("Authorization", "Bearer "+tokenA)
	resSearch, err := server.Client().Do(reqSearch)
	if err != nil || resSearch.StatusCode != 200 {
		t.Fatalf("search notes: %v, code: %d", err, resSearch.StatusCode)
	}
	var searchResults []notes.NoteDTO
	_ = json.NewDecoder(resSearch.Body).Decode(&searchResults)
	_ = resSearch.Body.Close()
	if len(searchResults) != 1 || searchResults[0].ID != noteA1.ID {
		t.Fatalf("expected 1 search result matching 'Earth' (noteA1), got %d", len(searchResults))
	}

	// Filter by tag "rotation"
	reqTag, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes?tag=rotation", nil)
	reqTag.Header.Set("Authorization", "Bearer "+tokenA)
	resTag, err := server.Client().Do(reqTag)
	if err != nil || resTag.StatusCode != 200 {
		t.Fatalf("tag filter: %v, code: %d", err, resTag.StatusCode)
	}
	var tagResults []notes.NoteDTO
	_ = json.NewDecoder(resTag.Body).Decode(&tagResults)
	_ = resTag.Body.Close()
	if len(tagResults) != 1 || tagResults[0].ID != noteA1.ID {
		t.Fatalf("expected 1 tag result for 'rotation', got %d", len(tagResults))
	}

	// Learner B must never see Learner A's notes
	reqB, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes", nil)
	reqB.Header.Set("Authorization", "Bearer "+tokenB)
	resB, _ := server.Client().Do(reqB)
	var bResults []notes.NoteDTO
	_ = json.NewDecoder(resB.Body).Decode(&bResults)
	_ = resB.Body.Close()
	if len(bResults) != 1 || bResults[0].RawText != "Learner B secret note." {
		t.Fatalf("learner scoping violated: %v", bResults)
	}

	// 3. Edit raw text with optimistic revision conflict detection
	// Successful edit: expected_version = 1
	editBody, _ := json.Marshal(map[string]any{
		"raw_text":         "Stars move because Earth spins west to east. Polaris is stationary.",
		"expected_version": 1,
	})
	reqEdit, _ := http.NewRequestWithContext(ctx, http.MethodPatch, server.URL+"/v1/notes/"+noteA1.ID, bytes.NewReader(editBody))
	reqEdit.Header.Set("Authorization", "Bearer "+tokenA)
	reqEdit.Header.Set("Content-Type", "application/json")
	resEdit, err := server.Client().Do(reqEdit)
	if err != nil || resEdit.StatusCode != 200 {
		t.Fatalf("edit note failed: %v, code: %d", err, resEdit.StatusCode)
	}
	var editedNote notes.NoteDTO
	_ = json.NewDecoder(resEdit.Body).Decode(&editedNote)
	_ = resEdit.Body.Close()
	if editedNote.Version != 2 {
		t.Fatalf("expected version 2 after edit, got %d", editedNote.Version)
	}
	// Enrichment should be preserved intact
	if editedNote.Summary == nil || *editedNote.Summary != "Earth rotation causes apparent star motion." {
		t.Fatalf("prior enrichment overwritten on edit: %+v", editedNote)
	}

	// Stale edit conflict: sending expected_version = 1 again should return 409 Conflict
	staleEditBody, _ := json.Marshal(map[string]any{
		"raw_text":         "Stale overwrite attempt.",
		"expected_version": 1,
	})
	reqStale, _ := http.NewRequestWithContext(ctx, http.MethodPatch, server.URL+"/v1/notes/"+noteA1.ID, bytes.NewReader(staleEditBody))
	reqStale.Header.Set("Authorization", "Bearer "+tokenA)
	reqStale.Header.Set("Content-Type", "application/json")
	resStale, err := server.Client().Do(reqStale)
	if err != nil || resStale.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 Conflict on stale version edit, got %d", resStale.StatusCode)
	}
	_ = resStale.Body.Close()

	// 4. Mark reviewed
	reqReview, _ := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes/"+noteA1.ID+"/review", nil)
	reqReview.Header.Set("Authorization", "Bearer "+tokenA)
	resReview, err := server.Client().Do(reqReview)
	if err != nil || resReview.StatusCode != 200 {
		t.Fatalf("mark reviewed failed: %v, code: %d", err, resReview.StatusCode)
	}
	var reviewedNote notes.NoteDTO
	_ = json.NewDecoder(resReview.Body).Decode(&reviewedNote)
	_ = resReview.Body.Close()
	if !reviewedNote.Reviewed || reviewedNote.ReviewedAt == nil {
		t.Fatalf("expected note marked reviewed with timestamp: %+v", reviewedNote)
	}

	// Filter by reviewed=false should only return noteA2
	reqUnreviewed, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes?reviewed=false", nil)
	reqUnreviewed.Header.Set("Authorization", "Bearer "+tokenA)
	resUnreviewed, _ := server.Client().Do(reqUnreviewed)
	var unreviewedList []notes.NoteDTO
	_ = json.NewDecoder(resUnreviewed.Body).Decode(&unreviewedList)
	_ = resUnreviewed.Body.Close()
	if len(unreviewedList) != 1 || unreviewedList[0].ID != noteA2.ID {
		t.Fatalf("expected only noteA2 in unreviewed list, got %d", len(unreviewedList))
	}

	// 5. Export Markdown
	reqExport, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes/"+noteA1.ID+"/export", nil)
	reqExport.Header.Set("Authorization", "Bearer "+tokenA)
	resExport, err := server.Client().Do(reqExport)
	if err != nil || resExport.StatusCode != 200 {
		t.Fatalf("export note: %v, code: %d", err, resExport.StatusCode)
	}
	exportBytes, _ := io.ReadAll(resExport.Body)
	_ = resExport.Body.Close()
	markdown := string(exportBytes)
	if !strings.Contains(markdown, "Your original note") || !strings.Contains(markdown, "AI additions") {
		t.Fatalf("markdown export missing section separation: %s", markdown)
	}
	if !strings.Contains(markdown, "Polaris is stationary") || !strings.Contains(markdown, "Earth rotation causes apparent star motion") {
		t.Fatalf("markdown export missing content: %s", markdown)
	}

	// 6. Delete note
	reqDelete, _ := http.NewRequestWithContext(ctx, http.MethodDelete, server.URL+"/v1/notes/"+noteA1.ID, nil)
	reqDelete.Header.Set("Authorization", "Bearer "+tokenA)
	resDelete, err := server.Client().Do(reqDelete)
	if err != nil || (resDelete.StatusCode != 200 && resDelete.StatusCode != 204) {
		t.Fatalf("delete note failed: %v, code: %d", err, resDelete.StatusCode)
	}
	_ = resDelete.Body.Close()

	// Verify deleted note is no longer returned
	reqGetDeleted, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/notes/"+noteA1.ID, nil)
	reqGetDeleted.Header.Set("Authorization", "Bearer "+tokenA)
	resGetDeleted, _ := server.Client().Do(reqGetDeleted)
	if resGetDeleted.StatusCode != 404 {
		t.Fatalf("expected 404 after note deletion, got %d", resGetDeleted.StatusCode)
	}
	_ = resGetDeleted.Body.Close()
}
