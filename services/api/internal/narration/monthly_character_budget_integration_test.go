//go:build integration

package narration_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"strings"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/backuptest"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/datamonsterr/duolify/services/api/internal/narration/narrationtest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// budgetRig is one Learner with narration on, a mock engine twin and a narration service
// whose monthly ElevenLabs character budget is set by the test.
type budgetRig struct {
	f      *backuptest.Fixture
	engine *narrationtest.Engine
	events *recorder
	logs   *bytes.Buffer
	build  func(narration.Budget) *narration.Service
}

func newBudgetRig(t *testing.T) *budgetRig {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)
	f := backuptest.New(t)
	f.Seed(t)
	pool := f.DB.Pool
	if _, err := pool.Exec(ctx, `UPDATE learner_profiles SET narration=true,narration_voice='default' WHERE learner_id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	mock := &narrationtest.Engine{}
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(server, mock)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	catalogue, err := pricing.Load(ctx, f.DB.Queries)
	if err != nil {
		t.Fatal(err)
	}
	events := &recorder{}
	financial := credits.NewServiceWithCatalogue(pool, nil, catalogue, events)
	if _, err = pool.Exec(ctx, `INSERT INTO credit_entries (learner_id, amount, kind, reference, pricing_version, provenance) VALUES ($1, 500, 'purchase', 'budget-topup:' || gen_random_uuid(), '2026.04', '{"bucket": "purchase"}'::jsonb)`, owner); err != nil {
		t.Fatal(err)
	}
	// The shared fixture leaves one open reservation; release it so the free-tier concurrency bound
	// only reflects this test's narration jobs.
	for {
		var id string
		if err = pool.QueryRow(ctx, `SELECT id::text FROM credit_jobs WHERE learner_id=$1 AND state='reserved' LIMIT 1`, owner).Scan(&id); err != nil {
			break
		}
		if _, err = financial.Release(ctx, owner, id); err != nil {
			t.Fatal(err)
		}
	}
	logs := &bytes.Buffer{}
	r := &budgetRig{f: f, engine: mock, events: events, logs: logs}
	r.build = func(b narration.Budget) *narration.Service {
		svc := narration.New(narration.Options{Pool: pool, Objects: f.ApplicationObjects(), Speech: enginev1.NewEngineServiceClient(conn), Financial: financial, Events: events, JobAttempts: 1, Budget: b, Logger: slog.New(slog.NewTextHandler(logs, nil))})
		if _, err := svc.Queue(); err != nil {
			t.Fatal(err)
		}
		return svc
	}
	return r
}

func (r *budgetRig) statuses(t *testing.T) map[string][2]string {
	t.Helper()
	rows, err := r.f.DB.Pool.Query(t.Context(), `SELECT slide_id,status,failure_code FROM slide_narrations WHERE lesson_id=$1 AND slide_id<>'seed-ready'`, lesson)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string][2]string{}
	for rows.Next() {
		var id, st, code string
		if err = rows.Scan(&id, &st, &code); err != nil {
			t.Fatal(err)
		}
		out[id] = [2]string{st, code}
	}
	return out
}

func TestExhaustedMonthlyBudgetMakesNoLiveCallAndFallsBackToCaptions(t *testing.T) {
	r := newBudgetRig(t)
	ctx := t.Context()
	// 20 characters allow 19 at 95%: no slide of the fixture lesson fits.
	svc := r.build(narration.Budget{Limit: 20, StopPercent: 95, CycleDay: 1})
	if queued, err := svc.Enqueue(ctx, owner, lesson); err != nil || queued == 0 {
		t.Fatalf("enqueue: %d %v", queued, err)
	}
	if err := svc.SynthesizeLesson(ctx, owner, lesson, true); err != nil {
		t.Fatal(err)
	}
	if n := r.engine.Calls(); n != 0 {
		t.Fatalf("exhausted budget reached the provider %d times", n)
	}
	for slide, st := range r.statuses(t) {
		if st != [2]string{narration.StatusDisabled, "narration_budget_exhausted"} {
			t.Fatalf("%s: %v", slide, st)
		}
	}
	got := r.events.named("narration_budget_exhausted")
	if len(got) != 1 || got[0]["characters_limit"] != int64(20) {
		t.Fatalf("event: %v", got)
	}
	if !strings.Contains(r.logs.String(), "level=ERROR") || !strings.Contains(r.logs.String(), "narration monthly character budget exhausted") {
		t.Fatalf("alert-worthy log line missing: %s", r.logs.String())
	}
	var reservations int
	if err := r.f.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM pricing_quotes q WHERE q.snapshot->>'action'='narration'`).Scan(&reservations); err != nil || reservations != 0 {
		t.Fatalf("no quote may exist for refused narration: %d %v", reservations, err)
	}
}

func TestMonthlyBudgetCountsGeneratedAssetsOnceAndNeverReusedAudio(t *testing.T) {
	r := newBudgetRig(t)
	ctx := t.Context()
	svc := r.build(narration.Budget{Limit: 40000, StopPercent: 95, CycleDay: 1})
	now := time.Now()
	start := narration.Budget{CycleDay: 1}.CycleStart(now)
	used := func() int64 {
		n, err := svc.CharactersUsed(ctx, start)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	base := used()
	if _, err := r.f.DB.Pool.Exec(ctx, `INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms,created_at)
		VALUES (repeat('a',64),repeat('b',64),'v','m','f','lessons/narration/a','lessons/narration/b',500,1,1,now()),
		       (repeat('c',64),repeat('d',64),'v','m','f','lessons/narration/c','lessons/narration/d',700,1,1,$1::timestamptz - interval '1 day')`, start); err != nil {
		t.Fatal(err)
	}
	if got := used(); got != base+500 {
		t.Fatalf("only this cycle's asset counts: %d, base %d", got, base)
	}
	// A learner reusing the asset adds a reference, not characters.
	if _, err := r.f.DB.Pool.Exec(ctx, `INSERT INTO narration_regenerations(content_hash,status,characters,completed_at) VALUES (repeat('a',64),'completed',120,now())`); err != nil {
		t.Fatal(err)
	}
	if got := used(); got != base+500+120 {
		t.Fatalf("a completed regeneration is provider usage too: %d", got)
	}
}

func TestMonthlyBudgetStillGeneratesWhileUnderTheStopLine(t *testing.T) {
	r := newBudgetRig(t)
	ctx := t.Context()
	svc := r.build(narration.Budget{Limit: 40000, StopPercent: 95, CycleDay: 1})
	if queued, err := svc.Enqueue(ctx, owner, lesson); err != nil || queued == 0 {
		t.Fatalf("enqueue: %d %v", queued, err)
	}
	if err := svc.SynthesizeLesson(ctx, owner, lesson, true); err != nil {
		t.Fatal(err)
	}
	if r.engine.Calls() == 0 {
		t.Fatalf("an unexhausted budget must reach the provider: %v logs=%s", r.statuses(t), r.logs.String())
	}
	if got := r.events.named("narration_budget_exhausted"); len(got) != 0 {
		t.Fatalf("unexpected exhaustion event: %v", got)
	}
	n, err := svc.CharactersUsed(ctx, narration.Budget{CycleDay: 1}.CycleStart(time.Now()))
	if err != nil || n == 0 {
		t.Fatalf("generated audio must be counted: %d %v", n, err)
	}
}
