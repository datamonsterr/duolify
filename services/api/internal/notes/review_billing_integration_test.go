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
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/datamonsterr/duolify/services/api/internal/worker"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
)

type reviewEngine struct {
	enginev1.EngineServiceClient
	enrich func(context.Context, *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error)
}

func TestNotesReviewSQLRequiresCurrentVersion(t *testing.T) {
	database := reviewDatabase(t)
	learner, _ := reviewLearner(t, database)
	service := notes.NewService(database.Pool, nil)
	note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = database.Pool.Exec(t.Context(), "UPDATE quick_notes SET version=5,raw_version=1 WHERE id=$1", note.ID); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int32{0, 1} {
		_, err = database.Queries.UpdateQuickNoteRawText(t.Context(), db.UpdateQuickNoteRawTextParams{ID: pgtype.UUID{Bytes: uuid.MustParse(note.ID), Valid: true}, LearnerID: pgtype.UUID{Bytes: uuid.MustParse(learner), Valid: true}, RawText: "Blind overwrite", Version: version})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("version %d update got %v; want no row", version, err)
		}
	}
	refreshed, err := service.GetNote(t.Context(), learner, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Version != 5 || refreshed.RawText != "Original" {
		t.Error("invalid version changed note")
	}
}

func TestNotesReviewWorkerQueryOwnership(t *testing.T) {
	database := reviewDatabase(t)
	owner, financial := reviewLearner(t, database)
	foreign, _ := reviewLearner(t, database)
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, nil)
	note, err := service.SaveNote(t.Context(), owner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Owner only"})
	if err != nil {
		t.Fatal(err)
	}
	id := pgtype.UUID{Bytes: uuid.MustParse(note.ID), Valid: true}
	foreignID := pgtype.UUID{Bytes: uuid.MustParse(foreign), Valid: true}
	before := reviewBalance(t, database, owner)
	if err = service.ProcessEnrichmentWithVersion(t.Context(), foreign, note.ID, note.RawVersion); !errors.Is(err, notes.ErrNotFound) {
		t.Errorf("foreign worker got %v; want not found", err)
	}
	if _, err = database.Queries.GetQuickNoteForUpdate(t.Context(), db.GetQuickNoteForUpdateParams{ID: id, LearnerID: foreignID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("foreign locking read got %v", err)
	}
	if _, err = database.Queries.UpdateQuickNoteStatus(t.Context(), db.UpdateQuickNoteStatusParams{ID: id, LearnerID: foreignID, Status: "enriched"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("foreign status update got %v", err)
	}
	refreshed, err := service.GetNote(t.Context(), owner, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != "enrichment_pending" {
		t.Error("foreign worker changed status")
	}
	if delta := reviewBalance(t, database, owner) - before; delta != 0 {
		t.Errorf("foreign worker ledger delta=%d want 0", delta)
	}
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

func reviewCourse(t *testing.T, database *store.Store, learner string) (string, string) {
	t.Helper()
	var course, lesson string
	err := database.Pool.QueryRow(t.Context(), `WITH q AS (INSERT INTO pricing_quotes(learner_id,owner_reference,pricing_version,snapshot,created_at,expires_at) SELECT $1,'fixture',version,'{}',now(),now()+interval '1 hour' FROM pricing_versions ORDER BY effective_from DESC LIMIT 1 RETURNING id), creation AS (INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) SELECT $1,gen_random_uuid(),1,id,'{}','enrolled' FROM q RETURNING id) INSERT INTO courses(learner_id,creation_id,title) SELECT $1,id,'Private course' FROM creation RETURNING id::text`, learner).Scan(&course)
	if err != nil {
		t.Fatal(err)
	}
	err = database.Pool.QueryRow(t.Context(), `WITH unit AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) VALUES($1,$2,'unit',0,'Private unit') RETURNING id) INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1,id,'lesson',0,'Private lesson title','Goal','ready_to_generate' FROM unit RETURNING id::text`, learner, course).Scan(&lesson)
	if err != nil {
		t.Fatal(err)
	}
	return course, lesson
}

func TestNotesReviewCourseOwnership(t *testing.T) {
	database := reviewDatabase(t)
	learner, financial := reviewLearner(t, database)
	foreign, _ := reviewLearner(t, database)
	ownCourse, ownLesson := reviewCourse(t, database, learner)
	foreignCourse, foreignLesson := reviewCourse(t, database, foreign)
	var calls atomic.Int64
	fixture := &reviewEngine{enrich: func(_ context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
		calls.Add(1)
		for _, l := range req.AllowedLessons {
			if l.LessonId == foreignLesson {
				t.Error("foreign lesson title reached engine")
			}
		}
		response := reviewResponse(req)
		response.Links = []string{foreignLesson, ownLesson}
		return response, nil
	}}
	service := notes.NewServiceWithEnrichment(database.Pool, nil, financial, fixture)
	before := reviewBalance(t, database, learner)
	for _, in := range []notes.SaveNoteInput{
		{ActionKey: uuid.NewString(), Source: "chat", RawText: "Foreign course", CourseID: &foreignCourse},
		{ActionKey: uuid.NewString(), Source: "lesson", RawText: "Mismatched course", CourseID: &foreignCourse, LessonID: &ownLesson},
	} {
		if _, err := service.SaveNote(t.Context(), learner, in); !errors.Is(err, notes.ErrUnauthorizedSource) {
			t.Errorf("foreign course accepted: %v", err)
		}
	}
	note, err := service.SaveNote(t.Context(), learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "lesson", RawText: "Authorized lesson", LessonID: &ownLesson})
	if err != nil {
		t.Fatal(err)
	}
	if note.CourseID == nil || *note.CourseID != ownCourse {
		t.Error("course not derived from authorized lesson")
	}
	// Old persisted bad references must also fail closed in the worker.
	if _, err = database.Pool.Exec(t.Context(), "UPDATE quick_notes SET course_id=$2 WHERE id=$1", note.ID, foreignCourse); err != nil {
		t.Fatal(err)
	}
	if err = service.ProcessEnrichment(t.Context(), learner, note.ID); !errors.Is(err, notes.ErrUnauthorizedSource) {
		t.Errorf("worker accepted foreign course: %v", err)
	}
	if calls.Load() != 0 {
		t.Error("foreign context invoked engine")
	}
	if delta := reviewBalance(t, database, learner) - before; delta != 0 {
		t.Errorf("foreign course ledger delta=%d want 0", delta)
	}
	if _, err = database.Pool.Exec(t.Context(), "UPDATE quick_notes SET course_id=$2 WHERE id=$1", note.ID, ownCourse); err != nil {
		t.Fatal(err)
	}
	if err = service.ProcessEnrichment(t.Context(), learner, note.ID); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.GetNote(t.Context(), learner, note.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range refreshed.Links {
		if link == foreignLesson {
			t.Error("foreign lesson link persisted")
		}
	}
}

func TestNotesReviewBillingAtomicity(t *testing.T) {
	database := reviewDatabase(t)
	for _, mode := range []string{"engine-error", "bounds", "stale-edit", "persist-error", "missing-usage", "cancelled-request", "success"} {
		t.Run(mode, func(t *testing.T) {
			ctx := t.Context()
			learner, financial := reviewLearner(t, database)
			fixture := &reviewEngine{}
			events := &testEventsDouble{}
			service := notes.NewServiceWithEnrichment(database.Pool, events, financial, fixture)
			note, err := service.SaveNote(ctx, learner, notes.SaveNoteInput{ActionKey: uuid.NewString(), Source: "chat", RawText: "Original note"})
			if err != nil {
				t.Fatal(err)
			}
			processCtx, cancelProcess := context.WithCancel(ctx)
			defer cancelProcess()
			fixture.enrich = func(ctx context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
				if mode == "engine-error" {
					return nil, errors.New("provider failed")
				}
				response := reviewResponse(req)
				if mode == "cancelled-request" {
					cancelProcess()
					return nil, context.Canceled
				}
				if mode == "missing-usage" {
					response.Usage = nil
					response.Attempts = nil
				}
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
			err = service.ProcessEnrichment(processCtx, learner, note.ID)
			if mode == "success" && err != nil {
				t.Fatal(err)
			}
			if mode != "success" && err == nil {
				t.Error("non-delivery returned success")
			}
			if mode != "success" {
				foundFailure := false
				events.mu.Lock()
				for _, event := range events.events {
					if event.Event == "quick_note_enrichment_failed" {
						foundFailure = true
					}
				}
				events.mu.Unlock()
				if !foundFailure {
					t.Error("enrichment failure analytics missing")
				}
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
