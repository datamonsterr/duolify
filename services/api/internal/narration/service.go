package narration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
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
	// Retention is how long an asset is kept after its last reference ends (default 30 days).
	Retention time.Duration
	// Entitlements verifies the funded plan; curated voices need its benefit (E7-07).
	Entitlements Entitlements
	// FixtureVoices admits the test-only voices of the voice config. Production leaves it false.
	FixtureVoices bool
	// Off is the server kill switch: no synthesis work, quotes or charges at all.
	Off bool
}

// Service queues and performs per-slide synthesis.
type Service struct {
	opts   Options
	client *river.Client[pgx.Tx]
	voices VoiceSet
}

// New creates the service.
func New(opts Options) *Service {
	if opts.JobAttempts <= 0 {
		opts.JobAttempts = 3
	}
	if opts.Retention <= 0 {
		opts.Retention = DefaultRetention
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	voices, err := LoadVoices(opts.FixtureVoices)
	if err != nil {
		panic("narration: invalid embedded voice config: " + err.Error())
	}
	return &Service{opts: opts, voices: voices}
}

// Args is the queue payload: identifiers only, never narration text. An empty SlideID
// synthesizes every pending slide of the lesson under one aggregate reservation.
type Args struct {
	LearnerID string `json:"learner_id" river:"unique"`
	LessonID  string `json:"lesson_id" river:"unique"`
	SlideID   string `json:"slide_id,omitempty" river:"unique"`
	// VoiceKey is the logical voice; empty means the default voice.
	VoiceKey string `json:"voice_key,omitempty" river:"unique"`
	// ConsentQuote is the Learner's accepted voice-change quote; the run is charged against it.
	ConsentQuote string `json:"consent_quote,omitempty"`
}

// Kind identifies the River job.
func (Args) Kind() string { return "narration_synthesize" }

// InsertOpts keeps one open job per lesson or slide; finished jobs do not block a retry.
func (Args) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: "narration", UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
		rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning, rivertype.JobStateRetryable, rivertype.JobStateScheduled}}}
}

type worker struct {
	river.WorkerDefaults[Args]
	service *Service
}

func (w *worker) Work(ctx context.Context, job *river.Job[Args]) error {
	final := job.Attempt >= job.MaxAttempts
	voice := job.Args.VoiceKey
	if voice == "" {
		voice = VoiceKey
	}
	var only []string
	if job.Args.SlideID != "" {
		only = []string{job.Args.SlideID}
	}
	return w.service.synthesize(ctx, job.Args.LearnerID, job.Args.LessonID, voice, only, job.Args.ConsentQuote, final)
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

// Enqueue stores each slide's text with status pending, then queues one job for the lesson.
// It runs only after the manifest (and so the narration text) is stored. Slides without
// text are recorded as failed(missing_text) and never reach the provider.
func (s *Service) Enqueue(ctx context.Context, owner, lessonID string) (int, error) {
	if s.client == nil {
		return 0, errors.New("narration queue not configured")
	}
	on, voice := s.preference(ctx, owner)
	if s.opts.Off || !on {
		return 0, nil // narration is off: no rows, no work, no charge
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
			if _, err = s.opts.Pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,voice_key,status,failure_code) VALUES($1,$2,$3,$4,'failed','missing_text')
				ON CONFLICT(lesson_id,slide_id,voice_key) DO NOTHING`, lessonID, owner, slide.ID, voice); err != nil {
				return queued, err
			}
			continue
		}
		tag, err := s.opts.Pool.Exec(ctx, `INSERT INTO slide_narrations(lesson_id,learner_id,slide_id,voice_key,status,narration_text) VALUES($1,$2,$3,$4,'pending',$5)
			ON CONFLICT(lesson_id,slide_id,voice_key) DO NOTHING`, lessonID, owner, slide.ID, voice, text)
		if err != nil {
			return queued, err
		}
		if tag.RowsAffected() == 0 {
			continue
		}
		queued++
	}
	if queued > 0 {
		// One job per lesson: its new characters are reserved, priced and settled together.
		if _, err = s.client.Insert(ctx, Args{LearnerID: owner, LessonID: lessonID, VoiceKey: voice}, &river.InsertOpts{MaxAttempts: s.opts.JobAttempts}); err != nil {
			return queued, err
		}
	}
	return queued, nil
}

func (s *Service) emit(owner, event string, properties map[string]any) {
	if s.opts.Events == nil {
		return
	}
	properties["source"], properties["platform"], properties["app_version"] = "api", "web", "development"
	_ = s.opts.Events.Capture(owner, event, properties)
}

// item is one distinct narration (content hash) of a lesson run and the slides that share it.
type item struct {
	hash, text string
	slides     []string
	characters int
	meta       meta
	usage      *enginev1.Usage
	latency    uint32
}

// Synthesize performs one slide. A retryable provider failure returns an error (River
// backs off) until final, then the slide becomes failed and the reservation stays released.
func (s *Service) Synthesize(ctx context.Context, owner, lessonID, slideID string, final bool) error {
	return s.synthesize(ctx, owner, lessonID, VoiceKey, []string{slideID}, "", final)
}

// SynthesizeLesson performs every pending slide of a lesson under one audio reservation, so
// the newly synthesized characters are priced and rounded up once.
func (s *Service) SynthesizeLesson(ctx context.Context, owner, lessonID string, final bool) error {
	return s.synthesize(ctx, owner, lessonID, VoiceKey, nil, "", final)
}

// SynthesizeVoice performs every pending slide of a lesson in one approved voice. A non-default
// voice needs the Learner's accepted quote (consent) and a verified entitlement.
func (s *Service) SynthesizeVoice(ctx context.Context, owner, lessonID, voice, consentQuote string, final bool) error {
	return s.synthesize(ctx, owner, lessonID, voice, nil, consentQuote, final)
}

func (s *Service) synthesize(ctx context.Context, owner, lessonID, voice string, only []string, consent string, final bool) error {
	rows, err := s.opts.Pool.Query(ctx, `SELECT slide_id,narration_text FROM slide_narrations
		WHERE lesson_id::text=$1 AND learner_id::text=$2 AND voice_key=$4 AND status='pending' AND failure_code='' AND (COALESCE(cardinality($3::text[]),0)=0 OR slide_id=ANY($3))
		ORDER BY created_at, slide_id`, lessonID, owner, only, voice)
	if err != nil {
		return err
	}
	type pendingSlide struct{ id, text string }
	var pending []pendingSlide
	for rows.Next() {
		var p pendingSlide
		if err = rows.Scan(&p.id, &p.text); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, p)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil // unknown, deleted or already finished: nothing to do
	}
	// Global narration-off (server flag or Learner setting) and an unapproved or unentitled voice
	// skip all work: no engine call, no quote, no reservation, no charge.
	if code := s.skipCode(ctx, owner, voice); code != "" {
		for _, p := range pending {
			if err = s.skip(ctx, owner, lessonID, voice, p.id, code); err != nil {
				return err
			}
		}
		return nil
	}
	resolved, err := s.opts.Speech.SynthesizeNarration(ctx, &enginev1.SynthesizeNarrationRequest{SlideId: pending[0].id, VoiceKey: voice, ResolveOnly: true})
	if err != nil {
		var first error
		for _, p := range pending {
			if e := s.retryOrFail(ctx, owner, lessonID, voice, p.id, "engine_unavailable", final, err); e != nil && first == nil {
				first = e
			}
		}
		return first
	}
	// The hash is computed from the normalized text and the resolved config before any provider call.
	items := map[string]*item{}
	var order []string
	for _, p := range pending {
		text := NormalizeText(p.text)
		if ValidText(text) != nil {
			if err = s.finish(ctx, owner, lessonID, voice, p.id, StatusFailed, "missing_text", nil); err != nil {
				return err
			}
			continue
		}
		hash := ContentHash(text, resolved.VoiceId, resolved.ModelId, resolved.SettingsFingerprint)
		it := items[hash]
		if it == nil {
			audioKey, alignmentKey := Keys(hash)
			it = &item{hash: hash, text: text, characters: len([]rune(text)),
				meta: meta{hash: hash, textHash: TextHash(text), voiceID: resolved.VoiceId, modelID: resolved.ModelId, fingerprint: resolved.SettingsFingerprint, audioKey: audioKey, alignmentKey: alignmentKey}}
			items[hash] = it
			order = append(order, hash)
		}
		it.slides = append(it.slides, p.id)
	}
	if len(order) == 0 {
		return nil
	}
	// One synthesis per key: locks are taken in hash order on one connection, so concurrent runs
	// with identical narration wait here instead of calling the provider again.
	sort.Strings(order)
	conn, err := s.opts.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	for _, hash := range order {
		if _, err = conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1,0))`, "narration:"+hash); err != nil {
			return err
		}
		defer func() {
			_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(hashtextextended($1,0))`, "narration:"+hash)
		}()
	}
	var fresh []*item
	for _, hash := range order {
		it := items[hash]
		var state string
		var hit meta
		err = s.opts.Pool.QueryRow(ctx, `SELECT state,characters,audio_bytes,duration_ms FROM narration_assets WHERE content_hash=$1`, hash).Scan(&state, &hit.characters, &hit.bytes, &hit.duration)
		switch {
		case err == nil && state == "ready":
			it.meta.characters, it.meta.bytes, it.meta.duration = hit.characters, hit.bytes, hit.duration
			for _, slide := range it.slides {
				if err = s.finish(ctx, owner, lessonID, voice, slide, StatusReady, "", &it.meta); errors.Is(err, errNotPending) {
					continue
				} else if err != nil {
					return err
				}
				s.emit(owner, "narration_cache_hit", map[string]any{"lesson_id": lessonID, "slide_id": slide, "model": it.meta.modelID})
			}
		case err == nil:
			// The shared object is gone: recovery is tracked and the Learner is never charged.
			if err = s.holdForRegeneration(ctx, it.hash, lessonID, owner, it.slides); err != nil {
				return err
			}
		default:
			fresh = append(fresh, it)
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	return s.synthesizeFresh(ctx, owner, lessonID, voice, consent, fresh, final)
}

// holdForRegeneration parks slides whose shared asset has no object until the operator queue restores it.
func (s *Service) holdForRegeneration(ctx context.Context, hash, lessonID, owner string, slides []string) error {
	for _, slide := range slides {
		if _, err := s.opts.Pool.Exec(ctx, `UPDATE slide_narrations SET failure_code='object_missing',content_hash=$4,updated_at=now() WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 AND status='pending'`, lessonID, owner, slide, hash); err != nil {
			return err
		}
	}
	return s.requestRegeneration(ctx, s.opts.Pool, hash)
}

type execer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (s *Service) requestRegeneration(ctx context.Context, db execer, hash string) error {
	_, err := db.Exec(ctx, `INSERT INTO narration_regenerations(content_hash) VALUES($1) ON CONFLICT (content_hash) WHERE status='requested' DO NOTHING`, hash)
	return err
}

func (s *Service) synthesizeFresh(ctx context.Context, owner, lessonID, voice, consent string, fresh []*item, final bool) error {
	failAll := func(st, code string) error {
		for _, it := range fresh {
			for _, slide := range it.slides {
				if err := s.finish(ctx, owner, lessonID, voice, slide, st, code, nil); err != nil {
					return err
				}
			}
		}
		return nil
	}
	retryAll := func(code string, cause error) error {
		var first error
		for _, it := range fresh {
			for _, slide := range it.slides {
				if e := s.retryOrFail(ctx, owner, lessonID, voice, slide, code, final, cause); e != nil && first == nil {
					first = e
				}
			}
		}
		return first
	}
	characters := int64(0)
	for _, it := range fresh {
		characters += int64(it.characters)
	}
	quotes := pricing.NewQuotes(s.opts.Pool).WithApprovedVoices(s.voices.Keys())
	var quote pricing.Quote
	var err error
	if consent != "" {
		// The Learner's explicit consent bounds the run: it must still be valid and cover the characters.
		quote, err = quotes.Get(ctx, owner, consent)
		if err != nil || !quote.Accepted || quote.Expired || quote.Input.Voice != voice || quote.Input.NarrationCharacters < characters {
			return failAll(StatusDisabled, "quote_expired")
		}
	} else {
		if quote, err = quotes.Create(ctx, owner, pricing.QuoteInput{Action: "narration", NarrationCharacters: characters, Voice: voice}); err != nil {
			return failAll(StatusFailed, "quote_unavailable")
		}
		if err = quotes.Accept(ctx, owner, quote.ID); err != nil {
			return failAll(StatusFailed, "quote_unavailable")
		}
	}
	reservation, err := s.opts.Financial.ReserveAction(ctx, owner, quote.ID, "narration")
	switch {
	case errors.Is(err, credits.ErrInsufficient):
		// Text and test stay usable; narration is simply not available.
		if consent != "" {
			// A voice change is not retried silently: the Learner reviews the quote again.
			return failAll(StatusDisabled, "voice_insufficient_credits")
		}
		return failAll(StatusDisabled, "insufficient_credits")
	case errors.Is(err, credits.ErrConcurrency):
		return retryAll("concurrency_limit", err)
	case err != nil:
		return retryAll("reservation_unavailable", err)
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
		return retryAll("claim_failed", err)
	}
	limits, err := s.opts.Financial.Limits(ctx, owner, reservation.ID, "narration")
	if err != nil {
		release(true)
		return retryAll("limits_unavailable", err)
	}
	limits.Provider, limits.Model = "elevenlabs", quote.Input.Model
	callCtx, cancel := context.WithDeadline(ctx, reservation.DeadlineAt)
	defer cancel()

	var delivered []*item
	var retryErr error
	for _, it := range fresh {
		callStarted := time.Now()
		response, err := s.opts.Speech.SynthesizeNarration(callCtx, &enginev1.SynthesizeNarrationRequest{SlideId: it.slides[0], Text: it.text, VoiceKey: voice, Limits: limits})
		it.latency = latencyMS(time.Since(callStarted))
		if err == nil {
			err = ValidateOutput(it.text, response)
		}
		if err == nil && (response.VoiceId != it.meta.voiceID || response.ModelId != it.meta.modelID || response.SettingsFingerprint != it.meta.fingerprint) {
			err = ErrOutput
		}
		if err == nil {
			err = s.store(ctx, it, response)
		}
		if err == nil && response.Usage == nil {
			err = errUsageUnavailable
		}
		if err != nil {
			code := failureCode(err)
			if errors.Is(err, errStorage) {
				code = "storage_unavailable"
			} else if errors.Is(err, errUsageUnavailable) {
				code = "usage_unavailable"
			}
			s.emit(owner, "$ai_generation", aiProperties(quote, "", int64(it.characters), it.latency, false, code))
			if retryable(err) && !errors.Is(err, errUsageUnavailable) {
				for _, slide := range it.slides {
					if e := s.retryOrFail(ctx, owner, lessonID, voice, slide, code, final, err); e != nil && retryErr == nil {
						retryErr = e
					}
				}
				continue
			}
			for _, slide := range it.slides {
				if e := s.finish(ctx, owner, lessonID, voice, slide, StatusFailed, code, nil); e != nil {
					return e
				}
			}
			continue
		}
		usage := response.Usage
		usage.Component, usage.Delivered, usage.ArtifactId, usage.LatencyMs = "audio", true, it.hash, it.latency
		if usage.Attempt == 0 {
			usage.Attempt = 1
		}
		it.usage = usage
		it.meta.characters, it.meta.bytes, it.meta.duration = int(response.Characters), int(response.AudioBytes), int(response.DurationMs)
		delivered = append(delivered, it)
	}
	if len(delivered) == 0 {
		// Nothing was delivered: the audio reservation is released and nothing is charged.
		release(true)
		return retryErr
	}
	usages := make([]*enginev1.Usage, 0, len(delivered))
	for _, it := range delivered {
		usages = append(usages, it.usage)
	}
	// Aggregate settlement: delivered characters are summed and rounded up once.
	settled, err := s.opts.Financial.SettleWith(ctx, owner, reservation.ID, usages, func(tx pgx.Tx, _ credits.Reservation) error {
		for _, it := range delivered {
			if _, e := tx.Exec(ctx, `INSERT INTO narration_assets(content_hash,text_hash,voice_id,model_id,settings_fingerprint,audio_key,alignment_key,characters,audio_bytes,duration_ms)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT (content_hash) DO UPDATE SET state='ready',updated_at=now()`,
				it.hash, it.meta.textHash, it.meta.voiceID, it.meta.modelID, it.meta.fingerprint, it.meta.audioKey, it.meta.alignmentKey, it.meta.characters, it.meta.bytes, it.meta.duration); e != nil {
				return e
			}
			for _, slide := range it.slides {
				if e := writeRow(ctx, tx, owner, lessonID, voice, slide, StatusReady, "", &it.meta, reservation.ID); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if err != nil {
		var first error
		for _, it := range delivered {
			for _, slide := range it.slides {
				if e := s.retryOrFail(ctx, owner, lessonID, voice, slide, "settlement_failed", final, err); e != nil && first == nil {
					first = e
				}
			}
		}
		return first
	}
	for _, it := range delivered {
		for _, slide := range it.slides {
			s.emit(owner, "narration_generated", map[string]any{"lesson_id": lessonID, "slide_id": slide, "characters": it.characters, "voice_id": it.meta.voiceID, "model": it.meta.modelID, "latency_ms": it.latency})
		}
		generation := aiProperties(quote, it.usage.Model, int64(it.characters), it.latency, true, "")
		generation["pricing_version"] = settled.PricingVersion
		s.emit(owner, "$ai_generation", generation)
		// Further slides sharing the hash in this run reuse the object at no charge.
		for _, slide := range it.slides[1:] {
			s.emit(owner, "narration_cache_hit", map[string]any{"lesson_id": lessonID, "slide_id": slide, "model": it.meta.modelID})
		}
	}
	return retryErr
}

var (
	// errNotPending means a concurrent run already finished the slide.
	errNotPending       = errors.New("narration row is no longer pending")
	errStorage          = errors.New("narration storage unavailable")
	errUsageUnavailable = errors.New("narration usage unavailable")
)

// store writes the content-addressed audio and alignment objects (immutable per hash).
func (s *Service) store(ctx context.Context, it *item, r *enginev1.SynthesizeNarrationResponse) error {
	alignment, err := alignmentJSON(r)
	if err == nil {
		err = s.opts.Objects.Put(ctx, it.meta.audioKey, r.Audio)
	}
	if err == nil {
		err = s.opts.Objects.Put(ctx, it.meta.alignmentKey, alignment)
	}
	if err != nil {
		return fmt.Errorf("%w: %v", errStorage, err)
	}
	return nil
}

type meta struct {
	hash, textHash, voiceID, modelID, fingerprint, audioKey, alignmentKey string
	characters, bytes, duration                                           int
}

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
	case codes.ResourceExhausted:
		return "rate_limited"
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

func (s *Service) retryOrFail(ctx context.Context, owner, lessonID, voice, slideID, code string, final bool, cause error) error {
	if !final {
		return fmt.Errorf("narration %s: %w", code, cause)
	}
	return s.finish(ctx, owner, lessonID, voice, slideID, StatusFailed, code, nil)
}

// finish records a terminal status. Failure events carry only a fixed code.
func (s *Service) finish(ctx context.Context, owner, lessonID, voice, slideID, st, code string, m *meta) error {
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	if err = writeRow(ctx, tx, owner, lessonID, voice, slideID, st, code, m, ""); err != nil {
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

func writeRow(ctx context.Context, tx pgx.Tx, owner, lessonID, voice, slideID, st, code string, m *meta, reservation string) error {
	if m == nil {
		m = &meta{}
	}
	var reservationID any
	if reservation != "" {
		reservationID = reservation
	}
	tag, err := tx.Exec(ctx, `UPDATE slide_narrations SET status=$4,failure_code=$5,content_hash=$6,voice_id=$7,model_id=$8,settings_fingerprint=$9,audio_key=$10,alignment_key=$11,
		characters=$12,audio_bytes=$13,duration_ms=$14,reservation_id=$15,updated_at=now()
		WHERE lesson_id::text=$1 AND learner_id::text=$2 AND slide_id=$3 AND voice_key=$16 AND status='pending'`,
		lessonID, owner, slideID, st, code, m.hash, m.voiceID, m.modelID, m.fingerprint, m.audioKey, m.alignmentKey, m.characters, m.bytes, m.duration, reservationID, voice)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errNotPending
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
