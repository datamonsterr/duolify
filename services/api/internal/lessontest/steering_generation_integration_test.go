//go:build integration

package lessontest_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

type steeringScreen struct{}

func (steeringScreen) ScreenInstruction(context.Context, *pb.ScreenInstructionRequest) (*pb.ScreenInstructionResponse, error) {
	return &pb.ScreenInstructionResponse{Allowed: true, ScreeningVersion: "steering-v1", Attempts: []*pb.Usage{{Provider: "mock", Model: "mock-screen", InputTokens: 20, OutputTokens: 4, Delivered: true}}}, nil
}

func TestSteeringRealGatedJobUsesInstructionOnceAndPreservesPastContent(t *testing.T) {
	f := newReviewFixture(t)
	ctx := t.Context()
	add := func(text string) string {
		t.Helper()
		code, out, raw := f.call("POST", "/v1/courses/"+f.course+"/instructions", map[string]any{"text": text, "scope": "next_lesson"}, f.token)
		if code != 201 {
			t.Fatalf("add %d %s", code, raw)
		}
		return out["id"].(string)
	}
	first := add("More examples in Go")
	job := f.prepare(t)
	second := add("Use concise explanations") // queued jobs have already taken their snapshot
	if err := f.service.RunLessonGeneration(ctx, job); err != nil {
		t.Fatal(err)
	}
	if len(f.engine.request.Instructions) != 1 || f.engine.request.Instructions[0].Id != first {
		t.Fatal(f.engine.request.Instructions)
	}
	var status string
	if err := f.db.Pool.QueryRow(ctx, `SELECT status FROM course_instructions WHERE id=$1::uuid`, first).Scan(&status); err != nil || status != "applied" {
		t.Fatalf("consumption %s %v", status, err)
	}
	var before []byte
	if err := f.db.Pool.QueryRow(ctx, `SELECT to_jsonb(g)::text FROM generated_lessons g WHERE lesson_stub_id=$1::uuid`, f.lesson).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := f.service.RunLessonGeneration(ctx, job); err != nil {
		t.Fatal(err)
	}
	code, out, raw := f.call("GET", "/v1/lessons/"+f.lesson+"/test", nil, f.token)
	if code != 200 {
		t.Fatalf("test %d %s", code, raw)
	}
	var next string
	for _, item := range out["questions"].([]any) {
		question := item.(map[string]any)
		code, out, raw = f.call("POST", "/v1/lessons/"+f.lesson+"/test/answer", map[string]any{"question_id": question["question_id"], "choice_index": 0}, f.token)
		if code != 200 {
			t.Fatalf("answer %d %s", code, raw)
		}
		if out["next_lesson_id"] != nil {
			next = out["next_lesson_id"].(string)
		}
	}
	if out["passed"] != true || next == "" {
		t.Fatalf("server mastery %v", out)
	}
	f.lesson = next
	nextJob := f.prepare(t)
	f.engine.err = errors.New("mock unavailable")
	if err := f.service.RunLessonGeneration(ctx, nextJob); err == nil {
		t.Fatal("failed provider was delivered")
	}
	if err := f.db.Pool.QueryRow(ctx, `SELECT status FROM course_instructions WHERE id=$1::uuid`, second).Scan(&status); err != nil || status != "active" {
		t.Fatalf("failure consumed instruction %s %v", status, err)
	}
	var released string
	if err := f.db.Pool.QueryRow(ctx, `SELECT j.state FROM credit_jobs j JOIN lesson_generations g ON g.reservation_id=j.id WHERE g.id=$1::uuid`, nextJob).Scan(&released); err != nil || released != "released" {
		t.Fatalf("failed reservation %s %v", released, err)
	}
	f.engine.err = nil
	nextJob = f.prepare(t)
	if err := f.service.RunLessonGeneration(ctx, nextJob); err != nil {
		t.Fatal(err)
	}
	if len(f.engine.request.Instructions) != 1 || f.engine.request.Instructions[0].Id != second {
		t.Fatalf("next snapshot %v", f.engine.request.Instructions)
	}
	// Later additions and generation never rewrite any byte of the first generated Lesson.
	var after []byte
	if err := f.db.Pool.QueryRow(ctx, `SELECT to_jsonb(g)::text FROM generated_lessons g JOIN lesson_generations j ON j.lesson_id=g.lesson_stub_id WHERE j.id=$1::uuid`, job).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("past lesson mutated")
	}
	var manifest map[string]any
	var rawManifest []byte
	if err := f.db.Pool.QueryRow(ctx, `SELECT manifest FROM generated_lessons WHERE lesson_stub_id=$1::uuid`, f.lesson).Scan(&rawManifest); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawManifest, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["lesson_instruction_ids"].([]any)[0] != second {
		t.Fatal(manifest)
	}
}
