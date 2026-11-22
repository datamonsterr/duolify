package player

import (
	"context"
	"errors"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

// GeneratedBackfillArgs identifies the one-off repair for pre-settlement Lessons.
type GeneratedBackfillArgs struct{}

// Kind is versioned so a future repair can have its own durable retry history.
func (GeneratedBackfillArgs) Kind() string { return "generated_lesson_publication_backfill_v1" }

type generatedBackfillWorker struct {
	river.WorkerDefaults[GeneratedBackfillArgs]
	service *Service
}

func (w *generatedBackfillWorker) Work(ctx context.Context, _ *river.Job[GeneratedBackfillArgs]) error {
	if err := w.service.BackfillGenerated(ctx); err != nil {
		return err
	}
	slog.InfoContext(ctx, "generated lesson manifest backfill completed")
	return nil
}

// GeneratedBackfillQueue processes only the one-off repair; callers enqueue it
// on worker startup. Successful manifests make every subsequent run a no-op.
func (s *Service) GeneratedBackfillQueue(logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &generatedBackfillWorker{service: s})
	return river.NewClient(riverpgxv5.New(s.opts.Pool), &river.Config{
		Logger: logger, Workers: workers,
		Queues: map[string]river.QueueConfig{"generated-publication": {MaxWorkers: 1}},
		// Only the main worker client owns periodic jobs (daily backup, credits). A secondary client that
		// wins leader election would starve them.
		LeaderElectionDisabled: true,
	})
}

// BackfillGenerated repairs missing legacy manifests in bounded pages. It uses
// the playback authorization checks and never calls providers or changes credits.
// Locked/deleted/blocked lessons stay hidden and retain the read-time fallback.
func (s *Service) BackfillGenerated(ctx context.Context) error {
	after := "00000000-0000-0000-0000-000000000000"
	var failures error
	for {
		rows, err := s.opts.Pool.Query(ctx, `SELECT g.lesson_stub_id::text,g.learner_id::text FROM generated_lessons g
 JOIN course_lesson_stubs l ON l.id=g.lesson_stub_id JOIN course_units u ON u.id=l.unit_id JOIN courses c ON c.id=u.course_id
 JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=g.learner_id
 WHERE g.lesson_stub_id>$1::uuid AND l.learner_id=g.learner_id AND c.learner_id=g.learner_id
 AND l.status<>'locked' AND c.deleted_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM course_copies cc WHERE cc.copy_course_id=c.id AND cc.blocked_at IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM lesson_manifests m WHERE m.lesson_id=g.lesson_stub_id)
 ORDER BY g.lesson_stub_id LIMIT 100`, after)
		if err != nil {
			return errors.Join(failures, err)
		}
		type pending struct{ lesson, owner string }
		var page []pending
		for rows.Next() {
			var item pending
			if err = rows.Scan(&item.lesson, &item.owner); err != nil {
				rows.Close()
				return errors.Join(failures, err)
			}
			page = append(page, item)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return errors.Join(failures, err)
		}
		if len(page) == 0 {
			return failures
		}
		for _, item := range page {
			if err = s.MaterializeGenerated(ctx, item.owner, item.lesson); err != nil {
				failures = errors.Join(failures, err)
			}
			after = item.lesson
		}
	}
}
