package chat

import (
	"context"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"google.golang.org/protobuf/proto"
)

// instructionBytes approximates the Engine's fixed instruction text; the JSON data block is
// larger than its protobuf encoding, so the proto size is inflated before budgeting.
const instructionBytes = 1500

func promptBytes(req *enginev1.ChatRequest) int { return proto.Size(req)*3/2 + instructionBytes }

// verdict says whether the mandatory prompt parts fit a bound.
type verdict int

const (
	fits verdict = iota
	needsApproval
	tooLarge
)

// judge compares the mandatory prompt tokens (outline, every summary, profile, recent messages, the question)
// with the quote's input bound and the configured prompt ceiling. Nothing is ever trimmed to make it fit.
func judge(mandatory, standard, ceiling int64) verdict {
	switch {
	case mandatory <= standard:
		return fits
	case mandatory <= ceiling:
		return needsApproval
	}
	return tooLarge
}

// cacheClass names how a provider used its prompt cache: cold (only written), warm (read) or none.
func cacheClass(u *enginev1.Usage) string {
	switch {
	case u.GetCacheReadTokens() > 0:
		return "warm"
	case u.GetCacheWriteTokens() > 0:
		return "cold"
	}
	return "none"
}

// assemble builds the Engine context from rows owned by the authenticated learner only:
// every query is keyed by learner and course. current_lesson is empty at Course level and holds only the verified lesson reference otherwise.
// It holds the mandatory parts only: every summary is kept, whatever the size. Retrieval fills the remaining room.
func (s *Service) assemble(ctx context.Context, owner, course, thread, message string, current *enginev1.Lesson) (*enginev1.ChatRequest, error) {
	req := &enginev1.ChatRequest{Outline: &enginev1.CourseOutline{CourseId: course}, CurrentLesson: current}
	if err := s.opts.Pool.QueryRow(ctx, `SELECT title,description FROM courses WHERE id=$1::uuid AND learner_id=$2::uuid`, course, owner).Scan(&req.Outline.Title, &req.Outline.Description); err != nil {
		return nil, err
	}
	rows, err := s.opts.Pool.Query(ctx, `SELECT u.id::text,u.title,COALESCE(l.id::text,''),COALESCE(l.title,''),COALESCE(l.goal,'') FROM course_units u LEFT JOIN course_lesson_stubs l ON l.unit_id=u.id AND l.learner_id=$2::uuid WHERE u.course_id=$1::uuid AND u.learner_id=$2::uuid ORDER BY u.position,l.position`, course, owner)
	if err != nil {
		return nil, err
	}
	var unit *enginev1.Unit
	for rows.Next() {
		var uid, utitle, lid, ltitle, lgoal string
		if err = rows.Scan(&uid, &utitle, &lid, &ltitle, &lgoal); err != nil {
			rows.Close()
			return nil, err
		}
		if unit == nil || unit.UnitId != uid {
			unit = &enginev1.Unit{UnitId: uid, Title: utitle}
			req.Outline.Units = append(req.Outline.Units, unit)
		}
		if lid != "" {
			unit.Lessons = append(unit.Lessons, &enginev1.LessonRef{LessonId: lid, Title: ltitle, Goal: lgoal})
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if req.Learner, err = s.opts.Profiles.EffectiveProfile(ctx, owner, course); err != nil {
		return nil, err
	}
	hrows, err := s.opts.Pool.Query(ctx, `SELECT l.title||': '||s.summary FROM lesson_completion_summaries s JOIN course_lesson_stubs l ON l.id=s.lesson_id WHERE s.learner_id=$1::uuid AND s.course_id=$2::uuid ORDER BY s.completed_at ASC,s.id ASC`, owner, course)
	if err != nil {
		return nil, err
	}
	for hrows.Next() {
		var h string
		if err = hrows.Scan(&h); err != nil {
			hrows.Close()
			return nil, err
		}
		req.History = append(req.History, h)
	}
	hrows.Close()
	if err = hrows.Err(); err != nil {
		return nil, err
	}
	if thread != "" {
		mrows, err := s.opts.Pool.Query(ctx, `SELECT role,content FROM (SELECT seq,role,content FROM tutor_messages WHERE thread_id=$1::uuid AND learner_id=$2::uuid ORDER BY seq DESC LIMIT $3) recent ORDER BY seq`, thread, owner, recentMessages)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			m := &enginev1.ChatMessage{}
			if err = mrows.Scan(&m.Role, &m.Content); err != nil {
				mrows.Close()
				return nil, err
			}
			req.Messages = append(req.Messages, m)
		}
		mrows.Close()
		if err = mrows.Err(); err != nil {
			return nil, err
		}
	}
	req.Messages = append(req.Messages, &enginev1.ChatMessage{Role: "user", Content: message})
	return req, nil
}
