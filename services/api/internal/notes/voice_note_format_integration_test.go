//go:build integration

package notes_test

import (
	"context"
	"errors"
	"log/slog"
	"net"
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
	"google.golang.org/grpc/credentials/insecure"
)

type formatEngine struct {
	enginev1.UnimplementedEngineServiceServer
	calls int
	last  *enginev1.EnrichNoteRequest
	fail  bool
}

func (f *formatEngine) EnrichNote(_ context.Context, req *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
	f.calls++
	f.last = req
	if f.fail {
		return nil, errors.New("engine failure")
	}
	u := &enginev1.Usage{Provider: req.Limits.Provider, Model: req.Limits.Model, Attempt: 1, InputTokens: 120, OutputTokens: 60, Delivered: true, Component: "content"}
	return &enginev1.EnrichNoteResponse{Title: "Unbuffered channels", FormattedBody: "- An unbuffered channel blocks the sender until a receiver is ready.", Summary: "Unbuffered channels block.", Tags: []string{"go"}, Usage: u, Attempts: []*enginev1.Usage{u}}, nil
}

func TestVoiceNoteFormattingFreeTurnsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("notes_voice_format"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	catalogue, err := pricing.Load(ctx, db.Queries)
	if err != nil {
		t.Fatal(err)
	}
	events := &testEventsDouble{}
	creditsSvc := credits.NewServiceWithCatalogue(db.Pool, nil, catalogue, events)
	engine := &formatEngine{}
	lis, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(server, engine)
	go func() { _ = server.Serve(lis) }()
	defer server.Stop()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	svc := notes.NewServiceWithEnrichment(db.Pool, events, creditsSvc, enginev1.NewEngineServiceClient(conn))

	learnerID := uuid.New().String()
	for _, q := range []string{`INSERT INTO learners(id, display_name) VALUES($1::uuid, 'Voice Learner')`, `INSERT INTO learner_profiles(learner_id,analytics_opt_out) VALUES($1::uuid,false)`} {
		if _, err = db.Pool.Exec(ctx, q, learnerID); err != nil {
			t.Fatal(err)
		}
	}
	var quoteID, creationID, courseID string
	if err = db.Pool.QueryRow(ctx, `INSERT INTO pricing_quotes(learner_id, owner_reference, pricing_version, snapshot, created_at, expires_at) VALUES($1::uuid, 'ref1', $2, '{}', now(), now()+interval '10 minutes') RETURNING id::text`, learnerID, catalogue.Config().Version).Scan(&quoteID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO course_creations(learner_id, draft_id, revision, quote_id, request, state) VALUES($1::uuid, gen_random_uuid(), 1, $2::uuid, '{}', 'enrolled') RETURNING id::text`, learnerID, quoteID).Scan(&creationID); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `INSERT INTO courses(learner_id, creation_id, title) VALUES($1::uuid, $2::uuid, 'Go for Beginners') RETURNING id::text`, learnerID, creationID).Scan(&courseID); err != nil {
		t.Fatal(err)
	}
	unitID, lessonID := uuid.New().String(), uuid.New().String()
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_units(id, learner_id, course_id, authored_id, position, title) VALUES($1::uuid, $2::uuid, $3::uuid, 'u1', 0, 'Unit 1')`, unitID, learnerID, courseID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_lesson_stubs(id, learner_id, unit_id, authored_id, position, title, goal, status) VALUES($1::uuid, $2::uuid, $3::uuid, 'l1', 0, 'Channels', 'Use channels', 'ready_to_generate')`, lessonID, learnerID, unitID); err != nil {
		t.Fatal(err)
	}
	if _, err = creditsSvc.EnsureDailyGrant(ctx, learnerID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	jobs := func() (n int) {
		_ = db.Pool.QueryRow(ctx, `SELECT count(*) FROM credit_jobs WHERE learner_id=$1::uuid`, learnerID).Scan(&n)
		return n
	}
	save := func(key string) *notes.NoteDTO {
		t.Helper()
		n, e := svc.SaveNote(ctx, learnerID, notes.SaveNoteInput{ActionKey: key, CourseID: &courseID, LessonID: &lessonID, SlideID: ptr(1), SlideTitle: "Unbuffered channels", Source: "slide", RawText: "an unbuffered chanel blocks the sender", Anchor: &notes.AnchorInput{PlaybackMs: 1200, Input: "voice"}})
		if e != nil {
			t.Fatal(e)
		}
		return n
	}

	// Five free turns: formatted with course context, no credits touched.
	for i := 1; i <= notes.FreeFormatTurnsPerDay; i++ {
		n := save("voice-free-" + string(rune('0'+i)))
		if n.Status != "enrichment_pending" || n.FormatOffer || n.FreeFormatsLeft == nil || *n.FreeFormatsLeft != notes.FreeFormatTurnsPerDay-i {
			t.Fatalf("turn %d: status %s offer %v left %v", i, n.Status, n.FormatOffer, n.FreeFormatsLeft)
		}
		if err = svc.ProcessEnrichment(ctx, learnerID, n.ID); err != nil {
			t.Fatal(err)
		}
		got, e := svc.GetNote(ctx, learnerID, n.ID)
		if e != nil || got.Status != "enriched" || got.Title == "" || got.FormattedBody == "" || got.RawText != "an unbuffered chanel blocks the sender" {
			t.Fatalf("turn %d: %+v %v", i, got, e)
		}
	}
	if r := engine.last; r.CourseTitle != "Go for Beginners" || r.LessonTitle != "Channels" || r.SlideTitle != "Unbuffered channels" || !r.Dictated {
		t.Fatalf("engine context: %+v", r)
	}
	if jobs() != 0 {
		t.Fatalf("free formatting created %d credit jobs", jobs())
	}

	// A replayed save never takes another turn.
	if again := save("voice-free-1"); again.Status != "enriched" {
		t.Fatalf("replay status %s", again.Status)
	}
	if turns, _ := svc.FormatTurns(ctx, learnerID); turns.Left != 0 || turns.Used != notes.FreeFormatTurnsPerDay {
		t.Fatalf("turns %+v", turns)
	}

	// Past the free turns: saved raw, offered Format, nothing formatted or charged on its own.
	calls := engine.calls
	raw := save("voice-raw-6")
	if raw.Status != "raw_only" || !raw.FormatOffer || *raw.FreeFormatsLeft != 0 {
		t.Fatalf("raw note: %+v", raw)
	}
	if err = svc.ProcessEnrichment(ctx, learnerID, raw.ID); err != nil || engine.calls != calls || jobs() != 0 {
		t.Fatalf("raw note was processed: err %v calls %d jobs %d", err, engine.calls-calls, jobs())
	}

	// Explicit Format at the quick_note price: charged once, and not repeatable.
	if _, err = svc.FormatNote(ctx, learnerID, raw.ID); err != nil {
		t.Fatal(err)
	}
	if err = svc.ProcessEnrichment(ctx, learnerID, raw.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.GetNote(ctx, learnerID, raw.ID); got.FormattedBody == "" || jobs() != 1 {
		t.Fatalf("paid format: %+v jobs %d", got, jobs())
	}
	if _, err = svc.FormatNote(ctx, learnerID, raw.ID); !errors.Is(err, notes.ErrFormatNotAvailable) {
		t.Fatalf("second Format: %v", err)
	}

	// Engine failure: the note stays raw and a free turn comes back.
	if _, err = db.Pool.Exec(ctx, `DELETE FROM quick_note_format_turns WHERE learner_id=$1::uuid`, learnerID); err != nil {
		t.Fatal(err)
	}
	engine.fail = true
	failing := save("voice-fail-1")
	perr := svc.ProcessEnrichment(ctx, learnerID, failing.ID)
	_ = perr
	got, _ := svc.GetNote(ctx, learnerID, failing.ID)
	turns, _ := svc.FormatTurns(ctx, learnerID)
	if got.Status != "raw_only" || got.FormattedBody != "" || got.RawText == "" || turns.Left != notes.FreeFormatTurnsPerDay || !got.FormatOffer {
		t.Fatalf("failed free format: %+v turns %+v", got, turns)
	}
}
