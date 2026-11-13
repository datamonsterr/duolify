package chat

import (
	"context"
	"errors"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/jackc/pgx/v5"
)

// maxSlideIndex bounds the slide cursor; it is validated but never reaches the Engine.
const maxSlideIndex = 9999

// lessonInput is the only lesson data a client may send: a reference, never content.
type lessonInput struct {
	LessonID   string `json:"lesson_id"`
	SlideIndex int    `json:"slide_index"`
}

// valid reports whether the reference is well formed; an absent lesson is valid (Course-level question).
func (l *lessonInput) valid() bool {
	return l == nil || (validUUID(l.LessonID) && l.SlideIndex >= 0 && l.SlideIndex <= maxSlideIndex)
}

var errLessonNotFound = errors.New("lesson not found")

// currentLesson builds the Engine's current_lesson from the Learner's own rows. The lesson must belong to the
// Learner and, through its unit, to the Course in the URL; every other case is the same not-found answer.
func (s *Service) currentLesson(ctx context.Context, owner, course string, l *lessonInput) (*enginev1.Lesson, error) {
	if l == nil {
		return nil, nil
	}
	ref := &enginev1.LessonRef{}
	err := s.opts.Pool.QueryRow(ctx, `SELECT l.id::text,l.title,l.goal FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1::uuid AND l.learner_id=$2::uuid AND u.course_id=$3::uuid AND u.learner_id=$2::uuid`, l.LessonID, owner, course).Scan(&ref.LessonId, &ref.Title, &ref.Goal)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errLessonNotFound
	}
	if err != nil {
		return nil, err
	}
	return &enginev1.Lesson{Ref: ref}, nil
}
