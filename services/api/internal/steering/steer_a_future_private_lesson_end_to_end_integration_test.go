//go:build integration

package steering

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store"
	"github.com/datamonsterr/duolify/services/api/migrations"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type screenDouble struct {
	down  bool
	calls int
}

func (s *screenDouble) ScreenInstruction(ctx context.Context, _ *pb.ScreenInstructionRequest) (*pb.ScreenInstructionResponse, error) {
	s.calls++
	d, ok := ctx.Deadline()
	if !ok || time.Until(d) > 10*time.Second {
		return nil, errors.New("missing deadline")
	}
	if s.down {
		return nil, errors.New("offline")
	}
	return &pb.ScreenInstructionResponse{Allowed: true, ScreeningVersion: "steering-v1", Attempts: []*pb.Usage{{Provider: "mock", Model: "mock-screen", InputTokens: 20, OutputTokens: 5, Attempt: 1, Delivered: true}}}, nil
}

func TestSteerFuturePrivateLessonSnapshotAndConsumption(t *testing.T) {
	ctx := t.Context()
	c, err := postgres.Run(ctx, "postgres:18", postgres.WithDatabase("steering"), postgres.WithUsername("fixture"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	testcontainers.CleanupContainer(t, c)
	url, err := c.ConnectionString(ctx, "sslmode=disable")
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
	const owner = "a1111111-1111-4111-8111-111111111111"
	if _, err = db.Pool.Exec(ctx, `INSERT INTO learners(id,display_name) VALUES($1::uuid,'Learner')`, owner); err != nil {
		t.Fatal(err)
	}
	q, err := pricing.NewQuotes(db.Pool).Create(ctx, owner, pricing.QuoteInput{Action: "lesson_content"})
	if err != nil {
		t.Fatal(err)
	}
	var course, lesson, job string
	err = db.Pool.QueryRow(ctx, `WITH cr AS (INSERT INTO course_creations(learner_id,draft_id,revision,quote_id,request,state) VALUES($1::uuid,gen_random_uuid(),1,$2::uuid,'{}','enrolled') RETURNING id), c AS (INSERT INTO courses(learner_id,creation_id,title) SELECT $1::uuid,id,'Go' FROM cr RETURNING id), u AS (INSERT INTO course_units(learner_id,course_id,authored_id,position,title) SELECT $1::uuid,id,'u',0,'Basics' FROM c RETURNING id,course_id), l AS (INSERT INTO course_lesson_stubs(learner_id,unit_id,authored_id,position,title,goal,status) SELECT $1::uuid,id,'l',0,'Functions','Learn Go','generating' FROM u RETURNING id), j AS (INSERT INTO lesson_generations(learner_id,course_id,lesson_id,quote_id,state) SELECT $1::uuid,u.course_id,l.id,$2::uuid,'queued' FROM u,l RETURNING id) SELECT u.course_id::text,l.id::text,j.id::text FROM u,l,j`, owner, q.ID).Scan(&course, &lesson, &job)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Pool.Exec(ctx, `INSERT INTO course_enrollments(learner_id,course_id) VALUES($1::uuid,$2::uuid)`, owner, course); err != nil {
		t.Fatal(err)
	}
	screener := &screenDouble{}
	s := New(Options{Pool: db.Pool, Engine: screener})
	screener.down = true
	if _, err = s.Add(ctx, owner, course, Input{Text: "More examples in Go", Scope: "next_lesson"}); Code(err) != "screening_unavailable" {
		t.Fatalf("fail closed: %v", err)
	}
	var n int
	if err = db.Pool.QueryRow(ctx, `SELECT count(*) FROM course_instructions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("saved on failure: %d %v", n, err)
	}
	screener.down = false
	first, err := s.Add(ctx, owner, course, Input{Text: "More examples in Go", Scope: "next_lesson"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = PinTx(ctx, tx, owner, course, job); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	later, err := s.Add(ctx, owner, course, Input{Text: "Use concise explanations", Scope: "next_lesson"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err = db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = PinTx(ctx, tx, owner, course, job); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Snapshot(ctx, db.Pool, owner, job)
	if err != nil || len(snapshot) != 1 || snapshot[0].Id != first.ID || snapshot[0].Text != first.Text {
		t.Fatalf("snapshot=%v err=%v", snapshot, err)
	}
	// A retry reads the same immutable text/version, including after a current-list mutation.
	if _, err = db.Pool.Exec(ctx, `UPDATE course_instructions SET text='future edit',version=2 WHERE id=$1::uuid`, first.ID); err != nil {
		t.Fatal(err)
	}
	snapshot, err = Snapshot(ctx, db.Pool, owner, job)
	if err != nil || snapshot[0].Text != first.Text || snapshot[0].Version != 1 {
		t.Fatalf("retry snapshot=%v %v", snapshot, err)
	}
	tx, err = db.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := ApplyTx(ctx, tx, owner, job, lesson)
	if err != nil || len(ids) != 1 {
		t.Fatalf("apply %v %v", ids, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.Latest(ctx, owner, course)
	if err != nil || got.ID != later.ID || got.Status != "active" {
		t.Fatalf("running boundary: %+v %v", got, err)
	}
	var state string
	if err = db.Pool.QueryRow(ctx, `SELECT status FROM course_instructions WHERE id=$1::uuid`, first.ID).Scan(&state); err != nil || state != "applied" {
		t.Fatalf("state %s %v", state, err)
	}

	// Configuration is enforced server-side under the Course lock.
	if _, err = db.Pool.Exec(ctx, `UPDATE steering_config SET max_active=1`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Add(ctx, owner, course, Input{Text: "another", Scope: "next_lesson"}); Code(err) != "active_limit" {
		t.Fatalf("count limit %v", err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE steering_config SET max_active=20,max_total_characters=30`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Add(ctx, owner, course, Input{Text: strings.Repeat("a", 30), Scope: "rest_of_course"}); Code(err) != "total_length_limit" {
		t.Fatalf("total limit %v", err)
	}
	if _, err = db.Pool.Exec(ctx, `UPDATE steering_config SET max_total_characters=4000`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err = s.Add(ctx, owner, course, Input{Text: strings.Repeat("a", 5), Scope: "rest_of_course"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Add(ctx, owner, course, Input{Text: "rate limited", Scope: "next_lesson"}); Code(err) != "rate_limit" {
		t.Fatalf("rate limit %v", err)
	}

	if _, err = db.Pool.Exec(ctx, `UPDATE course_instructions SET status='deleted',deleted_at=now()-interval '31 days' WHERE id=$1::uuid`, first.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.Purge(ctx); err != nil {
		t.Fatal(err)
	}
	snapshot, err = Snapshot(ctx, db.Pool, owner, job)
	if err != nil || len(snapshot) != 1 || snapshot[0].Text != first.Text {
		t.Fatalf("purge removed historical snapshot %v %v", snapshot, err)
	}
	if _, err = db.Pool.Exec(ctx, `DELETE FROM learners WHERE id=$1::uuid`, owner); err != nil {
		t.Fatal(err)
	}
	if err = db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM course_instructions)+(SELECT count(*) FROM lesson_instruction_snapshots)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("erasure %d %v", n, err)
	}
}
