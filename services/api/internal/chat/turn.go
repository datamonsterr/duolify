package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type sendInput struct {
	QuoteID        string `json:"quote_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Class          string `json:"class"`
	Message        string `json:"message"`
	ThreadID       string `json:"thread_id"`
}

func writeEvent(w http.ResponseWriter, name string, data any) {
	raw, _ := json.Marshal(data)
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
	_ = http.NewResponseController(w).Flush()
}

func startSSE(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// settlementUsage normalises Engine usage into one delivered content attempt that the E7 quote
// can price. Without reported usage (an interrupted reply) the tokens are estimated from the
// delivered bytes and clamped to the quote, so the settlement is partial but never unbounded.
func settlementUsage(q pricing.Quote, promptSize int, delivered string, reported *enginev1.Usage) []*enginev1.Usage {
	u := &enginev1.Usage{Provider: q.Rate.Provider, Model: q.Rate.Model, Component: "content", Attempt: 1, Delivered: true}
	if reported != nil {
		u.Provider, u.Model = reported.Provider, reported.Model
		u.InputTokens, u.OutputTokens = reported.InputTokens, reported.OutputTokens
		u.CacheWriteTokens, u.CacheReadTokens = reported.CacheWriteTokens, reported.CacheReadTokens
		u.LatencyMs = min(reported.LatencyMs, u32(q.Bounds["timeout_ms"]))
		u.UsageUnavailable = reported.UsageUnavailable
		u.Estimated = reported.Estimated
		u.FallbackUsed = reported.Provider != q.Rate.Provider || reported.Model != q.Rate.Model
	} else {
		u.InputTokens = u32(min(estimateTokens(promptSize), q.Bounds["input_tokens"]))
		u.OutputTokens = u32(min(estimateTokens(len(delivered)), q.Bounds["output_tokens"]))
		u.Estimated = true
	}
	return []*enginev1.Usage{u}
}

// u32 clamps a quote bound into the protobuf token range.
func u32(n int64) uint32 {
	if n <= 0 {
		return 0
	}
	if n > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(n)
}

var errTurnLost = errors.New("turn state lost")

// beginTurn inserts the idempotency/quote claim. existing is non-nil when the key already ran.
func (s *Service) beginTurn(ctx context.Context, owner, course string, in sendInput) (id string, replay *storedTurn, status int, err error) {
	for range 2 {
		err = s.opts.Pool.QueryRow(ctx, `INSERT INTO tutor_turns(learner_id,course_id,idempotency_key,quote_id) VALUES($1::uuid,$2::uuid,$3,$4::uuid) ON CONFLICT DO NOTHING RETURNING id::text`, owner, course, in.IdempotencyKey, in.QuoteID).Scan(&id)
		if err == nil {
			return id, nil, 0, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return "", nil, http.StatusServiceUnavailable, err
		}
		var t storedTurn
		err = s.opts.Pool.QueryRow(ctx, `SELECT id::text,state,COALESCE(thread_id::text,''),COALESCE(assistant_message_id::text,''),created_at FROM tutor_turns WHERE learner_id=$1::uuid AND idempotency_key=$2`, owner, in.IdempotencyKey).Scan(&t.ID, &t.State, &t.ThreadID, &t.AssistantID, &t.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			// Another key already owns this quote.
			return "", nil, http.StatusConflict, nil
		}
		if err != nil {
			return "", nil, http.StatusServiceUnavailable, err
		}
		if t.State != "pending" {
			return "", &t, 0, nil
		}
		if time.Since(t.CreatedAt) < stalePending {
			return "", nil, http.StatusConflict, nil
		}
		if _, err = s.opts.Pool.Exec(ctx, `DELETE FROM tutor_turns WHERE id=$1::uuid AND state='pending'`, t.ID); err != nil {
			return "", nil, http.StatusServiceUnavailable, err
		}
	}
	return "", nil, http.StatusConflict, nil
}

type storedTurn struct {
	ID, State, ThreadID, AssistantID string
	CreatedAt                        time.Time
}

func (s *Service) dropTurn(turn string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.opts.Pool.Exec(ctx, `DELETE FROM tutor_turns WHERE id=$1::uuid AND state='pending'`, turn); err != nil {
		s.opts.Logger.Error("tutor turn cleanup failed", "error", err)
	}
}

// tombstone turns a pending turn that delivered nothing into a text-free `released` row.
func (s *Service) tombstone(turn, reason, job string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tag, err := s.opts.Pool.Exec(ctx, `UPDATE tutor_turns SET state='released',end_reason=NULLIF($2,''),job_id=$3::uuid WHERE id=$1::uuid AND state='pending'`, turn, reason, job)
	if err != nil || tag.RowsAffected() != 1 {
		s.opts.Logger.Error("tutor turn tombstone failed", "error", err)
		return false
	}
	return true
}

func (s *Service) replay(ctx context.Context, w http.ResponseWriter, owner string, t *storedTurn) {
	if t.State == "released" {
		// Nothing was delivered or charged for this key; a deliberate resend needs a new key and quote.
		fail(w, http.StatusConflict, "released")
		return
	}
	var content, status string
	if err := s.opts.Pool.QueryRow(ctx, `SELECT content,status FROM tutor_messages WHERE id=$1::uuid AND learner_id=$2::uuid`, t.AssistantID, owner).Scan(&content, &status); err != nil {
		// The thread was deleted after the reply: nothing to replay and nothing to charge.
		fail(w, http.StatusGone, "thread_deleted")
		return
	}
	startSSE(w)
	writeEvent(w, "thread", map[string]any{"thread_id": t.ThreadID})
	writeEvent(w, "delta", map[string]any{"text": content})
	if status == "interrupted" {
		writeEvent(w, "error", map[string]any{"code": "interrupted", "interrupted": true, "message_id": t.AssistantID, "replayed": true})
		return
	}
	writeEvent(w, "done", map[string]any{"message_id": t.AssistantID, "thread_id": t.ThreadID, "replayed": true})
}

func (s *Service) send(w http.ResponseWriter, r *http.Request, owner, course string) {
	var in sendInput
	if !decode(w, r, &in) {
		return
	}
	if validateMessage(in.Class, in.Message) != nil || !validUUID(in.QuoteID) || len(in.IdempotencyKey) < 1 || len(in.IdempotencyKey) > 128 || (in.ThreadID != "" && !validUUID(in.ThreadID)) {
		fail(w, http.StatusBadRequest, "invalid_request")
		return
	}
	ctx := r.Context()
	if !s.ownsCourse(ctx, owner, course) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	thread, newThread := in.ThreadID, false
	if thread != "" {
		var ok bool
		if s.opts.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM tutor_threads WHERE id=$1::uuid AND learner_id=$2::uuid AND course_id=$3::uuid)`, thread, owner, course).Scan(&ok) != nil || !ok {
			fail(w, http.StatusNotFound, "not_found")
			return
		}
	} else {
		var err error
		if thread, err = s.latestThread(ctx, owner, course); err != nil {
			fail(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
	}
	if thread == "" {
		thread, newThread = uuid.NewString(), true
	}
	action := classAction(in.Class)
	q, err := pricing.NewQuotes(s.opts.Pool).Get(ctx, owner, in.QuoteID)
	if err != nil || q.Action != action || !q.Accepted || q.Expired {
		fail(w, http.StatusConflict, "quote_unavailable")
		return
	}
	turn, stored, status, err := s.beginTurn(ctx, owner, course, in)
	if err != nil {
		s.opts.Logger.Error("tutor turn claim failed", "error", err)
		fail(w, status, "unavailable")
		return
	}
	if stored != nil {
		s.replay(ctx, w, owner, stored)
		return
	}
	if status != 0 {
		fail(w, status, "turn_in_progress")
		return
	}
	// The flight outlives cleanup: a waiting Stop wakes only after settlement and turn cleanup are final.
	streamCtx, cancelCause := context.WithCancelCause(ctx)
	defer cancelCause(nil)
	flight := s.flights.add(owner, in.IdempotencyKey, cancelCause)
	if flight == nil {
		fail(w, http.StatusConflict, "turn_in_progress")
		return
	}
	defer s.flights.finish(owner, in.IdempotencyKey, flight)
	cleanup := true
	defer func() {
		if cleanup {
			s.dropTurn(turn)
		}
	}()
	req, err := s.assemble(ctx, owner, course, map[bool]string{false: thread, true: ""}[newThread], in.Message)
	if err != nil {
		s.opts.Logger.Error("tutor context failed", "error", err)
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	// The mandatory parts never shrink: if they do not fit the quote's bound the turn is refused, free of charge.
	standard, ceiling := q.Bounds["input_tokens"], q.Bounds["prompt_tokens"]
	if v := judge(estimateTokens(promptBytes(req)), standard, ceiling); v != fits {
		refuseContext(w, v, estimateTokens(promptBytes(req)), standard, ceiling)
		return
	}
	summaries := len(req.History)
	if req.Excerpts, err = s.retrieve(ctx, owner, course, in.Message, int(standard)*4-promptBytes(req)); err != nil {
		s.opts.Logger.Error("tutor retrieval failed", "error", err)
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	job, err := s.opts.Financial.ReserveAction(ctx, owner, in.QuoteID, action)
	if err != nil {
		if errors.Is(err, credits.ErrInsufficient) {
			respond(w, http.StatusPaymentRequired, map[string]any{"error": "insufficient_credits", "draft_kept": true})
			return
		}
		switch {
		case errors.Is(err, credits.ErrConcurrency):
			respond(w, http.StatusTooManyRequests, map[string]any{"error": "too_many_streams", "draft_kept": true})
		case errors.Is(err, credits.ErrBudget):
			respond(w, http.StatusServiceUnavailable, map[string]any{"error": "capacity_unavailable", "draft_kept": true})
		default:
			fail(w, http.StatusConflict, "quote_unavailable")
		}
		return
	}
	if job.State != "reserved" {
		fail(w, http.StatusConflict, "quote_unavailable")
		return
	}
	claimed := false
	// abort returns every credit: queued jobs are released, running jobs settle with no usage.
	abort := func() {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		var err error
		if claimed {
			_, err = s.opts.Financial.Settle(fctx, owner, job.ID, nil)
		} else {
			_, err = s.opts.Financial.Release(fctx, owner, job.ID)
		}
		if err != nil {
			s.opts.Logger.Error("tutor reservation release failed", "error", err)
		}
	}
	limits, err := s.opts.Financial.Limits(ctx, owner, job.ID, action)
	if err != nil {
		abort()
		fail(w, http.StatusConflict, "quote_unavailable")
		return
	}
	limits.FirstTokenTimeoutMs, limits.IdleTimeoutMs = firstTokenTimeoutMs, idleTimeoutMs
	req.Limits = limits
	if err = s.opts.Financial.Claim(ctx, owner, job.ID); err != nil {
		abort()
		fail(w, http.StatusServiceUnavailable, "busy")
		return
	}
	claimed = true
	s.emit(ctx, owner, "tutor_message_sent", map[string]any{"course_id": course, "class": in.Class, "message_length": len([]rune(in.Message))})
	if errors.Is(context.Cause(streamCtx), errStopped) {
		// Stopped before the Engine was called: nothing was produced.
		abort()
		cleanup = !s.tombstone(turn, "stop", job.ID)
		s.emit(context.WithoutCancel(ctx), owner, "tutor_reply_stopped", map[string]any{"course_id": course, "class": in.Class, "reason": "stop", "delivered_chars": 0, "latency_ms": 0})
		fail(w, http.StatusConflict, "stopped")
		return
	}
	started := time.Now()
	streamCtx, cancelDeadline := context.WithDeadline(streamCtx, job.DeadlineAt)
	defer cancelDeadline()
	stream, err := s.opts.Engine.Chat(streamCtx, req)
	var reply strings.Builder
	var reported *enginev1.Usage
	var streamErr, typedErr error
	sse := false
	if err != nil {
		streamErr = err
	} else {
		for {
			ev, rerr := stream.Recv()
			if errors.Is(rerr, io.EOF) {
				break
			}
			if rerr != nil {
				streamErr = rerr
				break
			}
			switch e := ev.GetEvent().(type) {
			case *enginev1.ChatEvent_Delta:
				if e.Delta == "" {
					continue
				}
				if !sse {
					startSSE(w)
					writeEvent(w, "thread", map[string]any{"thread_id": thread})
					sse = true
				}
				reply.WriteString(e.Delta)
				writeEvent(w, "delta", map[string]any{"text": e.Delta})
			case *enginev1.ChatEvent_Usage:
				reported = e.Usage
			case *enginev1.ChatEvent_ErrorCode:
				typedErr = &streamFailure{Code: e.ErrorCode, Retryable: true}
			case *enginev1.ChatEvent_StreamError:
				// Keep reading: the Engine sends its bounded usage estimate right after the typed error.
				typedErr = failureOf(e.StreamError)
			}
		}
	}
	if typedErr != nil {
		streamErr = typedErr
	}
	latency := time.Since(started).Milliseconds()
	finished := classifyEnd(streamErr, context.Cause(streamCtx), ctx.Err())
	failure := func(code string, interrupted, retryable bool) {
		props := map[string]any{"course_id": course, "class": in.Class, "error_code": code, "interrupted": interrupted, "retryable": retryable, "latency_ms": latency}
		s.emit(context.WithoutCancel(ctx), owner, "tutor_reply_failed", props)
	}
	// stopped records an explicit stop or a disconnect; engine failures use tutor_reply_failed instead.
	ended := func(delivered int) {
		if finished.Reason == "stop" || finished.Reason == "disconnect" {
			s.emit(context.WithoutCancel(ctx), owner, "tutor_reply_stopped", map[string]any{"course_id": course, "class": in.Class, "reason": finished.Reason, "delivered_chars": delivered, "latency_ms": latency})
			return
		}
		failure(finished.Reason, delivered > 0, finished.Retryable)
	}
	if reply.Len() == 0 {
		// Nothing delivered: no message, full release.
		abort()
		if finished.State == "completed" {
			finished = end{State: "interrupted", Reason: "engine_error", Retryable: true}
		}
		cleanup = !s.tombstone(turn, finished.Reason, job.ID)
		ended(0)
		switch finished.Reason {
		case "stop", "disconnect":
			fail(w, http.StatusConflict, "stopped")
		case "first_token_timeout", "idle_timeout", "deadline_exceeded", "provider_error":
			respond(w, http.StatusBadGateway, map[string]any{"error": finished.Reason, "retryable": finished.Retryable})
		default:
			respond(w, http.StatusBadGateway, map[string]any{"error": "engine_unavailable", "retryable": true})
		}
		return
	}
	// A reply cut off by a stop, a disconnect or an error is stored once as the delivered prefix.
	interrupted := finished.State != "completed"
	text := reply.String()
	usage := settlementUsage(q, promptBytes(req), text, reported)
	if interrupted {
		usage[0].ErrorCode = finished.Reason
	}
	assistantID, userID := uuid.NewString(), uuid.NewString()
	msgStatus := map[string]string{"completed": "complete", "stopped": "stopped", "interrupted": "interrupted"}[finished.State]
	commit := func(tx pgx.Tx, _ credits.Reservation) error {
		fctx := context.WithoutCancel(ctx)
		if newThread {
			if _, e := tx.Exec(fctx, `INSERT INTO tutor_threads(id,learner_id,course_id) VALUES($1::uuid,$2::uuid,$3::uuid)`, thread, owner, course); e != nil {
				return e
			}
		}
		if _, e := tx.Exec(fctx, `INSERT INTO tutor_messages(id,thread_id,learner_id,role,content) VALUES($1::uuid,$2::uuid,$3::uuid,'user',$4)`, userID, thread, owner, in.Message); e != nil {
			return e
		}
		if _, e := tx.Exec(fctx, `INSERT INTO tutor_messages(id,thread_id,learner_id,role,content,status) VALUES($1::uuid,$2::uuid,$3::uuid,'assistant',$4,$5)`, assistantID, thread, owner, text, msgStatus); e != nil {
			return e
		}
		tag, e := tx.Exec(fctx, `UPDATE tutor_turns SET state=$2,thread_id=$3::uuid,user_message_id=$4::uuid,assistant_message_id=$5::uuid,end_reason=NULLIF($6,''),job_id=$7::uuid,usage_estimated=$8 WHERE id=$1::uuid AND state='pending'`, turn, finished.State, thread, userID, assistantID, finished.Reason, job.ID, usage[0].Estimated)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return errTurnLost
		}
		return nil
	}
	finish, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancelFinish()
	_, err = s.opts.Financial.SettleWith(finish, owner, job.ID, usage, commit)
	if errors.Is(err, credits.ErrUsage) {
		// The Engine's report fell outside the quote: the reply was delivered, so settle at the quote ceiling.
		usage[0].UsageUnavailable = true
		_, err = s.opts.Financial.SettleWith(finish, owner, job.ID, usage, commit)
	}
	if err != nil {
		s.opts.Logger.Error("tutor settlement failed", "error", err)
		abort()
		failure("persist_failed", false, true)
		if sse {
			writeEvent(w, "error", map[string]any{"code": "persist_failed", "interrupted": false, "retryable": true})
		} else {
			fail(w, http.StatusServiceUnavailable, "unavailable")
		}
		return
	}
	cleanup = false
	model := q.Rate.Model
	if reported != nil && reported.Model != "" {
		model = reported.Model
	}
	if interrupted {
		ended(len([]rune(text)))
		code := finished.Reason
		if code == "engine_error" || code == "disconnect" {
			code = "interrupted"
		}
		writeEvent(w, "error", map[string]any{"code": code, "interrupted": true, "stopped": finished.State == "stopped", "retryable": finished.Retryable, "message_id": assistantID, "thread_id": thread})
		return
	}
	s.emit(context.WithoutCancel(ctx), owner, "tutor_reply_completed", map[string]any{"course_id": course, "class": in.Class, "model": model, "latency_ms": latency, "input_tokens": usage[0].InputTokens, "output_tokens": usage[0].OutputTokens,
		"envelope": in.Class, "cache_class": cacheClass(usage[0]), "cache_read_tokens": usage[0].CacheReadTokens, "cache_write_tokens": usage[0].CacheWriteTokens,
		"summary_count": summaries, "excerpt_count": len(req.Excerpts), "context_tokens": estimateTokens(promptBytes(req))})
	writeEvent(w, "done", map[string]any{"message_id": assistantID, "thread_id": thread, "replayed": false})
}
