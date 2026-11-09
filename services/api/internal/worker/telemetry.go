package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/datamonsterr/duolify/services/api/internal/telemetry"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// Telemetry propagates correlation through durable metadata, independent of args uniqueness.
type Telemetry struct {
	river.MiddlewareDefaults
	monitor *telemetry.Monitor
}

// NewTelemetry instruments all jobs, including jobs without a Learner.
func NewTelemetry(monitor *telemetry.Monitor) *Telemetry { return &Telemetry{monitor: monitor} }

// InsertMany preserves existing metadata and persists only safe correlation fields.
func (m *Telemetry) InsertMany(ctx context.Context, params []*rivertype.JobInsertParams, inner func(context.Context) ([]*rivertype.JobInsertResult, error)) (result []*rivertype.JobInsertResult, err error) {
	correlation := telemetry.CorrelationFrom(ctx)
	ctx, end := m.monitor.Start(ctx, "enqueue")
	defer func() { end(err == nil) }()
	if correlation.RequestID == "" {
		correlation = telemetry.CorrelationFrom(ctx)
	}
	for _, p := range params {
		meta := map[string]json.RawMessage{}
		if len(p.Metadata) > 0 {
			if err := json.Unmarshal(p.Metadata, &meta); err != nil {
				return nil, errors.New("invalid job metadata")
			}
		}
		if meta == nil {
			meta = map[string]json.RawMessage{}
		}
		meta["correlation"], _ = json.Marshal(correlation)
		p.Metadata, _ = json.Marshal(meta)
	}
	result, err = inner(ctx)
	if ctx.Err() == nil {
		m.monitor.Incident(ctx, "queue", "database", err == nil)
	}
	return result, err
}

// Work creates a new span for each attempt and sanitizes errors before River persists them.
func (m *Telemetry) Work(ctx context.Context, job *rivertype.JobRow, inner func(context.Context) error) (err error) {
	var meta struct {
		Correlation telemetry.Correlation `json:"correlation"`
	}
	_ = json.Unmarshal(job.Metadata, &meta)
	ctx = telemetry.Restore(ctx, meta.Correlation)
	ctx, end := m.monitor.Start(ctx, "job")
	m.monitor.Attempt(ctx, job.ID, job.Attempt, job.MaxAttempts)
	defer func() {
		if recover() != nil {
			err = errors.New("worker attempt panicked")
		}
		var snooze *river.JobSnoozeError
		var canceled *river.JobCancelError
		if errors.As(err, &snooze) {
			err = river.JobSnooze(snooze.Duration)
			end(true)
			return
		}
		if errors.As(err, &canceled) {
			err = river.JobCancel(errors.New("worker canceled"))
			end(true)
			return
		}
		if ctx.Err() == context.Canceled {
			err = context.Canceled
			end(false)
			return
		}
		end(err == nil)
		if err != nil {
			if job.Attempt >= job.MaxAttempts {
				kind := "queue"
				if job.Kind == "database_backup" {
					kind = "backup"
				}
				m.monitor.Incident(ctx, kind, "job-"+strconv.FormatInt(job.ID, 10), false)
			}
			err = errors.New("worker attempt failed")
		}
	}()
	return inner(ctx)
}

var _ rivertype.JobInsertMiddleware = (*Telemetry)(nil)
var _ rivertype.WorkerMiddleware = (*Telemetry)(nil)
