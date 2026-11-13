package narration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Speech is the engine narration RPC.
type Speech interface {
	SynthesizeNarration(context.Context, *enginev1.SynthesizeNarrationRequest, ...grpc.CallOption) (*enginev1.SynthesizeNarrationResponse, error)
}

// Objects is the application object store (S3 in production, a fake in tests).
type Objects interface {
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte) error
}

// Financial is the subset of credits used for the separate audio reservation.
type Financial interface {
	ReserveAction(ctx context.Context, owner, quoteID, action string) (credits.Reservation, error)
	Claim(ctx context.Context, owner, id string) error
	Limits(ctx context.Context, owner, id, action string) (*enginev1.UsageLimits, error)
	Release(ctx context.Context, owner, id string) (credits.Reservation, error)
	Settle(ctx context.Context, owner, id string, usage []*enginev1.Usage) (credits.Reservation, error)
	SettleWith(ctx context.Context, owner, id string, usage []*enginev1.Usage, commit func(pgx.Tx, credits.Reservation) error) (credits.Reservation, error)
}

// Options wires the service. Enabled must be set explicitly; live narration defaults off.
type Options struct {
	Pool      *pgxpool.Pool
	Objects   Objects
	Speech    Speech
	Financial Financial
	Events    analytics.Events
	Logger    *slog.Logger
	// JobAttempts bounds background retries of one slide (default 3, with River backoff).
	JobAttempts int
}

// Service queues and performs per-slide synthesis.
type Service struct {
	opts   Options
	client *river.Client[pgx.Tx]
}

// New creates the service.
func New(opts Options) *Service {
	if opts.JobAttempts <= 0 {
		opts.JobAttempts = 3
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Service{opts: opts}
}

// Args is the queue payload: identifiers only, never narration text.
type Args struct {
	LearnerID string `json:"learner_id" river:"unique"`
	LessonID  string `json:"lesson_id" river:"unique"`
	SlideID   string `json:"slide_id" river:"unique"`
}

// Kind identifies the River job.
func (Args) Kind() string { return "narration_synthesize" }

// InsertOpts keeps one pending job per slide.
func (Args) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "narration", UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

type worker struct {
	river.WorkerDefaults[Args]
	service *Service
}

func (w *worker) Work(ctx context.Context, job *river.Job[Args]) error {
	final := job.Attempt >= job.MaxAttempts
	return w.service.Synthesize(ctx, job.Args.LearnerID, job.Args.LessonID, job.Args.SlideID, final)
}

// Queue starts the River client for narration jobs. Call Start on the result.
func (s *Service) Queue() (*river.Client[pgx.Tx], error) {
	if s.client != nil {
		return s.client, nil
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &worker{service: s})
	client, err := river.NewClient(riverpgxv5.New(s.opts.Pool), &river.Config{
		Logger:                 s.opts.Logger,
		Workers:                workers,
		Queues:                 map[string]river.QueueConfig{"narration": {MaxWorkers: 1}},
		LeaderElectionDisabled: true,
	})
	if err != nil {
		return nil, err
	}
	s.client = client
	return client, nil
}

// Enqueue stores each slide's text with status pending, then queues one job per slide.
// It runs only after the manifest (and so the narration text) is stored. Slides without
// text are recorded as failed(missing_text) and never reach the provider.
func (s *Service) Enqueue(ctx context.Context, owner, lessonID string) (int, error) {
	if s.client == nil {
		return 0, errors.New("narration queue not configured")
	}
	var key string
	if err := s.opts.Pool.QueryRow(ctx, `SELECT manifest_key FROM lesson_manifests WHERE lesson_id::text=$1 AND learner_id::text=$2`, lessonID, owner).Scan(&key); err != nil {
		return 0, err
	}
	raw, err := s.opts.Objects.Get(ctx, key)
	if err != nil {
		return 0, err
	}
	var manifest player.ObjectManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return 0, err
	}
	queued := 0
	for _, slide := range manifest.Slides {
		text := slide.NarrationText
		if ValidText(text) != nil {
			if _, err = s.opts.Pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,status,failure_code) VALUES($1,$2,$3,'failed','missing_text')
				ON CONFLICT(lesson_id,slide_id) DO NOTHING`, lessonID, owner, slide.ID); err != nil {
				return queued, err
			}
			continue
		}
		tag, err := s.opts.Pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,status,narration_text) VALUES($1,$2,$3,'pending',$4)
			ON CONFLICT(lesson_id,slide_id) DO NOTHING`, lessonID, owner, slide.ID, text)
		if err != nil {
			return queued, err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		if _, err = s.client.Insert(ctx, Args{LearnerID: owner, LessonID: lessonID, SlideID: slide.ID}, &river.InsertOpts{MaxAttempts: s.opts.JobAttempts}); err != nil {
			return queued, err
		}
		queued++
	}
	return queued, nil
}

type row struct {
	status, text string
}

func (s *Service) emit(owner, event string, properties map[string]any) {
	if s.opts.Events == nil {
		return
	}
	properties["source"], properties["platform"], properties["app_version"] = "api", "web", "development"
	_ = s.opts.Events.Capture(owner, event, properties)
}

// Synthesize performs one slide. A retryable provider failure returns an error (River
// backs off) until final, then the slide becomes failed and the reservation stays released.
func (s *Service) Synthesize(ctx context.Context, owner, lessonID, slideID string, final bool) error {
	var r row
	if err := s.opts.Pool.QueryRow(ctx, `SELECT status,narration_text FROM slide_narrations WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3`, lessonID, owner, slideID).Scan(&r.status, &r.text); err != nil {
		return nil // unknown or deleted slide: nothing to do
	}
	if r.status != StatusPending {
		return nil
	}
	if err := ValidText(r.text); err != nil {
		return s.finish(ctx, owner, lessonID, slideID, StatusFailed, "missing_text", nil)
	}
	started := time.Now()
	resolved, err := s.opts.Speech.SynthesizeNarration(ctx, &enginev1.SynthesizeNarrationRequest{SlideId: slideID, VoiceKey: VoiceKey, ResolveOnly: true})
	if err != nil {
		return s.retryOrFail(ctx, owner, lessonID, slideID, "engine_unavailable", final, err)
	}
	hash := ContentHash(r.text, resolved.VoiceId, resolved.ModelId, resolved.SettingsFingerprint)
	audioKey, alignmentKey := Keys(hash)
	meta := &meta{hash: hash, voiceID: resolved.VoiceId, modelID: resolved.ModelId, fingerprint: resolved.SettingsFingerprint, audioKey: audioKey, alignmentKey: alignmentKey}

	// One synthesis per key: concurrent slides with identical narration wait here.
	conn, err := s.opts.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, "narration:"+hash); err != nil {
		return err
	}
	defer func() {
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "narration:"+hash)
	}()

	var hit meta2
	err = s.opts.Pool.QueryRow(ctx, `SELECT characters,audio_bytes,duration_ms FROM slide_narrations WHERE content_hash=$1 AND status='ready' LIMIT 1`, hash).Scan(&hit.characters, &hit.bytes, &hit.duration)
	if err == nil {
		meta.characters, meta.bytes, meta.duration = hit.characters, hit.bytes, hit.duration
		if err = s.finish(ctx, owner, lessonID, slideID, StatusReady, "", meta); err != nil {
			return err
		}
		s.emit(owner, "narration_cache_hit", map[string]any{"lesson_id": lessonID, "slide_id": slideID, "model": meta.modelID})
		return nil
	}

	characters := int64(len([]rune(r.text)))
	quotes := pricing.NewQuotes(s.opts.Pool)
	quote, err := quotes.Create(ctx, owner, pricing.QuoteInput{Action: "narration", NarrationCharacters: characters, Voice: VoiceKey})
	if err != nil {
		return s.finish(ctx, owner, lessonID, slideID, StatusFailed, "quote_unavailable", nil)
	}
	if err = quotes.Accept(ctx, owner, quote.ID); err != nil {
		return s.finish(ctx, owner, lessonID, slideID, StatusFailed, "quote_unavailable", nil)
	}
	reservation, err := s.opts.Financial.ReserveAction(ctx, owner, quote.ID, "narration")
	switch {
	case errors.Is(err, credits.ErrInsufficient):
		// Text and test stay usable; narration is simply not available.
		return s.finish(ctx, owner, lessonID, slideID, StatusDisabled, "insufficient_credits", nil)
	case errors.Is(err, credits.ErrConcurrency):
		return s.retryOrFail(ctx, owner, lessonID, slideID, "concurrency_limit", final, err)
	case err != nil:
		return s.retryOrFail(ctx, owner, lessonID, slideID, "reservation_unavailable", final, err)
	}
	// release frees only this audio reservation: nothing else was reserved with it.
	release := func(claimed bool) {
		fctx := context.WithoutCancel(ctx)
		if !claimed {
			_, _ = s.opts.Financial.Release(fctx, owner, reservation.ID)
			return
		}
		_, _ = s.opts.Financial.Settle(fctx, owner, reservation.ID, []*enginev1.Usage{{Provider: "elevenlabs", Model: quote.Input.Model, Attempt: 1, Component: "audio"}})
	}
	if err = s.opts.Financial.Claim(ctx, owner, reservation.ID); err != nil {
		release(false)
		return s.retryOrFail(ctx, owner, lessonID, slideID, "claim_failed", final, err)
	}
	limits, err := s.opts.Financial.Limits(ctx, owner, reservation.ID, "narration")
	if err != nil {
		release(true)
		return s.retryOrFail(ctx, owner, lessonID, slideID, "limits_unavailable", final, err)
	}
	limits.Provider, limits.Model = "elevenlabs", quote.Input.Model
	callCtx, cancel := context.WithDeadline(ctx, reservation.DeadlineAt)
	defer cancel()
	response, err := s.opts.Speech.SynthesizeNarration(callCtx, &enginev1.SynthesizeNarrationRequest{SlideId: slideID, Text: r.text, VoiceKey: VoiceKey, Limits: limits})
	latency := latencyMS(time.Since(started))
	if err == nil {
		err = ValidateOutput(r.text, response)
	}
	if err == nil && (response.VoiceId != resolved.VoiceId || response.ModelId != resolved.ModelId || response.SettingsFingerprint != resolved.SettingsFingerprint) {
		err = ErrOutput
	}
	if err != nil {
		release(true)
		code := failureCode(err)
		s.emit(owner, "$ai_generation", aiProperties(quote, "", characters, latency, false, code))
		if retryable(err) {
			return s.retryOrFail(ctx, owner, lessonID, slideID, code, final, err)
		}
		return s.finish(ctx, owner, lessonID, slideID, StatusFailed, code, nil)
	}
	meta.characters, meta.bytes, meta.duration = int(response.Characters), int(response.AudioBytes), int(response.DurationMs)
	alignment, err := alignmentJSON(response)
	if err == nil {
		err = s.opts.Objects.Put(ctx, audioKey, response.Audio)
	}
	if err == nil {
		err = s.opts.Objects.Put(ctx, alignmentKey, alignment)
	}
	if err != nil {
		release(true)
		return s.retryOrFail(ctx, owner, lessonID, slideID, "storage_unavailable", final, err)
	}
	usage := response.Usage
	if usage == nil {
		release(true)
		return s.finish(ctx, owner, lessonID, slideID, StatusFailed, "usage_unavailable", nil)
	}
	usage.Component, usage.Delivered, usage.ArtifactId, usage.LatencyMs = "audio", true, hash, latency
	if usage.Attempt == 0 {
		usage.Attempt = 1
	}
	settled, err := s.opts.Financial.SettleWith(ctx, owner, reservation.ID, []*enginev1.Usage{usage}, func(tx pgx.Tx, _ credits.Reservation) error {
		return writeRow(ctx, tx, owner, lessonID, slideID, StatusReady, "", meta, reservation.ID)
	})
	if err != nil {
		return s.retryOrFail(ctx, owner, lessonID, slideID, "settlement_failed", final, err)
	}
	s.emit(owner, "narration_generated", map[string]any{"lesson_id": lessonID, "slide_id": slideID, "characters": characters, "voice_id": meta.voiceID, "model": meta.modelID, "latency_ms": latency})
	generation := aiProperties(quote, usage.Model, characters, latency, true, "")
	generation["pricing_version"] = settled.PricingVersion
	s.emit(owner, "$ai_generation", generation)
	return nil
}

type meta struct {
	hash, voiceID, modelID, fingerprint, audioKey, alignmentKey string
	characters, bytes, duration                                 int
}
type meta2 struct{ characters, bytes, duration int }

func aiProperties(q pricing.Quote, model string, characters int64, latency uint32, ok bool, code string) map[string]any {
	if model == "" {
		model = q.Input.Model
	}
	p := map[string]any{"feature": "narration", "provider": "elevenlabs", "model": model, "narration_characters": characters, "latency_ms": latency, "attempt": 1, "succeeded": ok, "pricing_version": q.PricingVersion}
	if code != "" {
		p["error_code"] = code
	}
	return p
}

func failureCode(err error) string {
	switch {
	case errors.Is(err, ErrAlignment):
		return "invalid_alignment"
	case errors.Is(err, ErrOutput):
		return "invalid_output"
	case errors.Is(err, ErrMissingText):
		return "missing_text"
	}
	switch status.Code(err) {
	case codes.DeadlineExceeded:
		return "timeout"
	case codes.InvalidArgument:
		return "invalid_request"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "engine_unavailable"
}

// retryable limits background retries to transient provider and storage failures.
func retryable(err error) bool {
	if errors.Is(err, ErrAlignment) || errors.Is(err, ErrOutput) || errors.Is(err, ErrMissingText) {
		return false
	}
	return status.Code(err) != codes.InvalidArgument && status.Code(err) != codes.FailedPrecondition
}

func (s *Service) retryOrFail(ctx context.Context, owner, lessonID, slideID, code string, final bool, cause error) error {
	if !final {
		return fmt.Errorf("narration %s: %w", code, cause)
	}
	return s.finish(ctx, owner, lessonID, slideID, StatusFailed, code, nil)
}

// finish records a terminal status. Failure events carry only a fixed code.
func (s *Service) finish(ctx context.Context, owner, lessonID, slideID, st, code string, m *meta) error {
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = writeRow(ctx, tx, owner, lessonID, slideID, st, code, m, ""); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if st == StatusFailed || st == StatusDisabled {
		s.emit(owner, "narration_failed", map[string]any{"lesson_id": lessonID, "slide_id": slideID, "error_code": code})
	}
	return nil
}

func writeRow(ctx context.Context, tx pgx.Tx, owner, lessonID, slideID, st, code string, m *meta, reservation string) error {
	if m == nil {
		m = &meta{}
	}
	var reservationID any
	if reservation != "" {
		reservationID = reservation
	}
	tag, err := tx.Exec(ctx, `UPDATE slide_narrations SET status=$4,failure_code=$5,content_hash=$6,voice_id=$7,model_id=$8,settings_fingerprint=$9,audio_key=$10,alignment_key=$11,
		characters=$12,audio_bytes=$13,duration_ms=$14,reservation_id=$15,updated_at=now()
		WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 AND status='pending'`,
		lessonID, owner, slideID, st, code, m.hash, m.voiceID, m.modelID, m.fingerprint, m.audioKey, m.alignmentKey, m.characters, m.bytes, m.duration, reservationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("narration row is no longer pending")
	}
	return nil
}

// UnsharedObjectKeys lists narration objects referenced only by the given Lessons, which are
// being deleted with their Course or account. Keys still referenced by any other Lesson stay.
func UnsharedObjectKeys(ctx context.Context, pool *pgxpool.Pool, lessonIDs []string) ([]string, error) {
	rows, err := pool.Query(ctx, `SELECT DISTINCT unnest(ARRAY[n.audio_key,n.alignment_key]) FROM slide_narrations n
		WHERE n.lesson_id::text = ANY($1) AND n.audio_key<>'' AND NOT EXISTS(
			SELECT 1 FROM slide_narrations o WHERE o.content_hash=n.content_hash AND NOT (o.lesson_id::text = ANY($1)))`, lessonIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func latencyMS(d time.Duration) uint32 {
	ms := d.Milliseconds()
	if ms < 0 || ms > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(ms)
}
