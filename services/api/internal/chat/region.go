package chat

import (
	"context"
	"errors"
	"net/http"
	"strings"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/jackc/pgx/v5"
)

// Slides reads the authorized Slides of a Lesson with their server-side regions (implemented by player.Service).
type Slides interface {
	SlideAt(ctx context.Context, owner, lesson string, index int) (player.ObjectSlide, error)
}

// regionInput is the only region data a client may send: where it selected and when. Never Slide text.
type regionInput struct {
	Rect player.RegionBox `json:"rect"`
	AtMS int              `json:"at_ms"`
	// Input says how the question was entered; it only labels analytics. Default text.
	Input string `json:"input"`
}

var (
	errRegionInvalid     = errors.New("invalid region")
	errRegionUnavailable = errors.New("region grounding unavailable")
)

// grounding is a resolved selection: what the Engine is told and what the message stores.
type grounding struct {
	Question  *enginev1.RegionQuestion
	LessonID  string
	SlideID   string
	Rect      player.RegionBox
	RegionIDs []string
	// Kind is "regions" when declared regions matched, else "whole_slide".
	Kind  string
	Input string
}

func (g *grounding) emitted() map[string]any {
	return map[string]any{"slide_index": int(g.Question.SlideIndex), "has_region": true, "grounding": g.Kind, "input": g.Input, "lesson_id": g.LessonID}
}

// resolveRegion validates a selection and grounds it on the Slide manifest the Learner may play.
// The lesson reference has already been authorized by currentLesson. A nil region needs no grounding.
func (s *Service) resolveRegion(ctx context.Context, owner string, lesson *lessonInput, in *regionInput) (*grounding, error) {
	if in == nil {
		return nil, nil
	}
	input := in.Input
	if input == "" {
		input = "text"
	}
	if lesson == nil || !in.Rect.InsideStage() || in.AtMS < 0 || (input != "text" && input != "voice") {
		return nil, errRegionInvalid
	}
	if s.opts.Slides == nil {
		return nil, errRegionUnavailable
	}
	slide, err := s.opts.Slides.SlideAt(ctx, owner, lesson.LessonID, lesson.SlideIndex)
	switch {
	case errors.Is(err, player.ErrSlideNotFound):
		return nil, errRegionInvalid
	case errors.Is(err, pgx.ErrNoRows):
		return nil, errLessonNotFound
	case err != nil:
		return nil, errRegionUnavailable
	}
	if in.AtMS > slide.DurationMS {
		return nil, errRegionInvalid
	}
	explanation := slide.Explanation
	if explanation == "" {
		explanation = slide.NarrationText
	}
	g := &grounding{
		Question: &enginev1.RegionQuestion{
			SlideIndex:       uint32(lesson.SlideIndex), //nolint:gosec // bounded by maxSlideIndex
			Rect:             &enginev1.RegionBox{X: in.Rect.X, Y: in.Rect.Y, W: in.Rect.W, H: in.Rect.H},
			AtMs:             uint32(in.AtMS), //nolint:gosec // 0..slide duration
			SlideExplanation: explanation,
		},
		LessonID: lesson.LessonID,
		SlideID:  slide.ID,
		Rect:     in.Rect,
		Kind:     "regions",
		Input:    input,
	}
	matches := player.RegionsAt(slide, in.Rect, in.AtMS)
	g.RegionIDs = []string{}
	for _, m := range matches {
		g.RegionIDs = append(g.RegionIDs, m.Region.ID)
		g.Question.Regions = append(g.Question.Regions, &enginev1.QuestionRegion{
			RegionId:    m.Region.ID,
			Role:        regionRole(m.Region.Role),
			Description: m.Region.Description,
			Overlap:     m.Overlap,
		})
	}
	if len(matches) == 0 {
		g.Kind, g.Question.WholeSlide = "whole_slide", true
	}
	return g, nil
}

func regionRole(role string) enginev1.RegionRole {
	return enginev1.RegionRole(enginev1.RegionRole_value["REGION_ROLE_"+strings.ToUpper(role)])
}

// regionFailure answers a refused selection; nothing has been reserved, stored or sent to the Engine.
func regionFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRegionInvalid):
		fail(w, http.StatusBadRequest, "invalid_request")
	case errors.Is(err, errLessonNotFound):
		fail(w, http.StatusNotFound, "not_found")
	default:
		fail(w, http.StatusServiceUnavailable, "unavailable")
	}
}

// The anchor columns of tutor_messages, with the placeholders that follow the fixed ones of each insert.
const (
	anchorColumns = "anchor_lesson_id,anchor_slide_index,anchor_slide_id,anchor_at_ms,anchor_x,anchor_y,anchor_w,anchor_h,anchor_region_ids,anchor_grounding"
	anchorValues  = "$5::uuid,$6,$7,$8,$9,$10,$11,$12,$13,$14"
	anchorValues6 = "$6::uuid,$7,$8,$9,$10,$11,$12,$13,$14,$15"
)

// anchorArgs are the tutor_messages anchor columns, in column order; all null without a selection.
func (g *grounding) anchorArgs() []any {
	if g == nil {
		return []any{nil, nil, nil, nil, nil, nil, nil, nil, []string{}, nil}
	}
	return []any{g.LessonID, int(g.Question.SlideIndex), g.SlideID, int(g.Question.AtMs), g.Rect.X, g.Rect.Y, g.Rect.W, g.Rect.H, g.RegionIDs, g.Kind}
}

// anchorDTO is the stored selection returned with thread messages (ids and numbers only).
type anchorDTO struct {
	LessonID   string   `json:"lesson_id"`
	SlideIndex int      `json:"slide_index"`
	SlideID    string   `json:"slide_id"`
	AtMS       int      `json:"at_ms"`
	Rect       rectDTO  `json:"rect"`
	RegionIDs  []string `json:"region_ids"`
	Grounding  string   `json:"grounding"`
}

type rectDTO struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}
