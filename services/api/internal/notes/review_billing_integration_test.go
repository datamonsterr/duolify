//go:build integration

package notes_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
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
