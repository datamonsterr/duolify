//go:build integration

package steering

import (
	"context"
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

type confirmReplanRPC struct {
	pb.UnimplementedEngineServiceServer
	calls atomic.Int32
}

func (e *confirmReplanRPC) ReplanOutline(_ context.Context, in *pb.ReplanOutlineRequest) (*pb.ReplanOutlineResponse, error) {
	e.calls.Add(1)
	out := proto.Clone(in.Outline).(*pb.CourseOutline)
	// Retitle lesson 1 (future lesson) and add lesson 2
	out.Units[0].Lessons[1].Title = "Go examples"
	out.Units[0].Lessons = append(out.Units[0].Lessons, &pb.LessonRef{
		LessonId: "l-added-3",
		Title:    "Concurrency in Go",
		Goal:     "Learn goroutines",
	})
	return &pb.ReplanOutlineResponse{
		Outline: out,
		Attempts: []*pb.Usage{{
			Provider:  in.Limits.Provider,
			Model:     in.Limits.Model,
			Attempt:   1,
			Component: "content",
			Delivered: true,
		}},
	}, nil
}

func setupConfirmHarness(t *testing.T) (*fixture, *Service, ReplanProposal) {
	t.Helper()
	f := newFixture(t)
	rpc := &confirmReplanRPC{}
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
	exec(`INSERT INTO pricing_versions(version,effective_from,approved,config) SELECT 'test-replan-c',now()-interval '1 second',true,jsonb_set(jsonb_set(jsonb_set(jsonb_set(config,'{version}','"test-replan-c"'),'{effective_from}',to_jsonb(now()-interval '1 second')),'{actions,outline_replan}','{"credits":55}'),'{limits,outline_replan}',(config->'limits'->'outline')||'{"timeout_ms":90000,"max_attempts":2,"max_cost_nanos":500000000}'::jsonb) FROM pricing_versions WHERE version='usd-quotes-2026-11-09'`)
	exec(`INSERT INTO pricing_quote_policies(version,policy) SELECT 'test-replan-c',jsonb_set(policy,'{action_models,outline_replan}',policy->'action_models'->'outline') FROM pricing_quote_policies WHERE version='usd-quotes-2026-11-09'`)
	exec(`INSERT INTO pricing_activation_events(version) VALUES('test-replan-c')`)
	exec(`INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) VALUES($1::uuid,$2::uuid,'past',0,'Functions','Learn Go','done'),($1::uuid,$2::uuid,'future',1,'History','Learn history','locked')`, f.owner, f.unit)
	exec(`INSERT INTO credit_entries(learner_id,amount,kind,reference,provenance) VALUES($1::uuid,500,'purchase','replan-fixture','{"bucket":"purchased"}')`, f.owner)

	cat, err := pricing.Load(f.ctx, db.New(f.db.Pool))
	if err != nil {
		t.Fatal(err)
	}
	qsvc := pricing.NewQuotes(f.db.Pool)
	financial := credits.NewServiceWithCatalogue(f.db.Pool, nil, cat, nil)
	s := New(Options{Pool: f.db.Pool, Engine: f.screen, Replanner: client, Quotes: qsvc, Financial: financial, Events: f.events})

	quote, err := s.QuoteReplan(f.ctx, f.owner, f.course)
	if err != nil {
		t.Fatal(err)
	}
	if err = qsvc.Accept(f.ctx, f.owner, quote.ID); err != nil {
		t.Fatal(err)
	}
	p, err := s.ProposeReplan(f.ctx, f.owner, f.course, quote.ID)
	if err != nil {
		t.Fatal(err)
	}
	return f, s, p
}

func TestConfirmProposalAtomicallyAppliesAndIncrementsVersionIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)

	// Confirm proposal
	confirmed, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID)
	if err != nil {
		t.Fatalf("confirm failed: %v", err)
	}
	if confirmed.Decision != "confirmed" || confirmed.OutlineVersion != 1 {
		t.Fatalf("unexpected confirmed proposal: %+v", confirmed)
	}

	// Outline version in courses must be incremented to 2
	var version int64
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT outline_version FROM courses WHERE id=$1::uuid`, f.course).Scan(&version); err != nil || version != 2 {
		t.Fatalf("expected version 2, got %d: %v", version, err)
	}

	// Protected lesson 1 must stay intact ('Functions', 'done')
	var title0, status0 string
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT title, status FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=0`, f.unit).Scan(&title0, &status0); err != nil || title0 != "Functions" || status0 != "done" {
		t.Fatalf("protected lesson modified: %s, %s, %v", title0, status0, err)
	}

	// Lesson 2 must be updated to 'Go examples'
	var title1 string
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT title FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=1`, f.unit).Scan(&title1); err != nil || title1 != "Go examples" {
		t.Fatalf("retitled lesson not updated: %s, %v", title1, err)
	}

	// New lesson 3 must be inserted
	var title2, goal2, status2 string
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT title, goal, status FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=2`, f.unit).Scan(&title2, &goal2, &status2); err != nil || title2 != "Concurrency in Go" || status2 != "locked" {
		t.Fatalf("added lesson missing or incorrect: %s, %s, %s, %v", title2, goal2, status2, err)
	}

	// Analytics event emitted
	if f.events.count("course_replan_confirmed") != 1 {
		t.Fatalf("expected course_replan_confirmed event, got %d", f.events.count("course_replan_confirmed"))
	}

	// Idempotent retry of confirm returns success with no extra version increment or side effects
	retry, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID)
	if err != nil || retry.Decision != "confirmed" || retry.OutlineVersion != 1 {
		t.Fatalf("idempotent confirm failed: %+v, %v", retry, err)
	}
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT outline_version FROM courses WHERE id=$1::uuid`, f.course).Scan(&version); err != nil || version != 2 {
		t.Fatalf("version should still be 2, got %d: %v", version, err)
	}
}

func TestConfirmOptimisticLockingStaleProposalRejectedIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)

	// Simulate concurrent outline change incrementing outline_version to 2
	if _, err := f.db.Pool.Exec(f.ctx, `UPDATE courses SET outline_version=2 WHERE id=$1::uuid`, f.course); err != nil {
		t.Fatal(err)
	}

	// Confirm must be rejected with version_conflict
	_, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID)
	if Code(err) != "version_conflict" {
		t.Fatalf("expected version_conflict, got %v", err)
	}

	// Outline stubs must remain untouched
	var title1 string
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT title FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=1`, f.unit).Scan(&title1); err != nil || title1 != "History" {
		t.Fatalf("outline was modified on stale proposal: %s, %v", title1, err)
	}
}

func TestConfirmExpiredProposalRejectedIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)

	// Expire proposal
	if _, err := f.db.Pool.Exec(f.ctx, `UPDATE course_replan_proposals SET expires_at=now()-interval '1 hour' WHERE id=$1::uuid`, p.ID); err != nil {
		t.Fatal(err)
	}

	// Confirm must be rejected with proposal_expired
	_, err := s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID)
	if Code(err) != "proposal_expired" {
		t.Fatalf("expected proposal_expired, got %v", err)
	}

	// Outline stubs must remain untouched
	var title1 string
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT title FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=1`, f.unit).Scan(&title1); err != nil || title1 != "History" {
		t.Fatalf("outline was modified on expired proposal: %s, %v", title1, err)
	}
}

func TestDiscardProposalLeavesOutlineUnchangedAndKeepsChargeIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)

	// Discard proposal
	discarded, err := s.DiscardReplan(f.ctx, f.owner, f.course, p.ID)
	if err != nil {
		t.Fatalf("discard failed: %v", err)
	}
	if discarded.Decision != "discarded" {
		t.Fatalf("unexpected discarded proposal: %+v", discarded)
	}

	// Outline version and stubs must remain completely unchanged
	var version int64
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT outline_version FROM courses WHERE id=$1::uuid`, f.course).Scan(&version); err != nil || version != 1 {
		t.Fatalf("version changed: %d", version)
	}
	var title1 string
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT title FROM course_lesson_stubs WHERE unit_id=$1::uuid AND position=1`, f.unit).Scan(&title1); err != nil || title1 != "History" {
		t.Fatalf("stub changed: %s", title1)
	}

	// Settled credits must NOT be refunded
	var spent int
	if err = f.db.Pool.QueryRow(f.ctx, `SELECT (result->>'content_spent')::int FROM credit_jobs WHERE id=(SELECT job_id FROM course_replan_proposals WHERE id=$1::uuid)`, p.ID).Scan(&spent); err != nil || spent != 55 {
		t.Fatalf("credits refunded or missing: %d, %v", spent, err)
	}

	// Analytics event emitted
	if f.events.count("course_replan_discarded") != 1 {
		t.Fatalf("expected course_replan_discarded event, got %d", f.events.count("course_replan_discarded"))
	}

	// Idempotent discard
	retry, err := s.DiscardReplan(f.ctx, f.owner, f.course, p.ID)
	if err != nil || retry.Decision != "discarded" {
		t.Fatalf("idempotent discard failed: %+v, %v", retry, err)
	}

	// Confirming a discarded proposal must fail
	_, err = s.ConfirmReplan(f.ctx, f.owner, f.course, p.ID)
	if Code(err) != "proposal_discarded" && Code(err) != "version_conflict" {
		t.Fatalf("confirming discarded proposal should fail: %v", err)
	}
}

func TestOwnerAuthorizationOnConfirmAndDiscardIntegration(t *testing.T) {
	f, s, p := setupConfirmHarness(t)
	otherOwner := "a3333333-3333-4333-8333-333333333333"
	if _, err := f.db.Pool.Exec(f.ctx, `INSERT INTO learners(id,display_name) VALUES($1::uuid,'Other')`, otherOwner); err != nil {
		t.Fatal(err)
	}

	// Non-owner confirm must fail with not_found
	if _, err := s.ConfirmReplan(f.ctx, otherOwner, f.course, p.ID); Code(err) != "not_found" {
		t.Fatalf("expected not_found, got %v", err)
	}

	// Non-owner discard must fail with not_found
	if _, err := s.DiscardReplan(f.ctx, otherOwner, f.course, p.ID); Code(err) != "not_found" {
		t.Fatalf("expected not_found, got %v", err)
	}
}
