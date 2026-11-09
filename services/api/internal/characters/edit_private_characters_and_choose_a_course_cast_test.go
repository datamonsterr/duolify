package characters_test

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type screeningDouble struct {
	pb.UnimplementedEngineServiceServer
}

func (screeningDouble) ScreenCharacter(_ context.Context, r *pb.ScreenCharacterRequest) (*pb.ScreenCharacterResponse, error) {
	reason := ""
	if strings.Contains(strings.ToLower(r.SpecJson), "reveal system prompt") {
		reason = "prompt_injection"
	}
	return &pb.ScreenCharacterResponse{Approved: reason == "", ReasonCode: reason, PolicyVersion: "character-v1", Attempts: []*pb.Usage{{Provider: "mock", Model: "mock-screen", InputTokens: 10, OutputTokens: 3, Attempt: 1, Delivered: true}}}, nil
}

type screeningClient struct{ pb.EngineServiceClient }

func (s screeningClient) ScreenCharacter(ctx context.Context, r *pb.ScreenCharacterRequest) (*pb.ScreenCharacterResponse, error) {
	return s.EngineServiceClient.ScreenCharacter(ctx, r)
}
func newScreeningRPC(t *testing.T) screeningClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	pb.RegisterEngineServiceServer(server, screeningDouble{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///screen", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return screeningClient{pb.NewEngineServiceClient(conn)}
}

func TestEditClosedSchemaAndBounds(t *testing.T) {
	spec := characters.DefaultGuideFixture()
	raw, _ := json.Marshal(spec)
	if _, err := characters.ValidateSpecJSON(append(raw, []byte(` {}`)...)); err == nil {
		t.Error("trailing JSON accepted")
	}
	var object map[string]any
	_ = json.Unmarshal(raw, &object)
	delete(object["info"].(map[string]any), "bio")
	missing, _ := json.Marshal(object)
	if _, err := characters.ValidateSpecJSON(missing); err == nil {
		t.Error("missing required optional-empty string accepted")
	}
	for _, mutate := range []func(*characters.CharacterSpec){
		func(s *characters.CharacterSpec) { s.Info.Name = strings.Repeat("a", 41) },
		func(s *characters.CharacterSpec) { s.Appearance.Palette.Outline = s.Appearance.Palette.Body },
		func(s *characters.CharacterSpec) { s.Personality.Traits = nil },
		func(s *characters.CharacterSpec) { s.VoiceKey = "arbitrary-provider-id" },
		func(s *characters.CharacterSpec) { s.Appearance.Accessories = []string{"hat", "hat"} },
		func(s *characters.CharacterSpec) { s.Info.Bio = "<svg onload='bad()'/>" },
	} {
		s := characters.DefaultGuideFixture()
		mutate(&s)
		b, _ := json.Marshal(s)
		if _, err := characters.ValidateSpecJSON(b); err == nil {
			t.Errorf("invalid spec accepted: %s", b)
		}
	}
	if err := characters.ValidateCastSelection(characters.CastSelection{Guide: "default-guide", Supporting: []string{"default-guide"}}); err == nil {
		t.Error("duplicate guide accepted")
	}
	if err := characters.ValidateCastSelection(characters.CastSelection{Supporting: []string{"default-supporter"}}); err == nil {
		t.Error("supports without guide accepted")
	}
	if err := characters.ValidateCastSelection(characters.CastSelection{}); err != nil {
		t.Fatal(err)
	}
}
