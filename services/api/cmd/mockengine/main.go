// Command mockengine is a load-baseline-only Engine behind the API (E18-02). It serves the
// Engine gRPC contract's Health and Chat; Chat holds the stream open silently for
// MOCK_ENGINE_CHAT_SECONDS so the API's SSE handler (and later the edge proxy) is what gets measured.
package main

import (
	"context"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/grpc"
)

type server struct {
	enginev1.UnimplementedEngineServiceServer
	hold time.Duration
}

func (s *server) Health(context.Context, *enginev1.HealthRequest) (*enginev1.HealthResponse, error) {
	return &enginev1.HealthResponse{}, nil
}

func (s *server) Chat(r *enginev1.ChatRequest, stream enginev1.EngineService_ChatServer) error {
	send := func(e *enginev1.ChatEvent) error { return stream.Send(e) }
	if err := send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Delta{Delta: "Let me think about that. "}}); err != nil {
		return err
	}
	select {
	case <-time.After(s.hold):
	case <-stream.Context().Done():
		return stream.Context().Err()
	}
	if err := send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Delta{Delta: "A moon orbits a planet."}}); err != nil {
		return err
	}
	return send(&enginev1.ChatEvent{Event: &enginev1.ChatEvent_Usage{Usage: &enginev1.Usage{Provider: r.GetLimits().GetProvider(), Model: r.GetLimits().GetModel(), InputTokens: 10, OutputTokens: 8}}})
}

func main() {
	addr := ":50051"
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		c, err := (&net.Dialer{Timeout: 2 * time.Second}).DialContext(context.Background(), "tcp", "127.0.0.1:50051")
		if err != nil {
			os.Exit(1)
		}
		_ = c.Close()
		return
	}
	secs, err := strconv.Atoi(os.Getenv("MOCK_ENGINE_CHAT_SECONDS"))
	if err != nil || secs < 0 {
		secs = 600
	}
	lis, err := new(net.ListenConfig).Listen(context.Background(), "tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	rpc := grpc.NewServer()
	enginev1.RegisterEngineServiceServer(rpc, &server{hold: time.Duration(secs) * time.Second})
	log.Printf("mock engine on %s, chat hold %ds", addr, secs)
	log.Fatal(rpc.Serve(lis))
}
