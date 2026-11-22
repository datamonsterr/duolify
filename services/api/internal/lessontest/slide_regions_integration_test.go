//go:build integration

package lessontest_test

import (
	"strings"
	"testing"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

func titleRegion() *pb.SlideRegion {
	return &pb.SlideRegion{RegionId: "t", Box: &pb.RegionBox{X: 0.05, Y: 0.04, W: 0.9, H: 0.18}, Role: pb.RegionRole_REGION_ROLE_TITLE, Description: "Title"}
}

// Publication does not trust the Engine's regions: a Lesson whose Slides are valid but whose
// regions are not fails the job, releases the reservation and publishes nothing.
func TestPublicationRejectsInvalidSlideRegions(t *testing.T) {
	for name, regions := range map[string]func() []*pb.SlideRegion{
		"outside the stage": func() []*pb.SlideRegion {
			r := titleRegion()
			r.Box.W = 0.99
			return []*pb.SlideRegion{r}
		},
		"over the count": func() []*pb.SlideRegion {
			var out []*pb.SlideRegion
			for i := 0; i < 7; i++ {
				r := titleRegion()
				r.RegionId = string(rune('a' + i))
				r.Box = &pb.RegionBox{X: float64(i) * 0.14, Y: 0.9, W: 0.1, H: 0.05}
				out = append(out, r)
			}
			return out
		},
		"description too long": func() []*pb.SlideRegion {
			r := titleRegion()
			r.Description = strings.Repeat("x", 301)
			return []*pb.SlideRegion{r}
		},
		"markup": func() []*pb.SlideRegion {
			r := titleRegion()
			r.Description = `<img src=x onerror=alert(1)>`
			return []*pb.SlideRegion{r}
		},
		"unknown role": func() []*pb.SlideRegion {
			r := titleRegion()
			r.Role = pb.RegionRole_REGION_ROLE_UNSPECIFIED
			return []*pb.SlideRegion{r}
		},
		"missing box": func() []*pb.SlideRegion {
			r := titleRegion()
			r.Box = nil
			return []*pb.SlideRegion{r}
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newReviewFixture(t)
			ctx := t.Context()
			job := f.prepare(t)
			f.engine.lesson.Slides[0].Regions = regions()
			if err := f.service.RunLessonGeneration(ctx, job); err == nil {
				t.Fatal("invalid regions were delivered")
			}
			var reservation string
			if err := f.db.Pool.QueryRow(ctx, `SELECT j.state FROM credit_jobs j JOIN lesson_generations g ON g.reservation_id=j.id WHERE g.id=$1::uuid`, job).Scan(&reservation); err != nil || reservation != "released" {
				t.Fatalf("reservation %s %v", reservation, err)
			}
			var delivered, manifests int
			if err := f.db.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM generated_lessons WHERE lesson_stub_id=$1::uuid), (SELECT count(*) FROM lesson_manifests WHERE lesson_id=$1::uuid)`, f.lesson).Scan(&delivered, &manifests); err != nil || delivered != 0 || manifests != 0 {
				t.Fatalf("delivered=%d manifests=%d %v", delivered, manifests, err)
			}
		})
	}
}
