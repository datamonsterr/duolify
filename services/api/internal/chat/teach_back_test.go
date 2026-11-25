package chat

import (
	"testing"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/player"
)

func TestTeachBackPointsPreferLessonPlanAndMatchSlide(t *testing.T) {
	slides := []teachBackSlide{
		{position: 0, topics: []string{"orbit"}, regions: []player.Region{{Role: "explanation", Description: "Earth orbits the Sun each year."}}},
		{position: 1, topics: []string{"rotation"}, regions: []player.Region{{Role: "explanation", Description: "Earth rotates on its axis once a day."}}},
	}
	points := buildTeachBackPoints([]string{"Earth rotates on its axis", "Earth orbits the Sun"}, slides)
	if len(points) != 2 || points[0].GetText() != "Earth rotates on its axis" || points[0].GetSlideIndex() != 1 || points[1].GetSlideIndex() != 0 {
		t.Fatalf("unexpected plan points %v", points)
	}
	points = buildTeachBackPoints(nil, slides)
	if len(points) != 2 || points[0].GetId() == points[1].GetId() || points[0].GetText() != "Earth orbits the Sun each year." || points[1].GetSlideIndex() != 1 {
		t.Fatalf("unexpected topic points %v", points)
	}
}

func TestTeachBackSummaryUsesOnlyRubricIDs(t *testing.T) {
	points := []*enginev1.TeachBackPoint{{Id: "a", Text: "First point", SlideIndex: 0}, {Id: "b", Text: "Second point", SlideIndex: 2}}
	summary := summarizeTeachBack(points, &enginev1.TeachBackResponse{Complete: true, CoveredPointIds: []string{"b", "b", "injected point"}})
	if len(summary.Covered) != 1 || summary.Covered[0].Text != "Second point" || len(summary.Missed) != 1 || summary.Missed[0].SlideIndex != 0 || summary.SelfCheck {
		t.Fatalf("unexpected summary %+v", summary)
	}
	summary = summarizeTeachBack(points, &enginev1.TeachBackResponse{Complete: true, SelfCheck: true, CoveredPointIds: []string{"b"}})
	if !summary.SelfCheck || len(summary.Covered) != 0 || len(summary.Missed) != 2 {
		t.Fatalf("unexpected self-check %+v", summary)
	}
}
