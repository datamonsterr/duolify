package steering

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ForkStub is one lesson of a copy in reading order.
type ForkStub struct {
	ID     string
	Status string
	Reused bool
}

// ForkPlan is where a copy forks: the marker lesson, the locked lessons whose published
// content is dropped, and how many earlier lessons stay.
type ForkPlan struct {
	MarkerLessonID string
	Unreuse        []string
	KeptLessons    int
}

// PlanFork finds the first lesson not yet unlocked. Everything before it stays; reused content
// from the marker on is dropped so the learner's own generation takes over.
func PlanFork(stubs []ForkStub) ForkPlan {
	var p ForkPlan
	for _, s := range stubs {
		if p.MarkerLessonID == "" {
			if s.Status != "locked" {
				p.KeptLessons++
				continue
			}
			p.MarkerLessonID = s.ID
		}
		if s.Reused {
			p.Unreuse = append(p.Unreuse, s.ID)
		}
	}
	return p
}

// ForkInfo tells the client whether steering this course forks it from the published version.
type ForkInfo struct {
	// Required: an unforked copy; the first instruction needs explicit confirmation.
	Required bool `json:"required"`
	Forked   bool `json:"forked"`
	// KeptLessons counts the lessons already unlocked that stay as they are.
	KeptLessons int `json:"kept_lessons"`
	// LessonCredits is the per-lesson price from the active Pricing config (what a lesson quote
	// shows); absent when no active price is available.
	LessonCredits int64 `json:"lesson_credits,omitempty"`
}

// LessonPrice reads the active per-lesson price; it is never hard-coded here.
type LessonPrice interface {
	LessonCredits(context.Context) (int64, error)
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func forkStubs(ctx context.Context, q querier, course string) ([]ForkStub, error) {
	rows, err := q.Query(ctx, `SELECT l.id::text,l.status,l.reused_from_snapshot FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1::uuid AND u.archived_at IS NULL AND l.archived_at IS NULL ORDER BY u.position,l.position`, course)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ForkStub
	for rows.Next() {
		var s ForkStub
		if err = rows.Scan(&s.ID, &s.Status, &s.Reused); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// forkInfo returns nil for a course that is not a published copy.
func (s *Service) forkInfo(ctx context.Context, owner, course string) (*ForkInfo, error) {
	var forked bool
	err := s.opts.Pool.QueryRow(ctx, `SELECT forked_at IS NOT NULL FROM course_copies WHERE copy_course_id=$1::uuid AND learner_id=$2::uuid`, course, owner).Scan(&forked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info := &ForkInfo{Forked: forked, Required: !forked}
	if s.opts.Price != nil && !forked {
		if credits, perr := s.opts.Price.LessonCredits(ctx); perr == nil && credits > 0 {
			info.LessonCredits = credits
		}
	}
	stubs, err := forkStubs(ctx, s.opts.Pool, course)
	if err != nil {
		return nil, err
	}
	for _, st := range stubs {
		if st.Status != "locked" {
			info.KeptLessons++
		}
	}
	return info, nil
}

// LockCopyTx makes a lesson unlock wait for, or be waited on by, a fork of the same copy.
// A lesson that is not part of a copy locks nothing.
func LockCopyTx(ctx context.Context, tx pgx.Tx, lesson string) error {
	var locked string
	err := tx.QueryRow(ctx, `SELECT cc.copy_course_id::text FROM course_copies cc WHERE cc.copy_course_id=(SELECT u.course_id FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE l.id=$1::uuid) FOR SHARE OF cc`, lesson).Scan(&locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// forkTx forks an unforked copy under the course lock the caller holds. It returns the 1-based
// index of the marker lesson (0 when every lesson was unlocked) and whether it forked.
func (s *Service) forkTx(ctx context.Context, tx pgx.Tx, owner, course string) (index int, forked bool, err error) {
	var already bool
	err = tx.QueryRow(ctx, `SELECT forked_at IS NOT NULL FROM course_copies WHERE copy_course_id=$1::uuid AND learner_id=$2::uuid FOR UPDATE`, course, owner).Scan(&already)
	if errors.Is(err, pgx.ErrNoRows) || already {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	stubs, err := forkStubs(ctx, tx, course)
	if err != nil {
		return 0, false, err
	}
	plan := PlanFork(stubs)
	if len(plan.Unreuse) > 0 {
		for _, q := range []string{`DELETE FROM lesson_manifests WHERE lesson_id=ANY($1::uuid[])`, `DELETE FROM lesson_slides WHERE lesson_stub_id=ANY($1::uuid[])`, `DELETE FROM lesson_questions WHERE lesson_stub_id=ANY($1::uuid[])`, `DELETE FROM lesson_retry_pools WHERE lesson_stub_id=ANY($1::uuid[])`, `DELETE FROM generated_lessons WHERE lesson_stub_id=ANY($1::uuid[])`, `UPDATE course_lesson_stubs SET reused_from_snapshot=false WHERE id=ANY($1::uuid[])`} {
			if _, err = tx.Exec(ctx, q, plan.Unreuse); err != nil {
				return 0, false, err
			}
		}
	}
	var marker any
	if plan.MarkerLessonID != "" {
		marker = plan.MarkerLessonID
	}
	if _, err = tx.Exec(ctx, `UPDATE course_copies SET forked_at=now(),forked_at_lesson=$2::uuid,lock_version=lock_version+1,updated_at=now() WHERE copy_course_id=$1::uuid`, course, marker); err != nil {
		return 0, false, err
	}
	if plan.MarkerLessonID != "" {
		index = plan.KeptLessons + 1
	}
	return index, true, nil
}
