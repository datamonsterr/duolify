package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
)

type fakePersonEraser struct {
	enabled bool
	calls   int
	err     error
}

func (f *fakePersonEraser) PersonDeletionEnabled() bool { return f.enabled }
func (f *fakePersonEraser) ProcessPersonDeletions(context.Context) (int, error) {
	f.calls++
	return 1, f.err
}

func TestPersonDeletionIsScheduledOnlyWhenPostHogIsConfigured(t *testing.T) {
	if jobs := registerPersonDeletionWorker(river.NewWorkers(), &fakePersonEraser{enabled: false}, nil); len(jobs) != 0 {
		t.Fatal("scheduled without PostHog")
	}
	if jobs := registerPersonDeletionWorker(river.NewWorkers(), struct{}{}, nil); len(jobs) != 0 {
		t.Fatal("scheduled for a service without the capability")
	}
	if jobs := registerPersonDeletionWorker(river.NewWorkers(), &fakePersonEraser{enabled: true}, nil); len(jobs) != 1 {
		t.Fatal("not scheduled with PostHog")
	}
}

func TestPersonDeletionWorkerRunsTheEraserAndReportsFailure(t *testing.T) {
	eraser := &fakePersonEraser{enabled: true}
	w := &PersonDeletionWorker{Service: eraser}
	if err := w.Work(t.Context(), nil); err != nil || eraser.calls != 1 {
		t.Fatal(err, eraser.calls)
	}
	eraser.err = errors.New("1 of 1 posthog person deletions failed and stay queued")
	if err := w.Work(t.Context(), nil); err == nil {
		t.Fatal("failure not reported")
	}
	if opts := (PersonDeletionArgs{}).InsertOpts(); opts.MaxAttempts != 1 {
		t.Fatal("a failed run must wait for the next scheduled one")
	}
}
