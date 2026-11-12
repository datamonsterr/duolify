//go:build integration

package notes_test

import (
	"context"
	"errors"
	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestNotesReviewSettlementErrorRemainsRecoverable(t *testing.T) {
	database := reviewDatabase(t)
	learner, financial := reviewLearner(t, database)
	fixture := &reviewEngine{enrich: func(context.Context, *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
		return nil, errors.New("provider failure")
	}}
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, fixture)
	note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Recover failed settlement"})
	if err != nil {
		t.Fatal(err)
	}
	before := reviewBalance(t, database, learner)
	if _, err = database.Pool.Exec(t.Context(), `CREATE FUNCTION fail_note_settle() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected settlement failure'; END $$; CREATE TRIGGER fail_note_settle BEFORE INSERT ON credit_usage FOR EACH ROW EXECUTE FUNCTION fail_note_settle()`); err != nil {
		t.Fatal(err)
	}
	err = service.ProcessEnrichment(t.Context(), learner, note.ID)
	if err == nil || !strings.Contains(err.Error(), "injected settlement failure") {
		t.Errorf("settlement failure swallowed: %v", err)
	}
	var retained bool
	if err = database.Pool.QueryRow(t.Context(), "SELECT reservation_id IS NOT NULL AND quote_id IS NOT NULL FROM quick_notes WHERE id=$1", note.ID).Scan(&retained); err != nil {
		t.Fatal(err)
	}
	if !retained {
		t.Error("failed settlement lost reservation needed for recovery")
	}
	if _, err = database.Pool.Exec(t.Context(), "DROP TRIGGER fail_note_settle ON credit_usage; DROP FUNCTION fail_note_settle()"); err != nil {
		t.Fatal(err)
	}
	err = service.ProcessEnrichment(t.Context(), learner, note.ID)
	if err == nil || !strings.Contains(err.Error(), "provider failure") {
		t.Errorf("recovery did not reach provider: %v", err)
	}
	if delta := reviewBalance(t, database, learner) - before; delta != 0 {
		t.Errorf("failed delivery recovery ledger delta=%d want 0", delta)
	}
	var active int
	if err = database.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state IN ('reserved','running')", learner).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Errorf("recovery left %d active reservations", active)
	}
}

func TestNotesReviewQueueFailureRollsBackSaveAndEdit(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	service := notes.NewService(database.Pool, nil)
	input := notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Original"}
	existing, err := service.SaveNote(t.Context(), learner, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Pool.Exec(t.Context(), `CREATE FUNCTION fail_notes_queue() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected River failure'; END $$; CREATE TRIGGER fail_notes_queue BEFORE INSERT ON river_job FOR EACH ROW EXECUTE FUNCTION fail_notes_queue()`); err != nil {
		t.Fatal(err)
	}
	// A replay must return without attempting to enqueue, even while River fails.
	if _, err = service.SaveNote(t.Context(), learner, input); err != nil {
		t.Errorf("replay attempted a River insert: %v", err)
	}
	input.ActionKey = uuid.NewString()
	if _, err = service.SaveNote(t.Context(), learner, input); err == nil || !strings.Contains(err.Error(), "injected River failure") {
		t.Errorf("save swallowed queue error: %v", err)
	}
	if _, err = service.UpdateNote(t.Context(), learner, existing.ID, notes.UpdateNoteInput{RawText: "Unqueued edit", ExpectedVersion: existing.Version}); err == nil || !strings.Contains(err.Error(), "injected River failure") {
		t.Errorf("edit swallowed queue error: %v", err)
	}
	var saved int
	if err = database.Pool.QueryRow(t.Context(), "SELECT count(*) FROM quick_notes WHERE learner_id=$1", learner).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if saved != 1 {
		t.Errorf("failed enqueue persisted %d notes; want only original", saved)
	}
	refreshed, err := service.GetNote(t.Context(), learner, existing.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.RawText != "Original" || refreshed.Version != existing.Version {
		t.Error("failed enqueue persisted an edit")
	}
}

type reviewLimitsFailure struct{ notes.EnrichmentCredits }

func (reviewLimitsFailure) Limits(context.Context, string, string, string) (*enginev1.UsageLimits, error) {
	return nil, credits.ErrReservation
}

func TestNotesReviewCheckedFailurePaths(t *testing.T) {
	database := reviewDatabase(t)
	for _, mode := range []string{"attach-status", "claim", "limits", "final-status", "reset-status"} {
		t.Run(mode, func(t *testing.T) {
			learner, financial := reviewLearner(t, database)
			calls := 0
			fixture := &reviewEngine{enrich: func(_ context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
				calls++
				if mode == "reset-status" {
					return nil, errors.New("provider failure")
				}
				return reviewResponse(req), nil
			}}
			var creditClient notes.EnrichmentCredits = financial
			if mode == "limits" {
				creditClient = reviewLimitsFailure{financial}
			}
			service := notes.NewServiceWithEnrichment(database.Pool, nil, creditClient, fixture)
			note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Injected failure"})
			if err != nil {
				t.Fatal(err)
			}
			table := "quick_notes"
			condition := "NEW.reservation_id IS NOT NULL AND OLD.reservation_id IS NULL"
			body := `RAISE EXCEPTION 'injected checked failure';`
			switch mode {
			case "claim":
				table = "credit_jobs"
				condition = "NEW.state='running'"
			case "final-status":
				condition = "NEW.status='enriched'"
			case "reset-status":
				condition = "NEW.status='enrichment_failed'"
			}
			if mode != "limits" {
				if _, err = database.Pool.Exec(t.Context(), `CREATE FUNCTION fail_note_path() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN `+body+` END $$; CREATE TRIGGER fail_note_path BEFORE UPDATE ON `+table+` FOR EACH ROW WHEN (`+condition+`) EXECUTE FUNCTION fail_note_path()`); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, e := database.Pool.Exec(t.Context(), "DROP TRIGGER fail_note_path ON "+table+"; DROP FUNCTION fail_note_path()"); e != nil {
						t.Error(e)
					}
				}()
			}
			before := reviewBalance(t, database, learner)
			err = service.ProcessEnrichment(t.Context(), learner, note.ID)
			if mode == "limits" {
				if !errors.Is(err, credits.ErrReservation) {
					t.Errorf("limits error ignored: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), "injected checked failure") {
				t.Errorf("write error ignored: %v", err)
			}
			if mode == "attach-status" || mode == "claim" || mode == "limits" {
				if calls != 0 {
					t.Error("engine called after early failure")
				}
			}
			if delta := reviewBalance(t, database, learner) - before; delta != 0 {
				t.Errorf("failure ledger delta=%d want 0", delta)
			}
			var enrichments, active int
			if err = database.Pool.QueryRow(t.Context(), "SELECT count(*) FROM quick_note_enrichments WHERE note_id=$1", note.ID).Scan(&enrichments); err != nil {
				t.Fatal(err)
			}
			if err = database.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state IN ('reserved','running')", learner).Scan(&active); err != nil {
				t.Fatal(err)
			}
			if enrichments != 0 || active != 0 {
				t.Errorf("failure left enrichments=%d active=%d", enrichments, active)
			}
		})
	}
}
