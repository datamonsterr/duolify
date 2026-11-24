package characters_test

import (
	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/characters"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestPinnedSegmentsRejectUnknownSpeakersAndAssets(t *testing.T) {
	manifest, _ := characters.ProjectDefaultCastManifest("", "")
	segment := &pb.CharacterSegment{SpeakerKey: "guide", Text: "Earth turns.", Expression: "thinking", Pose: "point", DurationMs: 3000}
	for _, mutate := range []func(*pb.CharacterSegment){
		func(s *pb.CharacterSegment) { s.SpeakerKey = "system" },
		func(s *pb.CharacterSegment) { s.Expression = "angry" },
		func(s *pb.CharacterSegment) { s.Pose = "flying" },
		func(s *pb.CharacterSegment) { s.Text = "https://evil.test/image.svg" },
		func(s *pb.CharacterSegment) { s.Text = "<svg onload='alert(1)'/>" },
		func(s *pb.CharacterSegment) { s.DurationMs = 3001 },
	} {
		clone := proto.Clone(segment).(*pb.CharacterSegment)
		mutate(clone)
		if characters.ValidateSegments([]*pb.CharacterSegment{clone}, manifest.Cast, 3000) == nil {
			t.Fatalf("unsafe segment accepted: %s", clone)
		}
	}
	if err := characters.ValidateSegments([]*pb.CharacterSegment{segment}, manifest.Cast, 3000); err != nil {
		t.Fatal(err)
	}
	legacy := proto.Clone(segment).(*pb.CharacterSegment)
	legacy.Pose = ""
	projected := characters.ProjectSegments([]*pb.CharacterSegment{segment, legacy}, manifest.Cast)
	if projected[0].Pose != "point" || projected[1].Pose != "" {
		t.Fatalf("manifest changed segment poses: %+v", projected)
	}
}
