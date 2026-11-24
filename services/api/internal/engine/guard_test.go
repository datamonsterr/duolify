package engine

import (
	"context"
	"errors"
	"testing"

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
