package characters

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/narration"
	"github.com/jackc/pgx/v5"
)

// PlanSegments turns one Slide's dialogue into narration segments in their original order. The
// voice key comes only from the speaker's pinned spec; a speaker that is not in the pinned Cast,
// or text that is not plain, never reaches synthesis (captions are unaffected).
func PlanSegments(cast LessonCastSnapshot, slideID string, segments []SlideSegment) []narration.Segment {
	out := make([]narration.Segment, 0, len(segments))
	for i, segment := range segments {
		speaker, ok := cast.Speakers[segment.SpeakerKey]
		if !ok || i >= narration.MaxSegmentsPerSlide || !PlainSegmentText(segment.Text) {
			continue
		}
		out = append(out, narration.Segment{SlideID: slideID, Index: i, SpeakerKey: segment.SpeakerKey, VoiceKey: strings.ToLower(speaker.Spec.VoiceKey), Text: segment.Text})
	}
	return out
}

// NarrationSegments lists a generated Lesson's segments from its job-pinned Cast and stored
// dialogue. It never resolves current Characters, so later edits affect only new Lessons.
func (s *Service) NarrationSegments(ctx context.Context, owner, lessonID string) ([]narration.Segment, error) {
	if s.pool == nil {
		return nil, errors.New("characters: no database")
	}
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT c.snapshot FROM generated_lessons g JOIN lesson_cast_snapshots c ON c.course_id=g.course_id AND c.learner_id=g.learner_id AND c.lesson_key=g.manifest->>'cast_key'
		WHERE g.lesson_stub_id::text=$1 AND g.learner_id::text=$2`, lessonID, owner).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil // a plain Lesson keeps slide-level narration
	}
	if err != nil {
		return nil, err
	}
	var cast LessonCastSnapshot
	if err = json.Unmarshal(raw, &cast); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT slide_id,segments FROM lesson_slides WHERE lesson_stub_id::text=$1 AND learner_id::text=$2 ORDER BY position`, lessonID, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []narration.Segment
	for rows.Next() {
		var slide string
		var stored []byte
		if err = rows.Scan(&slide, &stored); err != nil {
			return nil, err
		}
		var segments []SlideSegment
		if json.Unmarshal(stored, &segments) != nil {
			continue
		}
		out = append(out, PlanSegments(cast, slide, segments)...)
	}
	return out, rows.Err()
}
