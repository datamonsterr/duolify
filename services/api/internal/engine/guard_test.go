package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type guardRPC struct {
	enginev1.EngineServiceClient
	verdict *enginev1.GuardInputResponse
	err     error
	guards  int
	mains   int
	surface string
}

func (g *guardRPC) GuardInput(_ context.Context, in *enginev1.GuardInputRequest, _ ...grpc.CallOption) (*enginev1.GuardInputResponse, error) {
	g.guards++
	g.surface = in.GetSurface()
	return g.verdict, g.err
}
func (g *guardRPC) SetupChat(context.Context, *enginev1.SetupChatRequest, ...grpc.CallOption) (*enginev1.SetupChatResponse, error) {
	g.mains++
	return &enginev1.SetupChatResponse{}, nil
}
func (g *guardRPC) EnrichNote(context.Context, *enginev1.EnrichNoteRequest, ...grpc.CallOption) (*enginev1.EnrichNoteResponse, error) {
	g.mains++
	return &enginev1.EnrichNoteResponse{}, nil
}
func (g *guardRPC) OutlineChatChange(context.Context, *enginev1.OutlineChatChangeRequest, ...grpc.CallOption) (*enginev1.OutlineChatChangeResponse, error) {
	g.mains++
	return &enginev1.OutlineChatChangeResponse{}, nil
}

func newGuarded(g *guardRPC) (*Client, *[]GuardEvent) {
	c := &Client{rpc: g, guard: &guardState{}}
	var seen []GuardEvent
	c.EnableGuard(func(_ context.Context, e GuardEvent) { seen = append(seen, e) })
	return c, &seen
}

func TestGuardOffByDefaultNeverCallsTheGuard(t *testing.T) {
	g := &guardRPC{}
	c := &Client{rpc: g, guard: &guardState{}}
	if _, err := c.SetupChat(context.Background(), &enginev1.SetupChatRequest{Answer: "hello"}); err != nil || g.guards != 0 || g.mains != 1 {
		t.Fatalf("err=%v guards=%d mains=%d", err, g.guards, g.mains)
	}
}

func TestAllowedTextReachesTheMainCallAndBlockedTextDoesNot(t *testing.T) {
	g := &guardRPC{verdict: &enginev1.GuardInputResponse{Allowed: true, Source: "model"}}
	c, seen := newGuarded(g)
	if _, err := c.SetupChat(context.Background(), &enginev1.SetupChatRequest{Answer: "I like maths"}); err != nil || g.mains != 1 || g.surface != SurfaceCourseTopic {
		t.Fatalf("allowed: err=%v mains=%d surface=%s", err, g.mains, g.surface)
	}
	g.verdict = &enginev1.GuardInputResponse{Allowed: false, ReasonCode: "self_harm", Source: "model"}
	_, err := c.OutlineChatChange(context.Background(), &enginev1.OutlineChatChangeRequest{Request: "x"})
	b, ok := Blocked(err)
	if !ok || !b.SafeResponse() || g.mains != 1 || status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("blocked: err=%v mains=%d", err, g.mains)
	}
	if len(*seen) != 2 || (*seen)[1].ReasonCode != "self_harm" || (*seen)[1].Allowed {
		t.Fatalf("events: %+v", *seen)
	}
}

func TestSkippedAndEmptyAnswersAreNotGuarded(t *testing.T) {
	g := &guardRPC{err: errors.New("down")}
	c, _ := newGuarded(g)
	if _, err := c.SetupChat(context.Background(), &enginev1.SetupChatRequest{Skipped: true, Answer: "ignored"}); err != nil || g.guards != 0 {
		t.Fatalf("skipped: err=%v guards=%d", err, g.guards)
	}
	if _, err := c.OutlineChatChange(context.Background(), &enginev1.OutlineChatChangeRequest{Request: "  "}); err != nil || g.guards != 0 {
		t.Fatalf("empty: err=%v guards=%d", err, g.guards)
	}
}

func TestOutageFollowsTheSurfaceFailureRule(t *testing.T) {
	g := &guardRPC{err: status.Error(codes.Unavailable, "down")}
	c, seen := newGuarded(g)
	if _, err := c.OutlineChatChange(context.Background(), &enginev1.OutlineChatChangeRequest{Request: "add a unit"}); err == nil {
		t.Fatal("outline chat must fail closed")
	} else if b, ok := Blocked(err); !ok || b.Source != "failure" {
		t.Fatalf("err=%v", err)
	}
	if _, err := c.GuardedRPC().EnrichNote(context.Background(), &enginev1.EnrichNoteRequest{Note: "my note"}); err != nil {
		t.Fatalf("quick notes fail open: %v", err)
	}
	if len(*seen) != 2 || (*seen)[0].Allowed || !(*seen)[1].Allowed {
		t.Fatalf("events: %+v", *seen)
	}
}

type slowGuard struct {
	guardRPC
	delay time.Duration
}

func (s *slowGuard) GuardInput(ctx context.Context, in *enginev1.GuardInputRequest, o ...grpc.CallOption) (*enginev1.GuardInputResponse, error) {
	time.Sleep(s.delay)
	return s.guardRPC.GuardInput(ctx, in, o...)
}

func heldClient(g enginev1.EngineServiceClient) (*Client, func() []GuardEvent) {
	var mu sync.Mutex
	var seen []GuardEvent
	c := &Client{rpc: g, guard: &guardState{}}
	c.EnableGuard(func(_ context.Context, e GuardEvent) { mu.Lock(); seen = append(seen, e); mu.Unlock() })
	return c, func() []GuardEvent { mu.Lock(); defer mu.Unlock(); return append([]GuardEvent(nil), seen...) }
}

func TestAFastVerdictOnTutorChatBlocksAsBefore(t *testing.T) {
	g := &slowGuard{guardRPC: guardRPC{verdict: &enginev1.GuardInputResponse{ReasonCode: "self_harm", Source: "model"}}}
	c, _ := heldClient(g)
	err := c.check(context.Background(), SurfaceTutorChat, "i want to end it", "")
	if b, ok := Blocked(err); !ok || !b.SafeResponse() {
		t.Fatalf("err=%v", err)
	}
}

func TestALateVerdictFailsOpenThenReportsALateSelfHarmBlock(t *testing.T) {
	g := &slowGuard{guardRPC: guardRPC{verdict: &enginev1.GuardInputResponse{ReasonCode: "self_harm", Source: "model"}}, delay: GuardHold + 150*time.Millisecond}
	c, seen := heldClient(g)
	ctx, late := WithLateVerdict(context.Background())
	started := time.Now()
	if err := c.check(ctx, SurfaceTutorChat, "i want to end it", ""); err != nil {
		t.Fatalf("a late verdict must fail open: %v", err)
	}
	if held := time.Since(started); held > GuardHold+100*time.Millisecond {
		t.Fatalf("held %v, want about %v", held, GuardHold)
	}
	if !late.Pending() {
		t.Fatal("verdict must be pending after the hold")
	}
	select {
	case b := <-late.C():
		if b == nil || !b.SafeResponse() {
			t.Fatalf("late block = %v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the late verdict never completed")
	}
	time.Sleep(20 * time.Millisecond)
	ev := seen()
	if len(ev) != 1 || !ev[0].Late || ev[0].Allowed || ev[0].ReasonCode != "self_harm" {
		t.Fatalf("events: %+v", ev)
	}
	if late.Pending() {
		t.Fatal("no longer pending")
	}
}

func TestALateAllowedVerdictClosesTheHandleWithoutABlock(t *testing.T) {
	g := &slowGuard{guardRPC: guardRPC{verdict: &enginev1.GuardInputResponse{Allowed: true, Source: "model"}}, delay: GuardHold + 100*time.Millisecond}
	c, _ := heldClient(g)
	ctx, late := WithLateVerdict(context.Background())
	if err := c.check(ctx, SurfaceQuickNote, "my note", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case b, ok := <-late.C():
		if ok || b != nil {
			t.Fatalf("expected a closed channel, got %v", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("never completed")
	}
}

func TestClosedSurfacesStillWaitForTheVerdict(t *testing.T) {
	g := &slowGuard{guardRPC: guardRPC{verdict: &enginev1.GuardInputResponse{ReasonCode: "prompt_injection", Source: "model"}}, delay: GuardHold + 100*time.Millisecond}
	c, _ := heldClient(g)
	if _, ok := Blocked(c.check(context.Background(), SurfaceCourseTopic, "x", "")); !ok {
		t.Fatal("a creation surface must wait and block")
	}
}
