package courses_test

import (
	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/courses"
	"testing"
)

func TestValidatePreview(t *testing.T) {
	good := func() *enginev1.PreviewCourseResponse {
		return &enginev1.PreviewCourseResponse{SampleOutline: &enginev1.CourseOutline{Title: "Sample", Units: []*enginev1.Unit{{Title: "Unit", Lessons: []*enginev1.LessonRef{{Title: "Lesson", Goal: "Learn"}}}}}, ExampleSlides: []*enginev1.Slide{{SlideId: "slide", Html: `<div id="root">Example</div>`, Narration: "Example", DurationMs: 1000}}}
	}
	if courses.ValidatePreview(good()) != nil {
		t.Fatal("valid rejected")
	}
	for _, mutate := range []func(*enginev1.PreviewCourseResponse){func(r *enginev1.PreviewCourseResponse) { r.SampleOutline.Title = " " }, func(r *enginev1.PreviewCourseResponse) { r.SampleOutline.Units[0].Lessons[0].Goal = "" }, func(r *enginev1.PreviewCourseResponse) { r.ExampleSlides = nil }, func(r *enginev1.PreviewCourseResponse) {
		r.ExampleSlides[0].Html = `<iframe src="https://evil.test"></iframe>`
	}, func(r *enginev1.PreviewCourseResponse) { r.ExampleSlides[0].Narration = "" }} {
		r := good()
		mutate(r)
		if courses.ValidatePreview(r) == nil {
			t.Fatal("invalid accepted")
		}
	}
	if courses.ValidatePreview(nil) == nil {
		t.Fatal("nil accepted")
	}
}
