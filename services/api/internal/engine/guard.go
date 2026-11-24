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
	Late                        bool // the verdict arrived after GuardHold and did not hold the turn
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

// GuardHold is how long a fail-open surface (tutor chat, quick note) waits for the verdict before
// proceeding (design.md section 14). A verdict that is late still completes in the background.
const GuardHold = 800 * time.Millisecond

// verdict asks the engine for one verdict. A transport failure follows the surface rule locally.
func (c *Client) verdict(ctx context.Context, surface, text, topic string) GuardEvent {
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
	return ev
}

func (c *Client) observe(ctx context.Context, ev GuardEvent) {
	if f := c.guard.observe.Load(); f != nil {
		(*f)(ctx, ev)
	}
}

func (ev GuardEvent) blocked() *BlockedError {
	if ev.Allowed {
		return nil
	}
	return &BlockedError{Surface: ev.Surface, ReasonCode: ev.ReasonCode, Source: ev.Source}
}

// check is the guard for surfaces that wait for the verdict (creation and publication, fail closed).
// Tutor chat and quick notes use checkHeld.
func (c *Client) check(ctx context.Context, surface, text, topic string) error {
	if !c.guard.on.Load() || strings.TrimSpace(text) == "" {
		return nil
	}
	if failsOpen(surface) {
		return c.checkHeld(ctx, surface, text, topic)
	}
	ev := c.verdict(ctx, surface, text, topic)
	c.observe(ctx, ev)
	if b := ev.blocked(); b != nil {
		return b
	}
	return nil
}

// checkHeld waits GuardHold for the verdict. A verdict in time decides as usual. A late one fails
// open: the turn proceeds, the verdict completes in the background (its event is marked Late), and a
// late block is handed to the LateVerdict in ctx, if any, so a late self_harm can still reach the learner.
func (c *Client) checkHeld(ctx context.Context, surface, text, topic string) error {
	done := make(chan GuardEvent, 1)
	bg := context.WithoutCancel(ctx)
	go func() { done <- c.verdict(bg, surface, text, topic) }()
	hold := time.NewTimer(GuardHold)
	defer hold.Stop()
	select {
	case ev := <-done:
		c.observe(ctx, ev)
		if b := ev.blocked(); b != nil {
			return b
		}
		return nil
	case <-hold.C:
	}
	late, _ := ctx.Value(lateKey{}).(*LateVerdict)
	if late != nil {
		late.pending.Store(true)
	}
	go func() {
		ev := <-done
		ev.Late = true
		c.observe(bg, ev)
		if b := ev.blocked(); b != nil && late != nil {
			late.ch <- b
		}
		if late != nil {
			late.pending.Store(false)
			close(late.ch)
		}
	}()
	return nil
}

type lateKey struct{}

// LateVerdict receives a block that arrived after the hold. The channel is closed when the verdict
// is in (a block is sent first), so a reader can wait for it without polling.
type LateVerdict struct {
	ch      chan *BlockedError
	pending atomic.Bool
}

// Pending is true when the hold expired and the verdict has not arrived yet.
func (l *LateVerdict) Pending() bool { return l != nil && l.pending.Load() }

// WithLateVerdict returns a context whose held guard reports a late block to the returned handle.
func WithLateVerdict(ctx context.Context) (context.Context, *LateVerdict) {
	l := &LateVerdict{ch: make(chan *BlockedError, 1)}
	return context.WithValue(ctx, lateKey{}, l), l
}

// C yields the late block (if any), then closes. When the guard answered in time it never yields,
// so callers check Pending before they wait.
func (l *LateVerdict) C() <-chan *BlockedError {
	if l == nil {
		return nil
	}
	return l.ch
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
