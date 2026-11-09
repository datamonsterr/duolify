package courses

import (
	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestCreationExactEditedOutline(t *testing.T) {
	base := &enginev1.CourseOutline{Title: "Sky", Units: []*enginev1.Unit{{UnitId: "manual", Title: "私の edits", Lessons: []*enginev1.LessonRef{{LessonId: "added", Title: "New", Goal: "Learn"}}}}}
	request := &enginev1.GenerateCourseOutlineRequest{EditedOutline: base, OutlineRevision: 7}
	valid := &enginev1.GenerateCourseOutlineResponse{Outline: proto.Clone(base).(*enginev1.CourseOutline), OutlineRevision: 7, Attempts: []*enginev1.Usage{{Delivered: true}}}
	if ValidateCreatedOutline(request, valid) != nil {
		t.Fatal("valid outline rejected")
	}
	for _, change := range []func(*enginev1.GenerateCourseOutlineResponse){
		func(r *enginev1.GenerateCourseOutlineResponse) { r.OutlineRevision++ },
		func(r *enginev1.GenerateCourseOutlineResponse) { r.Outline.Units[0].Title = "Reverted" },
		func(r *enginev1.GenerateCourseOutlineResponse) { r.Outline.Units[0].Lessons = nil },
		func(r *enginev1.GenerateCourseOutlineResponse) { r.Outline.ProtoReflect().SetUnknown([]byte{0x7a, 0}) },
		func(r *enginev1.GenerateCourseOutlineResponse) { r.Attempts = nil },
	} {
		r := proto.Clone(valid).(*enginev1.GenerateCourseOutlineResponse)
		change(r)
		if ValidateCreatedOutline(request, r) == nil {
			t.Fatal("changed/unknown content accepted")
		}
	}
}
