package chat

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
)

// errStopped is the cancellation cause of an explicit Stop, so it is never mistaken for a disconnect.
var errStopped = errors.New("reply stopped by learner")

// streamFailure is a typed Engine stream error (first_token_timeout, idle_timeout, ...).
type streamFailure struct {
	Code      string
	Retryable bool
}

func (e *streamFailure) Error() string { return e.Code }

func failureOf(e *enginev1.ChatStreamError) error {
	code := e.GetCode()
	if code == "" {
		code = "provider_error"
	}
	return &streamFailure{Code: code, Retryable: e.GetRetryable()}
}

// end is how one turn finished. State is completed, stopped or interrupted.
type end struct {
	State, Reason string
	Retryable     bool
}

// classifyEnd decides the single outcome of a turn. A stream that ended cleanly is completed even when a
// stop or disconnect arrives later; an explicit stop outranks every other cause.
func classifyEnd(streamErr, cause, reqErr error) end {
	if streamErr == nil {
		return end{State: "completed"}
	}
	if errors.Is(cause, errStopped) {
		return end{State: "stopped", Reason: "stop"}
	}
	var typed *streamFailure
	if errors.As(streamErr, &typed) {
		return end{State: "interrupted", Reason: typed.Code, Retryable: typed.Retryable}
	}
	if reqErr != nil {
		return end{State: "interrupted", Reason: "disconnect", Retryable: true}
	}
	if errors.Is(cause, context.DeadlineExceeded) {
		return end{State: "interrupted", Reason: "deadline_exceeded"}
	}
	return end{State: "interrupted", Reason: "engine_error", Retryable: true}
}

// flight is one in-flight reply. done closes once its settlement is final.
type flight struct {
	cancel context.CancelCauseFunc
	done   chan struct{}
	once   sync.Once
}

// flights is the per-process registry of in-flight replies, keyed by the owning Learner and the
// idempotency key, so one Learner can never reach another's stream.
type flights struct {
	mu sync.Mutex
	m  map[string]*flight
}

func flightKey(owner, key string) string { return owner + "\x00" + key }

// add registers a reply; nil means that key is already streaming.
func (f *flights) add(owner, key string, cancel context.CancelCauseFunc) *flight {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.m == nil {
		f.m = map[string]*flight{}
	}
	if _, live := f.m[flightKey(owner, key)]; live {
		return nil
	}
	fl := &flight{cancel: cancel, done: make(chan struct{})}
	f.m[flightKey(owner, key)] = fl
	return fl
}

// stop cancels the owner's live reply (repeat calls are harmless) and returns it, or nil when none lives.
func (f *flights) stop(owner, key string) *flight {
	f.mu.Lock()
	fl := f.m[flightKey(owner, key)]
	f.mu.Unlock()
	if fl != nil {
		fl.cancel(errStopped)
	}
	return fl
}

func (f *flights) finish(owner, key string, fl *flight) {
	f.mu.Lock()
	if f.m[flightKey(owner, key)] == fl {
		delete(f.m, flightKey(owner, key))
	}
	f.mu.Unlock()
	fl.once.Do(func() { close(fl.done) })
}

type turnView struct {
	State, EndReason, ThreadID string
	MessageID, Content, Status string
}

func (s *Service) readTurn(ctx context.Context, owner, course, key string) (*turnView, error) {
	var v turnView
	err := s.opts.Pool.QueryRow(ctx, `SELECT t.state,COALESCE(t.end_reason,''),COALESCE(t.thread_id::text,''),COALESCE(m.id::text,''),COALESCE(m.content,''),COALESCE(m.status,'')
FROM tutor_turns t LEFT JOIN tutor_messages m ON m.id=t.assistant_message_id AND m.learner_id=t.learner_id
WHERE t.learner_id=$1::uuid AND t.course_id=$2::uuid AND t.idempotency_key=$3`, owner, course, key).Scan(&v.State, &v.EndReason, &v.ThreadID, &v.MessageID, &v.Content, &v.Status)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func validKey(key string) bool { return len(key) >= 1 && len(key) <= 128 }

// turn returns the owner's stored turn, including the partial reply after a dropped connection.
func (s *Service) turn(w http.ResponseWriter, r *http.Request, owner, course, key string) {
	if !validKey(key) || !s.ownsCourse(r.Context(), owner, course) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	v, err := s.readTurn(r.Context(), owner, course, key)
	if err != nil {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	var message any
	if v.MessageID != "" {
		message = map[string]any{"id": v.MessageID, "content": v.Content, "status": v.Status}
	}
	respond(w, http.StatusOK, map[string]any{"state": v.State, "end_reason": v.EndReason, "thread_id": v.ThreadID, "message": message})
}

// stop ends the owner's in-flight reply. It is idempotent: once the turn has settled it only reports
// the stored state and never charges or settles again.
func (s *Service) stop(w http.ResponseWriter, r *http.Request, owner, course, key string) {
	if !validKey(key) || !s.ownsCourse(r.Context(), owner, course) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	fl := s.flights.stop(owner, key)
	if fl != nil {
		select {
		case <-fl.done:
		case <-time.After(15 * time.Second):
		case <-r.Context().Done():
		}
	}
	v, err := s.readTurn(context.WithoutCancel(r.Context()), owner, course, key)
	switch {
	case err == nil && v.State == "pending":
		// Settlement is still running here, or the stream lives on another instance whose client connection
		// is the cancellation path.
		respond(w, http.StatusAccepted, map[string]any{"state": "pending", "stopped": false})
	case err == nil:
		respond(w, http.StatusOK, map[string]any{"state": v.State, "status": v.Status, "stopped": fl != nil && (v.State == "stopped" || v.State == "released"), "message_id": v.MessageID})
	case fl != nil:
		// Nothing was delivered: no message was stored and the reservation was released in full.
		respond(w, http.StatusOK, map[string]any{"state": "released", "stopped": true, "message_id": ""})
	default:
		fail(w, http.StatusNotFound, "not_found")
	}
}
