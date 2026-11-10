//go:build integration

package notes_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
)

type reviewEngine struct {
	enginev1.EngineServiceClient
	enrich func(context.Context, *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error)
}

func TestNotesReviewEnrichmentClaim(t *testing.T) {
	database := reviewDatabase(t)
	learner, financial := reviewLearner(t, database)
	entered, finish := make(chan struct{}, 10), make(chan struct{})
	var calls atomic.Int64
	fixture := &reviewEngine{enrich: func(ctx context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-finish:
			return reviewResponse(req), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}}
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, fixture)
	input := notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "A concurrent note"}
	note, err := service.SaveNote(t.Context(), learner, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.SaveNote(t.Context(), learner, input); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err = database.Pool.QueryRow(t.Context(), "SELECT count(*) FROM river_job WHERE kind='note_enrich' AND args->>'note_id'=$1", note.ID).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Errorf("idempotent save queued %d jobs; want 1", queued)
	}
	first := make(chan error, 1)
	go func() { first <- service.ProcessEnrichment(t.Context(), learner, note.ID) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("engine did not start")
	}
	queue, err := service.NotesQueue(slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	if err = queue.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
		defer cancel()
		if e := queue.Stop(ctx); e != nil {
			t.Error(e)
		}
	}()
	results := make(chan error, 3)
	go func() { results <- service.ProcessEnrichment(t.Context(), learner, note.ID) }()
	go func() {
		results <- service.ProcessEnrichmentWithVersion(t.Context(), learner, note.ID, note.RawVersion)
	}()
	go func() { _, e := service.ResumePendingEnrichments(t.Context(), learner); results <- e }()
	for range 3 {
		select {
		case e := <-results:
			if e != nil {
				t.Errorf("competing enrichment: %v", e)
			}
		case <-time.After(5 * time.Second):
			t.Error("competing entry point did not yield to active claim")
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err = database.Pool.QueryRow(t.Context(), "SELECT state FROM river_job WHERE kind='note_enrich' AND args->>'note_id'=$1", note.ID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker did not yield to existing enrichment claim: ", state)
		}
		time.Sleep(10 * time.Millisecond)
	}
	close(finish)
	if err = <-first; err != nil {
		t.Fatal(err)
	}
	var reservations int
	if err = database.Pool.QueryRow(t.Context(), "SELECT count(*) FROM credit_jobs WHERE learner_id=$1", learner).Scan(&reservations); err != nil {
		t.Fatal(err)
	}
	if reservations != 1 || calls.Load() != 1 {
		t.Errorf("reservations=%d engine calls=%d; want one each", reservations, calls.Load())
	}
}

func (e *reviewEngine) EnrichNote(ctx context.Context, req *enginev1.EnrichNoteRequest, _ ...grpc.CallOption) (*enginev1.EnrichNoteResponse, error) {
	return e.enrich(ctx, req)
}

func reviewDatabase(t *testing.T) *store.Store {
	t.Helper()
	ctx := t.Context()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("notes_review"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	database, err := store.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	if err = worker.Migrate(ctx, database.Pool, slog.Default()); err != nil {
		t.Fatal(err)
	}
	return database
}

func reviewLearner(t *testing.T, database *store.Store) (string, *credits.Service) {
	t.Helper()
	ctx := t.Context()
	learner := uuid.NewString()
	if _, err := database.Pool.Exec(ctx, "INSERT INTO learners(id,display_name) VALUES($1,'Review learner')", learner); err != nil {
		t.Fatal(err)
	}
	catalogue, err := pricing.Load(ctx, database.Queries)
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(database.Pool, nil, catalogue, nil)
	if _, err = financial.EnsureDailyGrant(ctx, learner, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err = database.Pool.Exec(ctx, "INSERT INTO credit_entries(learner_id,amount,kind,reference,pricing_version) VALUES($1,1000,'purchase',$2,$3)", learner, uuid.NewString(), catalogue.Config().Version); err != nil {
		t.Fatal(err)
	}
	return learner, financial
}

func reviewBalance(t *testing.T, database *store.Store, learner string) int64 {
	t.Helper()
	var amount int64
	if err := database.Pool.QueryRow(t.Context(), "SELECT coalesce(sum(amount),0) FROM credit_entries WHERE learner_id=$1", learner).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	return amount
}

func reviewResponse(req *enginev1.EnrichNoteRequest) *enginev1.EnrichNoteResponse {
	usage := &enginev1.Usage{Provider: req.Limits.Provider, Model: req.Limits.Model, Attempt: 1, Component: "content", Delivered: true, InputTokens: 100, OutputTokens: 50}
	return &enginev1.EnrichNoteResponse{Summary: "Grounded summary", Tags: []string{"tag"}, Usage: usage, Attempts: []*enginev1.Usage{usage}}
}

func TestNotesReviewBillingAtomicity(t *testing.T) {
	database := reviewDatabase(t)
	for _, mode := range []string{"engine-error", "bounds", "stale-edit", "persist-error", "success"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			learner, financial := reviewLearner(t, database)
			fixture := &reviewEngine{}
			service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, fixture)
			note, err := service.SaveNote(ctx, learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Original note"})
			if err != nil {
				t.Fatal(err)
			}
			fixture.enrich = func(ctx context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
				if mode == "engine-error" {
					return nil, errors.New("provider failed")
				}
				response := reviewResponse(req)
				if mode == "bounds" {
					response.Summary = strings.Repeat("x", 1001)
				}
				if mode == "stale-edit" {
					if _, err := service.UpdateNote(ctx, learner, note.ID, notes.UpdateNoteInput{RawText: "Edited while running", ExpectedVersion: note.Version}); err != nil {
						return nil, err
					}
				}
				return response, nil
			}
			if mode == "persist-error" {
				_, err = database.Pool.Exec(ctx, `CREATE FUNCTION fail_note_enrichment() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected write failure'; END $$; CREATE TRIGGER fail_note_enrichment BEFORE INSERT ON quick_note_enrichments FOR EACH ROW EXECUTE FUNCTION fail_note_enrichment()`)
				if err != nil {
					t.Fatal(err)
				}
				defer func() {
					if _, e := database.Pool.Exec(t.Context(), "DROP TRIGGER fail_note_enrichment ON quick_note_enrichments; DROP FUNCTION fail_note_enrichment()"); e != nil {
						t.Error(e)
					}
				}()
			}
			before := reviewBalance(t, database, learner)
			err = service.ProcessEnrichment(ctx, learner, note.ID)
			if mode == "success" && err != nil {
				t.Fatal(err)
			}
			if mode != "success" && err == nil {
				t.Error("non-delivery returned success")
			}
			if mode == "stale-edit" && !errors.Is(err, notes.ErrStaleVersion) {
				t.Errorf("got %v, want stale version", err)
			}
			delta := reviewBalance(t, database, learner) - before
			var spends, enrichments, active, cost int64
			if e := database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_entries WHERE learner_id=$1 AND kind='spend'", learner).Scan(&spends); e != nil {
				t.Fatal(e)
			}
			if e := database.Pool.QueryRow(ctx, "SELECT count(*) FROM quick_note_enrichments WHERE note_id=$1", note.ID).Scan(&enrichments); e != nil {
				t.Fatal(e)
			}
			if e := database.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_jobs WHERE learner_id=$1 AND state IN ('reserved','running')", learner).Scan(&active); e != nil {
				t.Fatal(e)
			}
			if e := database.Pool.QueryRow(ctx, "SELECT coalesce(sum(u.cost_nanos),0) FROM credit_usage u JOIN credit_jobs j ON j.id=u.job_id WHERE j.learner_id=$1", learner).Scan(&cost); e != nil {
				t.Fatal(e)
			}
			if mode == "success" {
				if delta >= 0 || spends != 1 || enrichments != 1 {
					t.Errorf("success delta=%d spends=%d enrichments=%d; want one charge and delivery", delta, spends, enrichments)
				}
				if err = service.ProcessEnrichment(ctx, learner, note.ID); err != nil {
					t.Fatal(err)
				}
				if repeat := reviewBalance(t, database, learner) - before; repeat != delta {
					t.Errorf("replay charged twice: %d vs %d", repeat, delta)
				}
			} else {
				if delta != 0 || spends != 0 || enrichments != 0 || cost != 0 {
					t.Errorf("non-delivery delta=%d spends=%d enrichments=%d cost=%d; want all zero", delta, spends, enrichments, cost)
				}
			}
			if active != 0 {
				t.Errorf("%d active reservations after completion", active)
			}
			refreshed, e := service.GetNote(ctx, learner, note.ID)
			if e != nil {
				t.Fatal(e)
			}
			if refreshed.Status == "enriching" {
				t.Error("note stuck enriching")
			}
		})
	}
}
