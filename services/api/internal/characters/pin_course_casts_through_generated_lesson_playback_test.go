package characters_test

import (
	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestPinnedSegmentsRejectUnknownSpeakersAndAssets(t *testing.T) {
	manifest, _ := characters.ProjectDefaultCastManifest("", "")
	segment := &pb.CharacterSegment{SpeakerKey: "guide", Text: "Earth turns.", Expression: "thinking", DurationMs: 3000}
	for _, mutate := range []func(*pb.CharacterSegment){
		func(s *pb.CharacterSegment) { s.SpeakerKey = "system" },
		func(s *pb.CharacterSegment) { s.Expression = "angry" },
		func(s *pb.CharacterSegment) { s.Text = "https://evil.test/image.svg" },
		func(s *pb.CharacterSegment) { s.Text = "<svg onload='alert(1)'/>" },
		func(s *pb.CharacterSegment) { s.DurationMs = 3001 },
	} {
		copy := proto.Clone(segment).(*pb.CharacterSegment)
		mutate(copy)
		if characters.ValidateSegments([]*pb.CharacterSegment{copy}, manifest.Cast, 3000) == nil {
			t.Fatalf("unsafe segment accepted: %s", copy)
		}
	}
	if err := characters.ValidateSegments([]*pb.CharacterSegment{segment}, manifest.Cast, 3000); err != nil {
		t.Fatal(err)
	}
}
