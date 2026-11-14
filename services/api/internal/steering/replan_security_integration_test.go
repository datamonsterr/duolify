//go:build integration

package steering

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	engineclient "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// flakyRPC is a replan engine double that can fail before delivering anything.
type flakyRPC struct {
	pb.UnimplementedEngineServiceServer
	calls atomic.Int32
	fail  atomic.Bool
}

func (e *flakyRPC) ReplanOutline(_ context.Context, in *pb.ReplanOutlineRequest) (*pb.ReplanOutlineResponse, error) {
	e.calls.Add(1)
	time.Sleep(150 * time.Millisecond)
	if e.fail.Load() {
		return nil, status.Error(codes.Unavailable, "provider down")
	}
	out := proto.Clone(in.Outline).(*pb.CourseOutline)
	out.Units[0].Lessons[1].Title = "Go examples"
	return &pb.ReplanOutlineResponse{Outline: out, Attempts: []*pb.Usage{{Provider: in.Limits.Provider, Model: in.Limits.Model, InputTokens: 10, OutputTokens: 10, Attempt: 1, Component: "content", Delivered: true}}}, nil
}

type replanHarness struct {
	*fixture
	s    *Service
	rpc  *flakyRPC
	qsvc *pricing.Quotes
}

// newReplanHarness enables an approved outline_replan price and funds nothing.
func newReplanHarness(t *testing.T) *replanHarness {
	t.Helper()
	f := newFixture(t)
	rpc := &flakyRPC{}
	listener, err := new(net.ListenConfig).Listen(f.ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	pb.RegisterEngineServiceServer(server, rpc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := engineclient.New("http://"+listener.Addr().String(), 90*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := f.db.Pool.Exec(f.ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT 'test-replan',now()-interval '1 second',true,jsonb_set(jsonb_set(jsonb_set(jsonb_set(config,'{version}','"test-replan"'),'{effective_from}',to_jsonb(now()-interval '1 second')),'{actions,outline_replan}','{"credits":55}'),'{limits,outline_replan}',(config->'limits'->'outline')||'{"timeout_ms":90000,"max_attempts":2,"max_cost_nanos":500000000}'::jsonb) FROM pricing_versions WHERE version='usd-quotes-2026-11-09'`)
	exec(`INSERT INTO pricing_quote_policies(version,policy) SELECT 'test-replan',jsonb_set(policy,'{action_models,outline_replan}',policy->'action_models'->'outline') FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'`)
	exec(`INSERT INTO pricing_activation_events(version) VALUES('test-replan')`)
	exec(`INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,'past',0,'Functions','Learn Go','done'),($1::uuid,$2::uuid,'future',1,'History','Learn history','locked')`, f.owner, f.unit)
	cat, err := pricing.Load(f.ctx, db.New(f.db.Pool))
	if err != nil {
		t.Fatal(err)
	}
	qsvc := pricing.NewQuotes(f.db.Pool)
	financial := credits.NewServiceWithCatalogue(f.db.Pool, nil, cat, nil)
	s := New(Options{Pool: f.db.Pool, Engine: f.screen, Replanner: client, Quotes: qsvc, Financial: financial, Events: f.events})
	return &replanHarness{fixture: f, s: s, rpc: rpc, qsvc: qsvc}
}

func (h *replanHarness) fund(t *testing.T) {
	t.Helper()
	if _, err := h.db.Pool.Exec(h.ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1::uuid,500,'purchase','replan-fixture','{"bucket":"purchased"}')`, h.owner); err != nil {
		t.Fatal(err)
	}
}

func (h *replanHarness) accepted(t *testing.T, owner string) string {
	t.Helper()
	q, err := h.s.QuoteReplan(h.ctx, owner, h.course)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.qsvc.Accept(h.ctx, owner, q.ID); err != nil {
		t.Fatal(err)
	}
	return q.ID
}

func (h *replanHarness) jobState(t *testing.T, quote string) (state string, n int) {
	t.Helper()
	rows, err := h.db.Pool.Query(h.ctx, `SELECT state FROM credit_jobs WHERE quote_id=$1::uuid`, quote)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		n++
		if err = rows.Scan(&state); err != nil {
			t.Fatal(err)
		}
	}
	return state, n
}

func TestReplanIsOwnerOnlyAndHonoursCopyForkRules(t *testing.T) {
	h := newReplanHarness(t)
	h.fund(t)
	stranger := "b3333333-3333-4333-8333-333333333333"
	if _, err := h.db.Pool.Exec(h.ctx, `INSERT INTO learners(id,display_name) VALUES($1::uuid,'Stranger')`, stranger); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.QuoteReplan(h.ctx, stranger, h.course); Code(err) != "not_found" {
		t.Fatalf("stranger quote must not see the course: %v", err)
	}
	quote := h.accepted(t, h.owner)
	if _, err := h.s.ProposeReplan(h.ctx, stranger, h.course, quote); Code(err) != "not_found" {
		t.Fatalf("stranger propose: %v", err)
	}
	if _, err := h.s.ProposeReplan(h.ctx, h.owner, "not-a-uuid", quote); Code(err) != "not_found" {
		t.Fatalf("malformed course: %v", err)
	}
	if h.rpc.calls.Load() != 0 {
		t.Fatal("engine called for an unauthorized request")
	}
	// An unforked published copy is pinned to its source: fork first (E16-05), then replan.
	if _, err := h.db.Pool.Exec(h.ctx, `INSERT INTO course_copies(copy_course_id,learner_id,version,content_hash) VALUES($1::uuid,$2::uuid,1,$3)`, h.course, h.owner, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.QuoteReplan(h.ctx, h.owner, h.course); Code(err) != "fork_confirmation_required" {
		t.Fatalf("unforked copy: %v", err)
	}
	if _, err := h.s.ProposeReplan(h.ctx, h.owner, h.course, quote); Code(err) != "fork_confirmation_required" {
		t.Fatalf("unforked copy propose: %v", err)
	}
	if _, err := h.db.Pool.Exec(h.ctx, `UPDATE course_copies SET forked_at=now() WHERE copy_course_id=$1::uuid`, h.course); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.QuoteReplan(h.ctx, h.owner, h.course); err != nil {
		t.Fatalf("forked copy is the learner's own course: %v", err)
	}
	if h.rpc.calls.Load() != 0 {
		t.Fatal("engine called while only quoting")
	}
}

func TestReplanStaleQuoteIsRefusedWithoutReserveOrEngine(t *testing.T) {
	h := newReplanHarness(t)
	h.fund(t)
	quote := h.accepted(t, h.owner)
	if _, err := h.db.Pool.Exec(h.ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,'late',2,'Added later','Goal','locked')`, h.owner, h.unit); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.ProposeReplan(h.ctx, h.owner, h.course, quote); Code(err) != "quote_invalid" {
		t.Fatalf("stale outline: %v", err)
	}
	if _, n := h.jobState(t, quote); n != 0 || h.rpc.calls.Load() != 0 {
		t.Fatalf("stale quote reserved or called the engine: jobs=%d calls=%d", n, h.rpc.calls.Load())
	}
}

func TestReplanFailureReleasesAndRetriesNeverRecharge(t *testing.T) {
	h := newReplanHarness(t)
	h.fund(t)
	h.rpc.fail.Store(true)
	quote := h.accepted(t, h.owner)
	if _, err := h.s.ProposeReplan(h.ctx, h.owner, h.course, quote); Code(err) != "generation_failed" {
		t.Fatalf("failure: %v", err)
	}
	if state, n := h.jobState(t, quote); n != 1 || state != "released" {
		t.Fatalf("undelivered work must release: %s %d", state, n)
	}
	var proposals int
	if err := h.db.Pool.QueryRow(h.ctx, `SELECT count(*) FROM course_replan_proposals WHERE state='proposed'`).Scan(&proposals); err != nil || proposals != 0 {
		t.Fatalf("no proposal on failure: %d %v", proposals, err)
	}
	if _, err := h.s.ProposeReplan(h.ctx, h.owner, h.course, quote); Code(err) != "generation_failed" || h.rpc.calls.Load() != 1 {
		t.Fatalf("a failed quote is terminal; retry needs a new quote: %v calls=%d", err, h.rpc.calls.Load())
	}
}

func TestReplanConcurrentRequestsOnOneQuoteRunTheEngineOnce(t *testing.T) {
	h := newReplanHarness(t)
	h.fund(t)
	quote := h.accepted(t, h.owner)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = h.s.ProposeReplan(h.ctx, h.owner, h.course, quote)
		}()
	}
	wg.Wait()
	if h.rpc.calls.Load() != 1 {
		t.Fatalf("engine calls: %d", h.rpc.calls.Load())
	}
	if state, n := h.jobState(t, quote); n != 1 || state != "settled" {
		t.Fatalf("one settled reservation expected: %s %d", state, n)
	}
	final, err := h.s.ProposeReplan(h.ctx, h.owner, h.course, quote)
	if err != nil || final.State != "proposed" || len(final.Diff) != 1 {
		t.Fatalf("final: %+v %v", final, err)
	}
}
