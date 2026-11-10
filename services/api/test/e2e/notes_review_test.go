package e2e

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/httpapi"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/google/uuid"
)

func TestNotesReviewForeignEnrichDoesNotBillOwner(t *testing.T) {
	auth, attackerToken, database := signedInDatabase(t)
	ctx := t.Context()
	if err := worker.Migrate(ctx, database.Pool, slog.Default()); err != nil {
		t.Fatal(err)
	}
	owner := uuid.NewString()
	if _, err := database.Pool.Exec(ctx, "INSERT INTO learners(id,display_name) VALUES($1,'note owner')", owner); err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, nil)
	if _, err = financial.EnsureDailyGrant(ctx, owner, time.Now()); err != nil {
		t.Fatal(err)
	}
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, nil)
	note, err := service.SaveNote(ctx, owner, notes.SaveNoteInput{ActionKey: "foreign-enrich", Source: "chat", RawText: "Private learner note"})
	if err != nil {
		t.Fatal(err)
	}
	var before, after, jobs int64
	if err = database.Pool.QueryRow(ctx, "SELECT coalesce(sum(amount),0) FROM credit_entries WHERE learner_id=$1", owner).Scan(&before); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Notes: service}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/v1/notes/"+note.ID+"/enrich", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+attackerToken)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNotFound {
		t.Errorf("foreign enrich: got %d, want 404", response.StatusCode)
	}
	if err = database.Pool.QueryRow(ctx, "SELECT coalesce(sum(amount),0) FROM credit_entries WHERE learner_id=$1", owner).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after-before != 0 {
		t.Errorf("foreign enrich ledger delta = %d, want 0", after-before)
	}
	if err = database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_jobs WHERE learner_id=$1", owner).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if jobs != 0 {
		t.Errorf("foreign enrich created %d reservations, want 0", jobs)
	}
}

func TestNotesReviewPatchRequiresVersion(t *testing.T) {
	auth, token, database := signedInDatabase(t)
	claims, err := auth.ParseAccess(token)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Migrate(t.Context(), database.Pool, slog.Default()); err != nil {
		t.Fatal(err)
	}
	service := notes.NewService(database.Pool, nil)
	server := httptest.NewServer(httpapi.New(httpapi.Dependencies{Accounts: auth, Notes: service}, slog.New(slog.NewTextHandler(io.Discard, nil))))
	defer server.Close()
	for _, body := range []string{`{"raw_text":"Blind overwrite"}`, `{"raw_text":"Blind overwrite","expected_version":0}`, `{"raw_text":"Blind overwrite","expected_version":-1}`} {
		note, e := service.SaveNote(t.Context(), claims.Subject, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Original"})
		if e != nil {
			t.Fatal(e)
		}
		req, e := http.NewRequestWithContext(t.Context(), http.MethodPatch, server.URL+"/v1/notes/"+note.ID, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response, e := server.Client().Do(req)
		if e != nil {
			t.Fatal(e)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("PATCH %s returned %d; want 400", body, response.StatusCode)
		}
		refreshed, e := service.GetNote(t.Context(), claims.Subject, note.ID)
		if e != nil {
			t.Fatal(e)
		}
		if refreshed.RawText != "Original" || refreshed.Version != note.Version {
			t.Error("invalid version overwrote note")
		}
	}
}
