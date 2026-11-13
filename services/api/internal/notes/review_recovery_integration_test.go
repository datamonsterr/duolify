//go:build integration

package notes_test

import (
	"context"
	"errors"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"sync/atomic"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/google/uuid"
)

func TestNotesReviewEarlyFailuresResetStatus(t *testing.T) {
	database := reviewDatabase(t)
	for _, mode := range []string{"missing-credits", "quote-error", "consent-error", "insufficient-credits"} {
		t.Run(mode, func(t *testing.T) {
			learner, financial := reviewLearner(t, database)
			var creditClient notes.EnrichmentCredits = financial
			if mode == "missing-credits" {
				creditClient = nil
			}
			service := notes.NewServiceWithEnrichment(database.Pool, nil, creditClient, nil)
			note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Recover this note"})
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "quote-error", "consent-error":
				table := "pricing_quotes"
				if mode == "consent-error" {
					table = "pricing_quote_consents"
				}
				if _, err = database.Pool.Exec(t.Context(), `CREATE FUNCTION fail_early_note() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected early failure'; END $$; CREATE TRIGGER fail_early_note BEFORE INSERT ON `+table+` FOR EACH ROW EXECUTE FUNCTION fail_early_note()`); err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, e := database.Pool.Exec(t.Context(), "DROP TRIGGER fail_early_note ON "+table+"; DROP FUNCTION fail_early_note()"); e != nil {
						t.Error(e)
					}
				}()
			case "insufficient-credits":
				if _, err = database.Pool.Exec(t.Context(), `INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version,provenance)
 SELECT learner_id,-amount,'spend',$2||id::text,pricing_version,jsonb_build_object('source_id',id::text,'bucket',CASE kind WHEN 'daily_grant' THEN 'daily_free' ELSE 'purchase' END,'date',coalesce(provenance->>'date',''))
 FROM credit_entries WHERE learner_id=$1 AND kind IN ('daily_grant','purchase')`, learner, uuid.NewString()); err != nil {
					t.Fatal(err)
				}
			}
			before := reviewBalance(t, database, learner)
			if _, err = service.ResumePendingEnrichments(t.Context(), learner); err == nil && mode != "insufficient-credits" {
				t.Error("early failure swallowed")
			}
			refreshed, e := service.GetNote(t.Context(), learner, note.ID)
			if e != nil {
				t.Fatal(e)
			}
			if refreshed.Status == "enriching" {
				t.Error("early failure stranded note in enriching")
			}
			if mode == "insufficient-credits" && refreshed.Status != "enrichment_pending" {
				t.Error("insufficient credit note must remain pending")
			}
			if delta := reviewBalance(t, database, learner) - before; delta != 0 {
				t.Errorf("early failure ledger delta=%d want 0", delta)
			}
		})
	}
}

func TestNotesReviewLeaseFencesLateWorker(t *testing.T) {
	database := reviewDatabase(t)
	learner, financial := reviewLearner(t, database)
	entered, finish := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	fixture := &reviewEngine{enrich: func(ctx context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
		n := calls.Add(1)
		if n == 1 {
			close(entered)
			select {
			case <-finish:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		response := reviewResponse(req)
		if n == 1 {
			response.Summary = "Late result"
		} else {
			response.Summary = "Replacement result"
		}
		return response, nil
	}}
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, fixture)
	note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Recover crashed lease"})
	if err != nil {
		t.Fatal(err)
	}
	before := reviewBalance(t, database, learner)
	first := make(chan error, 1)
	go func() { first <- service.ProcessEnrichment(t.Context(), learner, note.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("first worker never started")
	}
	if _, err = database.Pool.Exec(t.Context(), "UPDATE quick_notes SET enrichment_lease_until=clock_timestamp()-interval '1 second' WHERE id=$1", note.ID); err != nil {
		t.Fatal(err)
	}
	err = service.ProcessEnrichment(t.Context(), learner, note.ID)
	close(finish)
	if err != nil {
		t.Fatal(err)
	}
	if err = <-first; err == nil {
		t.Error("late worker reported delivered result")
	}
	refreshed, err := service.GetNote(t.Context(), learner, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "enriched" || refreshed.Summary == nil || *refreshed.Summary != "Replacement result" {
		t.Error("late worker overwrote replacement delivery")
	}
	var spends int
	if err = database.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='spend'", learner).Scan(&spends); err != nil {
		t.Fatal(err)
	}
	if spends != 1 || reviewBalance(t, database, learner) >= before {
		t.Errorf("lease recovery spends=%d; want one charge", spends)
	}
}

func TestNotesReviewExpiredEnrichmentLease(t *testing.T) {
	database := reviewDatabase(t)
	learner, financial := reviewLearner(t, database)
	calls := 0
	fixture := &reviewEngine{enrich: func(_ context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
		calls++
		return reviewResponse(req), nil
	}}
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, fixture)
	note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "A crashed worker note"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Pool.Exec(t.Context(), "UPDATE quick_notes SET status='enriching',updated_at=now()-interval '1 hour' WHERE id=$1", note.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.ResumePendingEnrichments(t.Context(), learner); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.GetNote(t.Context(), learner, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "enriched" || calls != 1 {
		t.Errorf("expired enrichment remained %s, engine calls=%d; want enriched once", refreshed.Status, calls)
	}
}

// A busy per-learner reservation slot is transient: the note must stay pending
// and resume once the competing enrichment finishes (#175).
func TestNotesReviewConcurrencyLimitKeepsNoteResumable(t *testing.T) {
	database := reviewDatabase(t)
	learner, financial := reviewLearner(t, database)
	entered, finish := make(chan struct{}, 4), make(chan struct{})
	fixture := &reviewEngine{enrich: func(ctx context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
		entered <- struct{}{}
		select {
		case <-finish:
			return reviewResponse(req), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, fixture)
	oldest, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Oldest"})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Newer"})
	if err != nil {
		t.Fatal(err)
	}
	holder := make(chan error, 1)
	go func() { holder <- service.ProcessEnrichment(t.Context(), learner, newer.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("holder did not start")
	}
	if err = service.ProcessEnrichment(t.Context(), learner, oldest.ID); !errors.Is(err, credits.ErrConcurrency) {
		t.Fatalf("competing enrichment err=%v; want concurrency_limit", err)
	}
	refreshed, err := service.GetNote(t.Context(), learner, oldest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "enrichment_pending" {
		t.Fatalf("status=%s; want enrichment_pending", refreshed.Status)
	}
	resumed := make(chan error, 1)
	go func() { _, e := service.ResumePendingEnrichments(t.Context(), learner); resumed <- e }()
	time.Sleep(300 * time.Millisecond)
	close(finish)
	if err = <-holder; err != nil {
		t.Fatal(err)
	}
	if err = <-resumed; err != nil {
		t.Fatal(err)
	}
	refreshed, err = service.GetNote(t.Context(), learner, oldest.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "enriched" {
		t.Fatalf("status=%s; want enriched after the slot freed", refreshed.Status)
	}
}
