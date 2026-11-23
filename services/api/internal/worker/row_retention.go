package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
)

// RowRetainer purges idempotency rows past their retention window.
type RowRetainer interface {
	PurgeExpiredRows(ctx context.Context) (RowRetentionResult, error)
	SweepInterval() time.Duration
}

// RowRetentionResult counts deleted rows per table.
type RowRetentionResult = struct{ PlaybackSyncEvents, AnswerOperations, GuidedChatDrafts int64 }

// RowRetentionArgs is the content-free periodic retention job.
type RowRetentionArgs struct{}

// Kind identifies the persisted job.
func (RowRetentionArgs) Kind() string { return "row_retention" }

// RowRetentionWorker runs one purge. River's per-period uniqueness keeps replicas to one run.
type RowRetentionWorker struct {
	river.WorkerDefaults[RowRetentionArgs]
	Service RowRetainer
	Logger  *slog.Logger
}

// Work deletes expired rows in batches; a failure is retried by River.
func (w *RowRetentionWorker) Work(ctx context.Context, _ *river.Job[RowRetentionArgs]) error {
	res, err := w.Service.PurgeExpiredRows(ctx)
	if w.Logger != nil {
		w.Logger.InfoContext(ctx, "row retention sweep", "playback_sync_events", res.PlaybackSyncEvents, "lesson_test_answer_operations", res.AnswerOperations, "guided_chat_drafts", res.GuidedChatDrafts, "ok", err == nil)
	}
	return err
}

func registerRowRetentionWorker(workers *river.Workers, service any, logger *slog.Logger) []*river.PeriodicJob {
	r, ok := service.(RowRetainer)
	if !ok {
		return nil
	}
	interval := r.SweepInterval()
	river.AddWorker(workers, &RowRetentionWorker{Service: r, Logger: logger})
	return []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(interval), func() (river.JobArgs, *river.InsertOpts) {
			return RowRetentionArgs{}, &river.InsertOpts{MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByPeriod: interval}}
		}, &river.PeriodicJobOpts{ID: "row-retention", RunOnStart: true}),
	}
}
