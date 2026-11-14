package steering

import (
	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/protobuf/proto"
	"testing"
)

func outlineFixture() *pb.CourseOutline {
	return &pb.CourseOutline{CourseId: "course", Title: "Go", Units: []*pb.Unit{{UnitId: "u1", Title: "Basics", Lessons: []*pb.LessonRef{{LessonId: "l1", Title: "Past", Goal: "Keep"}, {LessonId: "l2", Title: "History", Goal: "Focus"}}}, {UnitId: "u2", Title: "Next", Lessons: []*pb.LessonRef{{LessonId: "l3", Title: "Practice", Goal: "Build"}}}}}
}
func TestReplanRejectsEveryProtectedMutation(t *testing.T) {
	base := outlineFixture()
	for name, edit := range map[string]func(*pb.CourseOutline){
		"retitle": func(o *pb.CourseOutline) { o.Units[0].Lessons[0].Title = "changed" },
		"goal":    func(o *pb.CourseOutline) { o.Units[0].Lessons[0].Goal = "changed" },
		"remove":  func(o *pb.CourseOutline) { o.Units[0].Lessons = o.Units[0].Lessons[1:] },
		"move": func(o *pb.CourseOutline) {
			o.Units[0].Lessons[0], o.Units[0].Lessons[1] = o.Units[0].Lessons[1], o.Units[0].Lessons[0]
		},
		"unit title": func(o *pb.CourseOutline) { o.Units[0].Title = "changed" },
		"unit move":  func(o *pb.CourseOutline) { o.Units[0], o.Units[1] = o.Units[1], o.Units[0] },
		"course":     func(o *pb.CourseOutline) { o.Title = "changed" },
	} {
		t.Run(name, func(t *testing.T) {
			out := proto.Clone(base).(*pb.CourseOutline)
			edit(out)
			if _, err := ValidateReplan(base, out, []string{"l1"}); err == nil {
				t.Fatal("accepted protected edit")
			}
		})
	}
}
func TestReplanDiffContainsAddedRemovedMovedRetitledAndNoProtectedLesson(t *testing.T) {
	base := outlineFixture()
	out := proto.Clone(base).(*pb.CourseOutline)
	out.Units[0].Lessons = out.Units[0].Lessons[:1]
	out.Units[1].Lessons = []*pb.LessonRef{{LessonId: "l2", Title: "Go examples", Goal: "Focus"}, {LessonId: "l4", Title: "Build in Go", Goal: "Apply"}}
	diff, err := ValidateReplan(base, out, []string{"l1"})
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, d := range diff {
		kinds[d.Kind] = true
		if d.LessonID == "l1" {
			t.Fatal("protected lesson in diff")
		}
	}
	for _, k := range []string{"added", "removed", "moved", "retitled"} {
		if !kinds[k] {
			t.Fatalf("missing %s: %+v", k, diff)
		}
	}
	for name, invalid := range map[string]*pb.CourseOutline{"nil": nil, "empty": {}, "duplicate": proto.Clone(out).(*pb.CourseOutline)} {
		t.Run(name, func(t *testing.T) {
			if name == "duplicate" {
				invalid.Units[1].Lessons[1].LessonId = "l1"
			}
			if _, err := ValidateReplan(base, invalid, []string{"l1"}); err == nil {
				t.Fatal("invalid schema accepted")
			}
		})
	}
}
