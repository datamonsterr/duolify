package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Guard surfaces (ADR 0025). Creation surfaces fail closed; chat, notes and transcripts fail open
// because the engine has already run the deterministic pre-filter and the text stays delimited data.
const (
	SurfaceCourseTopic = "course_topic"
	SurfaceOutlineChat = "outline_chat"
	SurfaceQuickNote   = "quick_note"
	SurfaceTutorChat   = "tutor_chat"
)

// GuardEvent is what an observer sees: a code per decision, never the guarded text.
type GuardEvent struct {
	Surface, ReasonCode, Source string
	Allowed                     bool
	Latency                     time.Duration
}

// BlockedError is returned instead of calling the main model. SafeResponse asks the caller for the
// owner-approved self-harm copy and no Credit charge; the thread stays usable.
type BlockedError struct {
	Surface, ReasonCode, Source string
}

func (e *BlockedError) Error() string { return "input_blocked: " + e.ReasonCode }

// GRPCStatus keeps existing generic engine-error handling working (a refusal, not an outage).
func (e *BlockedError) GRPCStatus() *status.Status {
	return status.New(codes.FailedPrecondition, e.Error())
}

// SafeResponse is true for a self_harm verdict.
func (e *BlockedError) SafeResponse() bool { return e.ReasonCode == "self_harm" }

// Blocked extracts a guard refusal from an error chain.
func Blocked(err error) (*BlockedError, bool) {
	var b *BlockedError
	return b, errors.As(err, &b)
}

type guardState struct {
	on      atomic.Bool
	observe atomic.Pointer[func(context.Context, GuardEvent)]
}

// EnableGuard turns the input guard on for every learner free-text engine call. Until it is called
// (INPUT_GUARD_ENABLED unset) the client behaves exactly as before. observe may be nil.
func (c *Client) EnableGuard(observe func(context.Context, GuardEvent)) {
	if observe != nil {
		c.guard.observe.Store(&observe)
	}
	c.guard.on.Store(true)
}

func failsOpen(surface string) bool {
	return surface == SurfaceTutorChat || surface == SurfaceQuickNote
}

// check asks the engine for one verdict. A transport failure follows the surface rule locally.
func (c *Client) check(ctx context.Context, surface, text, topic string) error {
	if !c.guard.on.Load() || strings.TrimSpace(text) == "" {
		return nil
	}
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := c.rpc.GuardInput(callCtx, &enginev1.GuardInputRequest{Surface: surface, Text: text, CourseTopic: topic})
	ev := GuardEvent{Surface: surface, Latency: time.Since(started)}
	switch {
	case err != nil:
		ev.Source = "failure"
		ev.Allowed = failsOpen(surface)
		if !ev.Allowed {
			ev.ReasonCode = "unsafe_content"
		}
	default:
		ev.Allowed, ev.ReasonCode, ev.Source = res.GetAllowed(), res.GetReasonCode(), res.GetSource()
	}
	if f := c.guard.observe.Load(); f != nil {
		(*f)(ctx, ev)
	}
	if ev.Allowed {
		return nil
	}
	return &BlockedError{Surface: surface, ReasonCode: ev.ReasonCode, Source: ev.Source}
}

// lastUser returns the newest learner message of a tutor turn.
func lastUser(m []*enginev1.ChatMessage) string {
	for i := len(m) - 1; i >= 0; i-- {
		if m[i].GetRole() == "user" {
			return m[i].GetContent()
		}
	}
	return ""
}

// previewText joins the free-text brief fields a preview sends to the model.
func previewText(r *enginev1.PreviewCourseRequest) string {
	b := r.GetBrief()
	return strings.TrimSpace(strings.Join([]string{r.GetTopic(), b.GetTopic(), b.GetGoal(), b.GetIncludeTopics(), b.GetSkipTopics()}, "\n"))
}

// GuardedRPC is the raw engine client with the guard in front of note enrichment, for services that
// take an EngineServiceClient (notes). Other methods pass through untouched.
func (c *Client) GuardedRPC() enginev1.EngineServiceClient { return guardedRPC{c.rpc, c} }

type guardedRPC struct {
	enginev1.EngineServiceClient
	c *Client
}

func (g guardedRPC) EnrichNote(ctx context.Context, in *enginev1.EnrichNoteRequest, opts ...grpc.CallOption) (*enginev1.EnrichNoteResponse, error) {
	if err := g.c.check(ctx, SurfaceQuickNote, in.GetNote(), ""); err != nil {
		return nil, err
	}
	return g.EngineServiceClient.EnrichNote(ctx, in, opts...)
}
