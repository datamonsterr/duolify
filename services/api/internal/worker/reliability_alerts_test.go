package worker

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type alerterDouble struct{ runs int }

func (a *alerterDouble) RunScheduledAlerts(context.Context) error { a.runs++; return nil }

func TestReliabilityAlertsRegisteredAsStablePeriodicJob(t *testing.T) {
	a := new(alerterDouble)
	jobs := registerReliabilityAlertWorkers(river.NewWorkers(), a)
	if len(jobs) != 1 {
		t.Fatalf("periodic jobs=%d", len(jobs))
	}
	if got := registerReliabilityAlertWorkers(river.NewWorkers(), nil); len(got) != 0 {
		t.Fatal("job registered without an alerter")
	}
	if err := (&ReliabilityAlertWorker{Service: a}).Work(t.Context(), nil); err != nil || a.runs != 1 {
		t.Fatalf("runs=%d err=%v", a.runs, err)
	}
	if ReliabilityAlertInterval != 5*time.Minute || ReliabilityAlertPeriodicID != "reliability-alerts" {
		t.Fatal("interval or id changed")
	}
}
