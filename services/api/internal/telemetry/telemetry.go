// Package telemetry emits fixed-schema JSON spans and independent operational metrics.
// Payloads, URLs, error messages and learner identities are never span attributes.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Correlation is the only context persisted to River or forwarded to the engine.
type Correlation struct {
	RequestID    string `json:"request_id"`
	TraceID      string `json:"trace_id"`
	SpanID       string `json:"span_id"`
	ParentSpanID string `json:"-"`
}
type correlationKey struct{}
type monitorKey struct{}

func randomHex(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic("secure randomness unavailable")
	}
	return hex.EncodeToString(b)
}
func validHex(value string, size int) bool {
	if len(value) != size || strings.ToLower(value) != value || strings.Trim(value, "0") == "" {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
func validID(value string) bool {
	if len(value) != 26 {
		return false
	}
	for _, c := range value {
		if (c < 'A' || c > 'Z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// Root creates a trusted, opaque server correlation ID; HTTP input is not trusted.
func Root(ctx context.Context) context.Context {
	return context.WithValue(ctx, correlationKey{}, Correlation{RequestID: rand.Text(), TraceID: randomHex(16), SpanID: randomHex(8)})
}

// CorrelationFrom returns safe correlation only, never arbitrary request metadata.
func CorrelationFrom(ctx context.Context) Correlation {
	c, _ := ctx.Value(correlationKey{}).(Correlation)
	return c
}

// Restore validates durable metadata; older jobs and malformed records get a new root.
func Restore(ctx context.Context, c Correlation) context.Context {
	if !validID(c.RequestID) || !validHex(c.TraceID, 32) || !validHex(c.SpanID, 16) {
		return Root(ctx)
	}
	c.ParentSpanID = ""
	return context.WithValue(ctx, correlationKey{}, c)
}

// Traceparent is the W3C wire context. No baggage or learner data is propagated.
func (c Correlation) Traceparent() string { return "00-" + c.TraceID + "-" + c.SpanID + "-01" }

type counts struct {
	calls, errors uint64
	seconds       float64
}

// Monitor bounds metric cardinality and incident memory. Its state is disposable.
type Monitor struct {
	logger     *slog.Logger
	mu         sync.Mutex
	operations map[string]counts
	alerts     map[string]uint64
	incidents  map[string]time.Time
}

// New creates counters that work even with disabled or unreachable analytics.
func New(logger *slog.Logger) *Monitor {
	if logger == nil {
		logger = slog.Default()
	}
	return &Monitor{logger: logger, operations: map[string]counts{}, alerts: map[string]uint64{}, incidents: map[string]time.Time{}}
}

// FromContext shares the monitor of the originating action across internal calls.
func FromContext(ctx context.Context) *Monitor {
	if m, ok := ctx.Value(monitorKey{}).(*Monitor); ok {
		return m
	}
	return New(slog.Default())
}

// Start opens a child span and returns an end function recording only safe fields.
func (m *Monitor) Start(ctx context.Context, operation string) (context.Context, func(bool)) {
	switch operation {
	case "http", "grpc", "job", "enqueue", "dependency":
	default:
		operation = "other"
	}
	c := CorrelationFrom(ctx)
	if c.RequestID == "" {
		ctx = Root(ctx)
		c = CorrelationFrom(ctx)
	} else {
		c.ParentSpanID = c.SpanID
		c.SpanID = randomHex(8)
		ctx = context.WithValue(ctx, correlationKey{}, c)
	}
	ctx = context.WithValue(ctx, monitorKey{}, m)
	started := time.Now()
	var once sync.Once
	return ctx, func(ok bool) {
		once.Do(func() {
			elapsed := time.Since(started)
			m.mu.Lock()
			v := m.operations[operation]
			v.calls++
			if !ok {
				v.errors++
			}
			v.seconds += elapsed.Seconds()
			m.operations[operation] = v
			m.mu.Unlock()
			m.logger.InfoContext(ctx, "span", "request_id", c.RequestID, "trace_id", c.TraceID, "span_id", c.SpanID, "parent_span_id", c.ParentSpanID, "operation", operation, "duration_ms", elapsed.Milliseconds(), "ok", ok)
		})
	}
}

// Incident raises one alert until recovery. Keys never enter metrics or logs.
// Terminal job keys expire after 24h; a hard cap bounds memory during outages.
func (m *Monitor) Incident(ctx context.Context, kind, key string, healthy bool) {
	switch kind {
	case "backup", "queue", "dependency":
	default:
		return
	}
	if len(key) > 128 {
		return
	}
	incident := kind + ":" + key
	now := time.Now()
	m.mu.Lock()
	if healthy {
		delete(m.incidents, incident)
		m.mu.Unlock()
		return
	}
	if until, exists := m.incidents[incident]; exists && (until.IsZero() || now.Before(until)) {
		m.mu.Unlock()
		return
	}
	if len(m.incidents) >= 4096 {
		for k, until := range m.incidents {
			if !until.IsZero() && now.After(until) {
				delete(m.incidents, k)
			}
		}
		if len(m.incidents) >= 4096 {
			// Evict the oldest terminal job, never a continuing dependency outage.
			var oldest string
			var expiry time.Time
			for k, until := range m.incidents {
				if !until.IsZero() && (oldest == "" || until.Before(expiry)) {
					oldest = k
					expiry = until
				}
			}
			if oldest == "" {
				m.mu.Unlock()
				return
			}
			delete(m.incidents, oldest)
		}
	}
	until := time.Time{}
	if kind == "backup" || strings.HasPrefix(key, "job-") {
		until = now.Add(24 * time.Hour)
	}
	m.incidents[incident] = until
	m.alerts[kind]++
	m.mu.Unlock()
	c := CorrelationFrom(ctx)
	m.logger.ErrorContext(ctx, "operational alert", "kind", kind, "request_id", c.RequestID, "trace_id", c.TraceID)
}

// Attempt links a durable job and its retry number to a safe span, without args.
func (m *Monitor) Attempt(ctx context.Context, id int64, attempt, maxAttempts int) {
	c := CorrelationFrom(ctx)
	m.logger.InfoContext(ctx, "worker attempt", "request_id", c.RequestID, "trace_id", c.TraceID, "span_id", c.SpanID, "job_id", id, "attempt", attempt, "max_attempts", maxAttempts)
}

// ServeHTTP exports Prometheus text on a separate private operational listener.
func (m *Monitor) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	m.mu.Lock()
	alerts := make(map[string]uint64, len(m.alerts))
	for kind, count := range m.alerts {
		alerts[kind] = count
	}
	operations := make(map[string]counts, len(m.operations))
	for op, count := range m.operations {
		operations[op] = count
	}
	m.mu.Unlock()
	_, _ = fmt.Fprintln(w, "# TYPE duolify_alerts_total counter")
	for _, kind := range []string{"backup", "queue", "dependency"} {
		_, _ = fmt.Fprintf(w, "duolify_alerts_total{kind=%q} %d\n", kind, alerts[kind])
	}
	keys := make([]string, 0, len(operations))
	for op := range operations {
		keys = append(keys, op)
	}
	sort.Strings(keys)
	for _, op := range keys {
		v := operations[op]
		_, _ = fmt.Fprintf(w, "duolify_operations_total{operation=%q,ok=\"true\"} %d\nduolify_operations_total{operation=%q,ok=\"false\"} %d\nduolify_duration_seconds_sum{operation=%q} %g\nduolify_duration_seconds_count{operation=%q} %d\n", op, v.calls-v.errors, op, v.errors, op, v.seconds, op, v.calls)
	}
}
