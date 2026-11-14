package steering

import (
	"context"
	"fmt"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/jackc/pgx/v5"
)

// PinTx runs under the same Course lock as Add and the queued job transaction.
func PinTx(ctx context.Context, tx pgx.Tx, owner, course, job string) error {
	var locked string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM courses WHERE id=$1::uuid AND learner_id=$2::uuid FOR UPDATE`, course, owner).Scan(&locked); err != nil {
		return err
	}
	marked, err := tx.Exec(ctx, `UPDATE lesson_generations SET instruction_snapshot_version=1 WHERE id=$1::uuid AND learner_id=$2::uuid AND course_id=$3::uuid AND instruction_snapshot_version=0`, job, owner, course)
	if err != nil {
		return err
	}
	if marked.RowsAffected() == 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO lesson_instruction_snapshots(generation_id,learner_id,instruction_id,text,scope,version,added_at,screening_version)
 SELECT $3::uuid,i.learner_id,i.id,i.text,i.scope,i.version,i.added_at,i.screening_version FROM course_instructions i
 WHERE i.learner_id=$1::uuid AND i.course_id=$2::uuid AND i.status='active'
 AND (i.scope='rest_of_course' OR NOT EXISTS(SELECT 1 FROM lesson_instruction_snapshots s JOIN lesson_generations g ON g.id=s.generation_id WHERE s.instruction_id=i.id AND g.state IN ('queued','generating','validated','completed')))
 ORDER BY i.added_at,i.id ON CONFLICT DO NOTHING`, owner, course, job)
	return err
}

// Querier reads pinned inputs through a pool or settlement transaction.
type Querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

// Snapshot never consults the mutable current list. Empty snapshots are valid.
func Snapshot(ctx context.Context, q Querier, owner, job string) ([]*pb.CourseInstruction, error) {
	rows, err := q.Query(ctx, `SELECT instruction_id::text,text,scope,version,(extract(epoch FROM added_at)*1000)::bigint FROM lesson_instruction_snapshots WHERE generation_id=$1::uuid AND learner_id=$2::uuid ORDER BY added_at,instruction_id`, job, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*pb.CourseInstruction
	total := 0
	for rows.Next() {
		var in pb.CourseInstruction
		var scope string
		if err = rows.Scan(&in.Id, &in.Text, &scope, &in.Version, &in.AddedAtUnixMs); err != nil {
			return nil, err
		}
		switch scope {
		case "next_lesson":
			in.Scope = pb.CourseInstruction_SCOPE_NEXT_LESSON
		case "rest_of_course":
			in.Scope = pb.CourseInstruction_SCOPE_REST_OF_COURSE
		default:
			return nil, fmt.Errorf("invalid snapshot scope")
		}
		result = append(result, &in)
		total += len([]rune(in.Text))
		if len(result) > 20 || len([]rune(in.Text)) > 500 || total > 4000 {
			return nil, fmt.Errorf("snapshot bounds exceeded")
		}
	}
	return result, rows.Err()
}

// ApplyTx consumes next_lesson only in the validated Lesson settlement transaction.
func ApplyTx(ctx context.Context, tx pgx.Tx, owner, job, lesson string) ([]string, error) {
	rows, err := tx.Query(ctx, `UPDATE course_instructions i SET status=CASE WHEN s.scope='next_lesson' THEN 'applied' ELSE i.status END,applied_lesson_id=CASE WHEN s.scope='next_lesson' THEN $3::uuid ELSE i.applied_lesson_id END FROM lesson_instruction_snapshots s WHERE s.generation_id=$1::uuid AND s.learner_id=$2::uuid AND i.id=s.instruction_id AND i.learner_id=s.learner_id AND i.status='active' RETURNING i.id::text`, job, owner, lesson)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
