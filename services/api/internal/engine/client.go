// Package engine wraps the private gRPC engine with bounded calls.
package engine

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Client owns a reusable gRPC connection and per-call deadline.
type Client struct {
	conn    *grpc.ClientConn
	rpc     enginev1.EngineServiceClient
	timeout time.Duration
}

// quoted bounds a quote-priced generation call. The caller's quote/reservation deadline (minutes
// for a live model) governs when it has one; the short default applies only to unbounded callers.
func (c *Client) quoted(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, c.timeout)
}

// ReplanOutline retains the action-specific 90s ceiling and the reservation deadline.
func (c *Client) ReplanOutline(ctx context.Context, r *enginev1.ReplanOutlineRequest) (*enginev1.ReplanOutlineResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return c.rpc.ReplanOutline(ctx, r)
}

// ScreenInstruction bounds platform-paid instruction screening.
func (c *Client) ScreenInstruction(ctx context.Context, r *enginev1.ScreenInstructionRequest) (*enginev1.ScreenInstructionResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.rpc.ScreenInstruction(ctx, r)
}

// ScreenCharacter bounds platform-paid screening independently of learner Credits.
func (c *Client) ScreenCharacter(ctx context.Context, r *enginev1.ScreenCharacterRequest) (*enginev1.ScreenCharacterResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.rpc.ScreenCharacter(ctx, r)
}

// ScreenCourse screens a bounded publication sample for the platform; the caller supplies the 120s deadline.
func (c *Client) ScreenCourse(ctx context.Context, r *enginev1.ScreenCourseRequest) (*enginev1.ScreenCourseResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 125*time.Second)
	defer cancel()
	return c.rpc.ScreenCourse(ctx, r)
}

// ScreenReview bounds platform-paid review screening to ten seconds.
func (c *Client) ScreenReview(ctx context.Context, r *enginev1.ScreenReviewRequest) (*enginev1.ScreenReviewResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return c.rpc.ScreenReview(ctx, r)
}

// MapCharacter is bounded by the quote's total job deadline and the engine's per-call ceiling.
func (c *Client) MapCharacter(ctx context.Context, r *enginev1.MapCharacterRequest) (*enginev1.MapCharacterResponse, error) {
	return c.rpc.MapCharacter(ctx, r)
}

// OutlineChatChange uses the same private transport and bounded request context.
func (c *Client) OutlineChatChange(ctx context.Context, r *enginev1.OutlineChatChangeRequest) (*enginev1.OutlineChatChangeResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.rpc.OutlineChatChange(ctx, r)
}

// Chat opens the tutor stream. The caller's context carries the reservation deadline, so no
// extra timeout is applied here and cancelling it ends the stream.
func (c *Client) Chat(ctx context.Context, r *enginev1.ChatRequest) (enginev1.EngineService_ChatClient, error) {
	cor := telemetry.CorrelationFrom(ctx)
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set("x-request-id", cor.RequestID)
	md.Set("traceparent", cor.Traceparent())
	return c.rpc.Chat(metadata.NewOutgoingContext(ctx, md), r)
}

// retryAttempts and retryBackoffs bound the deploy-restart retry: five attempts, about 7 s of waiting.
const retryAttempts = 5

var retryBackoffs = [...]time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}

// retryUnavailable retries a unary call that never reached an engine handler (connection refused or reset
// while the engine is being replaced in a deploy). An UNAVAILABLE the engine itself answered, such as
// provider_outage or screening_unavailable, is never retried, so paid work is not re-run. Every attempt is bounded by the caller's deadline and reuses its reservation.
func retryUnavailable(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	var err error
	for attempt := 0; attempt < retryAttempts; attempt++ {
		err = invoker(ctx, method, req, reply, cc, opts...)
		if err == nil || !transportDown(err) || attempt == retryAttempts-1 {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(retryBackoffs[attempt]):
		}
	}
	return err
}

// transportDown reports an UNAVAILABLE raised by the gRPC transport itself (no connection, reset, draining).
// Its message is generated by grpc-go; handler-returned statuses carry the engine's own reason codes.
func transportDown(err error) bool {
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unavailable {
		return false
	}
	m := st.Message()
	for _, marker := range []string{"connection error", "error reading from server", "connection refused", "transport is closing", "connection is draining", "last connection error", "name resolver error"} {
		if strings.Contains(m, marker) {
			return true
		}
	}
	return false
}

// New accepts the ENGINE_ADDR http(s) URL; http is for the private local network.
func New(addr string, timeout time.Duration) (*Client, error) {
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || timeout <= 0 {
		return nil, fmt.Errorf("invalid engine address or timeout")
	}
	creds := insecure.NewCredentials()
	if u.Scheme == "https" {
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	conn, err := grpc.NewClient("dns:///"+u.Host, grpc.WithTransportCredentials(creds), grpc.WithChainUnaryInterceptor(correlate, retryUnavailable), grpc.WithStreamInterceptor(watchStream))
	if err != nil {
		return nil, fmt.Errorf("create engine client: %w", err)
	}
	return &Client{conn: conn, rpc: enginev1.NewEngineServiceClient(conn), timeout: timeout}, nil
}

// correlate forwards safe metadata only and records status without request/error bodies.
func correlate(ctx context.Context, method string, req, reply any, conn *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	ctx, end := telemetry.FromContext(ctx).Start(ctx, "grpc")
	c := telemetry.CorrelationFrom(ctx)
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set("x-request-id", c.RequestID)
	md.Set("traceparent", c.Traceparent())
	err := invoke(metadata.NewOutgoingContext(ctx, md), method, req, reply, conn, opts...)
	logNotPromoted(ctx, method, err)
	end(err == nil)
	return err
}

// Close releases the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Health checks the engine over its actual contract.
func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_, err := c.rpc.Health(ctx, &enginev1.HealthRequest{})
	return err
}

// Providers returns the engine's primary and fallback provider names (Health).
func (c *Client) Providers(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	res, err := c.rpc.Health(ctx, &enginev1.HealthRequest{})
	if err != nil {
		return nil, err
	}
	return res.GetProviders(), nil
}

// PreviewCourse forwards the supplied context and bounds engine execution time.
func (c *Client) PreviewCourse(ctx context.Context, request *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	ctx, cancel := c.quoted(ctx)
	defer cancel()
	return c.rpc.PreviewCourse(ctx, request)
}

// SetupChat forwards the setup turn with a bounded context.
func (c *Client) SetupChat(ctx context.Context, request *enginev1.SetupChatRequest) (*enginev1.SetupChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.rpc.SetupChat(ctx, request)
}

// CourseBriefChat forwards request-local intent with the API deadline.
func (c *Client) CourseBriefChat(ctx context.Context, request *enginev1.CourseBriefChatRequest) (*enginev1.CourseBriefChatResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	return c.rpc.CourseBriefChat(ctx, request)
}

// RPC returns the underlying EngineServiceClient.
func (c *Client) RPC() enginev1.EngineServiceClient {
	return c.rpc
}

// EnrichNote forwards note enrichment within the context deadline.
func (c *Client) EnrichNote(ctx context.Context, request *enginev1.EnrichNoteRequest) (*enginev1.EnrichNoteResponse, error) {
	return c.rpc.EnrichNote(ctx, request)
}

// GenerateCourseOutline forwards the exact authored outline within the API deadline.
func (c *Client) GenerateCourseOutline(ctx context.Context, r *enginev1.GenerateCourseOutlineRequest) (*enginev1.GenerateCourseOutlineResponse, error) {
	ctx, cancel := c.quoted(ctx)
	defer cancel()
	return c.rpc.GenerateCourseOutline(ctx, r)
}

// GenerateLesson streams lesson generation and returns the completed lesson and usage attempts.
func (c *Client) GenerateLesson(ctx context.Context, req *enginev1.GenerateLessonRequest) (*enginev1.Lesson, []*enginev1.Usage, error) {
	ctx, cancel := c.quoted(ctx)
	defer cancel()
	stream, err := c.rpc.GenerateLesson(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	var lesson *enginev1.Lesson
	var attempts []*enginev1.Usage
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, attempts, err
		}
		switch e := ev.Event.(type) {
		case *enginev1.GenerateLessonEvent_Lesson:
			lesson = e.Lesson
		case *enginev1.GenerateLessonEvent_Usage:
			attempts = append(attempts, e.Usage)
		case *enginev1.GenerateLessonEvent_Attempt:
			attempts = append(attempts, e.Attempt)
		case *enginev1.GenerateLessonEvent_Unavailable:
			if e.Unavailable {
				return nil, attempts, errors.New("engine unavailable")
			}
		}
	}
	if lesson == nil {
		return nil, attempts, errors.New("no lesson delivered from engine")
	}
	return lesson, attempts, nil
}
