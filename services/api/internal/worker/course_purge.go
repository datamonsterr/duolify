package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// CourseDeletionGrace is how long a soft-deleted Course stays recoverable.
const CourseDeletionGrace = 30 * 24 * time.Hour

// CoursePurger hard-deletes Courses soft-deleted at or before the cutoff.
type CoursePurger interface {
	PurgeDeletedCourses(ctx context.Context, cutoff time.Time) (int64, error)
}

// ObjectPurger deletes the stored objects of purged Courses after their retention.
type ObjectPurger interface {
	SweepPurgedObjects(ctx context.Context, now time.Time) (int, error)
}

// CoursePurgeArgs is the content-free daily course purge job.
type CoursePurgeArgs struct{}

// Kind identifies the persisted purge job.
func (CoursePurgeArgs) Kind() string { return "course_purge" }

// InsertOpts makes the job unique per day, so concurrent workers insert it once.
func (CoursePurgeArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByPeriod: 24 * time.Hour}}
}

// CoursePurgeWorker purges Courses past the deletion grace period.
type CoursePurgeWorker struct {
	river.WorkerDefaults[CoursePurgeArgs]
	Service CoursePurger
	Logger  *slog.Logger
}

// Work purges everything deleted more than CourseDeletionGrace ago.
func (w *CoursePurgeWorker) Work(ctx context.Context, _ *river.Job[CoursePurgeArgs]) error {
	n, err := w.Service.PurgeDeletedCourses(ctx, time.Now().UTC().Add(-CourseDeletionGrace))
	if err != nil {
		return err
	}
	if n > 0 && w.Logger != nil {
		w.Logger.InfoContext(ctx, "deleted courses purged", "count", n)
	}
	if op, ok := w.Service.(ObjectPurger); ok {
		objects, err := op.SweepPurgedObjects(ctx, time.Now().UTC())
		if err != nil {
			return err
		}
		if objects > 0 && w.Logger != nil {
			w.Logger.InfoContext(ctx, "purged course objects deleted", "prefixes", objects)
		}
	}
	return nil
}

func registerCoursePurgeWorker(workers *river.Workers, service any) []*river.PeriodicJob {
	p, ok := service.(CoursePurger)
	if !ok {
		return nil
	}
	river.AddWorker(workers, &CoursePurgeWorker{Service: p})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(24*time.Hour), func() (river.JobArgs, *river.InsertOpts) { return CoursePurgeArgs{}, nil }, &river.PeriodicJobOpts{ID: "course-purge-daily", RunOnStart: true}),
	}
}
