// Package engine wraps the private gRPC engine with bounded calls.
package engine

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Client owns a reusable gRPC connection and per-call deadline.
type Client struct {
	conn    *grpc.ClientConn
	rpc     enginev1.EngineServiceClient
	timeout time.Duration
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
	conn, err := grpc.NewClient("dns:///"+u.Host, grpc.WithTransportCredentials(creds), grpc.WithUnaryInterceptor(correlate))
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

// PreviewCourse forwards the supplied context and bounds engine execution time.
func (c *Client) PreviewCourse(ctx context.Context, request *enginev1.PreviewCourseRequest) (*enginev1.PreviewCourseResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
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
