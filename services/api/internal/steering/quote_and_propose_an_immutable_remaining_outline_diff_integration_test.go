//go:build integration

package steering

import (
	"context"
	"encoding/json"
	"net"
	"sync/atomic"
	"testing"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	engineclient "github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
)

type replanRPC struct {
	pb.UnimplementedEngineServiceServer
	calls   atomic.Int32
	corrupt atomic.Bool
}

func (e *replanRPC) ReplanOutline(ctx context.Context, in *pb.ReplanOutlineRequest) (*pb.ReplanOutlineResponse, error) {
	e.calls.Add(1)
	if d, ok := ctx.Deadline(); !ok || time.Until(d) > 90*time.Second {
		return nil, context.DeadlineExceeded
	}
	out := proto.Clone(in.Outline).(*pb.CourseOutline)
	if e.corrupt.Load() {
		out.Units[0].Lessons[0].Title = "forbidden edit"
	} else {
		out.Units[0].Lessons[1].Title = "Go examples"
	}
	return &pb.ReplanOutlineResponse{Outline: out, Attempts: []*pb.Usage{{Provider: in.Limits.Provider, Model: in.Limits.Model, InputTokens: 100, OutputTokens: 100, Attempt: 1, Component: "content", Delivered: true}}}, nil
}

func TestReplanQuoteProposalFinancialImmutabilityIntegration(t *testing.T) {
	f := newFixture(t)
	qsvc := pricing.NewQuotes(f.db.Pool)
	rpc := &replanRPC{}
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
	cat, err := pricing.Load(f.ctx, db.New(f.db.Pool))
	if err != nil {
		t.Fatal(err)
	}
	financial := credits.NewServiceWithCatalogue(f.db.Pool, nil, cat, nil)
	s := New(Options{Pool: f.db.Pool, Engine: f.screen, Replanner: client, Quotes: qsvc, Financial: financial, Events: f.events})
	if _, err = s.QuoteReplan(f.ctx, f.owner, f.course); Code(err) != "replan_unavailable" {
		t.Fatalf("unapproved price must disable: %v", err)
	}
	// A successor is explicit and immutable; old active config is never edited.
	_, err = f.db.Pool.Exec(f.ctx, `INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT 'test-replan',now()-interval '1 second',true,jsonb_set(jsonb_set(jsonb_set(jsonb_set(config,'{version}','"test-replan"'),'{effective_from}',to_jsonb(now()-interval '1 second')),'{actions,outline_replan}','{"credits":55}'),'{limits,outline_replan}',(config->'limits'->'outline')||'{"timeout_ms":90000,"max_attempts":2,"max_cost_nanos":500000000}'::jsonb) FROM pricing_versions WHERE version='usd-quotes-2026-11-09'`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Pool.Exec(f.ctx, `INSERT INTO pricing_quote_policies(version,policy) SELECT 'test-replan',jsonb_set(policy,'{action_models,outline_replan}',policy->'action_models'->'outline') FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Pool.Exec(f.ctx, `INSERT INTO pricing_activation_events(version) VALUES('test-replan')`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Pool.Exec(f.ctx, `INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,'past',0,'Functions','Learn Go','done'),($1::uuid,$2::uuid,'future',1,'History','Learn history','locked')`, f.owner, f.unit)
	if err != nil {
		t.Fatal(err)
	}
	quote, err := s.QuoteReplan(f.ctx, f.owner, f.course)
	if err != nil {
		t.Fatal(err)
	}
	if quote.ContentCeiling != 55 || quote.DailyFreeEligible || quote.Bounds["timeout_ms"] != 90000 || quote.Bounds["max_attempts"] != 2 {
		t.Fatalf("terms: %+v", quote)
	}
	if _, err = s.ProposeReplan(f.ctx, f.owner, f.course, quote.ID); Code(err) != "quote_invalid" {
		t.Fatalf("unaccepted: %v", err)
	}
	if err = qsvc.Accept(f.ctx, f.owner, quote.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProposeReplan(f.ctx, f.owner, f.course, quote.ID); Code(err) != "insufficient_credits" {
		t.Fatalf("balance: %v", err)
	}
	if rpc.calls.Load() != 0 {
		t.Fatal("engine called before balance check")
	}
	_, err = f.db.Pool.Exec(f.ctx, `INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1::uuid,500,'purchase','replan-fixture','{"bucket":"purchased"}')`, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.ProposeReplan(f.ctx, f.owner, f.course, quote.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != "proposed" || p.OutlineVersion != 1 || len(p.Diff) != 1 || p.Diff[0].Kind != "retitled" || time.Until(p.ExpiresAt) < 23*time.Hour {
		t.Fatalf("proposal: %+v", p)
	}
	replay, err := s.ProposeReplan(f.ctx, f.owner, f.course, quote.ID)
	if err != nil || replay.ID != p.ID || rpc.calls.Load() != 1 {
		t.Fatalf("idempotency: %+v %v", replay, err)
	}
	var title, state string
	var spent int
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT title FROM course_lesson_stubs WHERE unit_id=$1::uuid AND authored_id='future'`, f.unit).Scan(&title); err != nil || title != "History" {
		t.Fatalf("changed live outline: %s %v", title, err)
	}
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT state,(result->>'content_spent')::int FROM credit_jobs WHERE quote_id=$1::uuid`, quote.ID).Scan(&state, &spent); err != nil || state != "settled" || spent != 55 {
		t.Fatalf("settlement: %s %d %v", state, spent, err)
	}
	rpc.corrupt.Store(true)
	bad, err := s.QuoteReplan(f.ctx, f.owner, f.course)
	if err != nil {
		t.Fatal(err)
	}
	if err = qsvc.Accept(f.ctx, f.owner, bad.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProposeReplan(f.ctx, f.owner, f.course, bad.ID); Code(err) != "protected_lesson_changed" {
		t.Fatalf("must reject: %v", err)
	}
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT state,(result->>'content_spent')::int FROM credit_jobs WHERE quote_id=$1::uuid`, bad.ID).Scan(&state, &spent); err != nil || state != "released" || spent != 0 {
		t.Fatalf("release: %s %d %v", state, spent, err)
	}
	raw, _ := json.Marshal(f.events.events)
	if !json.Valid(raw) {
		t.Fatal("events")
	}
	for _, name := range []string{"course_replan_quoted", "course_replan_proposed", "course_replan_failed", "$ai_generation"} {
		if f.events.count(name) == 0 {
			t.Fatalf("missing %s", name)
		}
	}
	// Retention is seven days after expiry, then learner erasure removes all rows.
	_, err = f.db.Pool.Exec(f.ctx, `UPDATE course_replan_proposals SET expires_at=now()-interval '8 days' WHERE id=$1::uuid`, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Purge(f.ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT count(*) FROM course_replan_proposals WHERE id=$1::uuid`, p.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("retention: %d %v", n, err)
	}
	_, err = f.db.Pool.Exec(f.ctx, `DELETE FROM learners WHERE id=$1::uuid`, f.owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT count(*) FROM course_replan_proposals WHERE learner_id=$1::uuid`, f.owner).Scan(&n); err != nil || n != 0 {
		t.Fatalf("erasure: %d %v", n, err)
	}
}
