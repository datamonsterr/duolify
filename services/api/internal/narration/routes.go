package narration

import (
	"context"
	"errors"
	"math"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

func (s *Service) courseLanguage(ctx context.Context, owner, lesson string) (string, error) {
	var language string
	err := s.opts.Pool.QueryRow(ctx, `SELECT COALESCE(cc.request #>> '{brief,language}','')
		FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id JOIN courses c ON c.id=u.course_id
		LEFT JOIN course_creations cc ON cc.id=c.creation_id
		WHERE l.id=$1::uuid AND l.learner_id=$2::uuid`, lesson, owner).Scan(&language)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return language, err
}

func (s *Service) resolveRoutes(ctx context.Context, slide, speaker, voice, language string, explicitVoice bool) (*enginev1.SynthesizeNarrationResponse, []*enginev1.SynthesizeNarrationResponse, error) {
	req := &enginev1.SynthesizeNarrationRequest{SlideId: slide, SegmentId: segmentOf(speaker, slide), SpeakerKey: speaker, VoiceKey: voice, Language: language, ResolveOnly: true}
	if explicitVoice {
		req.Limits = &enginev1.UsageLimits{Provider: elevenLabsProvider}
	}
	primary, err := s.opts.Speech.SynthesizeNarration(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	if !validResolution(primary) {
		return nil, nil, ErrOutput
	}
	if primary.Provider != openRouterProvider {
		return primary, nil, nil
	}
	var fallbacks []*enginev1.SynthesizeNarrationResponse
	// The Engine advertises at most one secondary OpenRouter model.
	if len(primary.FallbackModels) == 1 && primary.FallbackModels[0] != primary.ModelId {
		model := primary.FallbackModels[0]
		req.Limits = &enginev1.UsageLimits{Provider: openRouterProvider, Model: model}
		if r, e := s.opts.Speech.SynthesizeNarration(ctx, req); e == nil && validResolution(r) && r.Provider == openRouterProvider && r.ModelId == model {
			fallbacks = append(fallbacks, r)
		}
	}
	req.Limits = &enginev1.UsageLimits{Provider: elevenLabsProvider}
	if r, e := s.opts.Speech.SynthesizeNarration(ctx, req); e == nil && validResolution(r) && r.Provider != openRouterProvider {
		fallbacks = append(fallbacks, r)
	}
	return primary, fallbacks, nil
}

func validResolution(r *enginev1.SynthesizeNarrationResponse) bool {
	if r == nil || r.VoiceId == "" || r.ModelId == "" || r.SettingsFingerprint == "" {
		return false
	}
	switch r.Provider {
	case "":
		return true // pre-CF-02 engine contract
	case elevenLabsProvider:
		return r.TimingSource == "vendor"
	case openRouterProvider:
		return r.TimingSource == "estimated"
	default:
		return false
	}
}

func resolvedAsset(text string, r *enginev1.SynthesizeNarrationResponse) meta {
	m := meta{textHash: TextHash(text), voiceID: r.VoiceId, modelID: r.ModelId, fingerprint: r.SettingsFingerprint,
		provider: r.Provider, timingSource: r.TimingSource, audioFormat: r.AudioFormat}
	if m.provider == "" {
		m.provider, m.timingSource, m.legacy = elevenLabsProvider, "vendor", true
		m.hash = ContentHash(text, m.voiceID, m.modelID, m.fingerprint)
		m.audioKey, m.alignmentKey = Keys(m.hash)
	} else {
		m.hash = ProviderContentHash(text, m.provider, m.modelID, m.voiceID, m.fingerprint)
		m.audioKey, m.alignmentKey = providerKeys(m.hash, m.audioFormat)
	}
	return m
}

func (m meta) legacyHash(text string) (string, bool) {
	if m.legacy || m.provider != elevenLabsProvider {
		return "", false
	}
	return ContentHash(text, m.voiceID, m.modelID, m.fingerprint), true
}

func (s *Service) reuseCandidate(ctx context.Context, owner, lesson string, it *item, seen map[string]bool) (bool, error) {
	for i, route := range it.routes {
		var keys = []string{route.hash}
		if legacy, ok := route.legacyHash(it.text); ok {
			keys = append(keys, legacy)
		}
		for _, key := range keys {
			hit := route
			var state string
			err := s.opts.Pool.QueryRow(ctx, `SELECT state,provider,timing_source,characters,audio_bytes,duration_ms,audio_key,alignment_key
				FROM narration_assets WHERE content_hash=$1 AND provider=$2`, key, route.provider).
				Scan(&state, &hit.provider, &hit.timingSource, &hit.characters, &hit.bytes, &hit.duration, &hit.audioKey, &hit.alignmentKey)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return false, err
			}
			if state != "ready" {
				return true, s.holdForRegeneration(ctx, key, lesson, owner, it.slides)
			}
			hit.hash = key
			for _, slide := range it.slides {
				info := it.rows[slide]
				err = s.finish(ctx, owner, lesson, info.voice, slide, StatusReady, "", &hit, info)
				if errors.Is(err, errNotPending) {
					continue
				}
				if err != nil {
					return false, err
				}
				p := hit.properties(i > 0)
				p["lesson_id"], p["slide_id"] = lesson, slide
				s.emit(owner, "narration_cache_hit", s.rowProperties(p, info))
				s.noteFallback(owner, info, seen)
			}
			return true, nil
		}
	}
	return false, nil
}

func (m meta) properties(fallback bool) map[string]any {
	return map[string]any{"provider": m.provider, "model": m.modelID, "timing_source": m.timingSource, "fallback_used": fallback}
}

type providerHold struct {
	id          string
	maxAttempts uint32
	amount      int64
}

type elevenLabsDenials struct {
	used, requested int64
	items           int
	model           string
}

func (s *Service) releaseHold(ctx context.Context, provider string, hold providerHold, actual int64) {
	if hold.id == "" {
		return
	}
	ctx = context.WithoutCancel(ctx)
	if provider == openRouterProvider {
		_ = s.settleOpenRouter(ctx, hold.id, actual)
	} else {
		_ = s.settleBudget(ctx, hold.id, actual)
	}
}

// admitNext takes the first eligible route whose independent budget admits it.
// The return code is used if every route is unavailable before any learner quote.
func (s *Service) admitNext(ctx context.Context, owner string, it *item, policy pricing.QuotePolicy, start int, attempted bool, denials *elevenLabsDenials) (string, error) {
	code := "route_unavailable"
	for i := start; i < len(it.routes); i++ {
		route := it.routes[i]
		rate := policy.AudioRate(route.provider, route.modelID)
		if rate <= 0 {
			continue
		}
		hold := providerHold{maxAttempts: 1}
		var used int64
		var err error
		if route.provider == openRouterProvider {
			hold.amount = openRouterCostNanos(int64(it.characters), rate)
			hold.id, used, err = s.reserveOpenRouter(ctx, hold.amount, time.Now())
		} else {
			if !attempted {
				hold.maxAttempts = 2
			}
			if s.opts.Budget.Enabled() {
				hold.amount = int64(it.characters) * int64(hold.maxAttempts)
				hold.id, used, err = s.reserveBudget(ctx, hold.amount, time.Now())
			}
		}
		if err != nil {
			return "", err
		}
		if hold.id != "" || route.provider == elevenLabsProvider && !s.opts.Budget.Enabled() {
			it.hash, it.meta, it.route, it.hold = route.hash, route, i, hold
			return "", nil
		}
		code = CodeBudgetExhausted
		if route.provider == openRouterProvider {
			s.alertOpenRouterBudget(owner, route.modelID, used, hold.amount)
		} else if denials == nil {
			s.alertBudgetExhausted(owner, used, hold.amount, 1, route.modelID)
		} else {
			denials.used, denials.model = used, route.modelID
			denials.requested += hold.amount
			denials.items++
		}
	}
	return code, nil
}

func (s *Service) callRoute(ctx context.Context, it *item, limits *enginev1.UsageLimits) (*enginev1.SynthesizeNarrationResponse, error) {
	first := it.rows[it.slides[0]]
	route, hold := it.meta, it.hold
	it.hold = providerHold{}
	call := proto.Clone(limits).(*enginev1.UsageLimits)
	call.Provider, call.Model = route.provider, route.modelID
	if call.MaxAttempts == 0 {
		call.MaxAttempts = hold.maxAttempts
	} else {
		call.MaxAttempts = min(call.MaxAttempts, hold.maxAttempts)
	}
	if it.segment() {
		SegmentLimits(call)
	}
	r, err := s.opts.Speech.SynthesizeNarration(ctx, &enginev1.SynthesizeNarrationRequest{
		SlideId: it.slides[0], SegmentId: segmentOf(first.speaker, it.slides[0]), SpeakerKey: first.speaker,
		Text: it.text, Language: it.language, VoiceKey: it.callVoice, Limits: call,
	})
	if _, _, refused := engine.NotPromoted(err); refused {
		s.releaseHold(ctx, route.provider, hold, 0)
	}
	if err != nil {
		return nil, err
	}
	use := r.GetUsage()
	if route.provider == openRouterProvider {
		// Binary TTS normally supplies no cost. The maximum configured endpoint rate
		// is then the conservative per-character estimate and remains in the ledger.
		if use != nil && use.Provider == route.provider && use.Model == route.modelID && use.ProviderCostNanos != nil {
			reported := *use.ProviderCostNanos
			if reported > math.MaxInt64 || int64(reported) > hold.amount || use.UsageUnavailable {
				return nil, ErrOutput
			}
			s.releaseHold(ctx, route.provider, hold, int64(reported))
		}
	} else if use != nil && use.Attempt >= 1 && use.Attempt <= call.MaxAttempts {
		s.releaseHold(ctx, route.provider, hold, int64(it.characters)*int64(use.Attempt))
	}
	if err = ValidateOutput(it.text, r); err != nil {
		return nil, err
	}
	if it.segment() && (r.SpeakerKey != first.speaker || r.SegmentId != segmentOf(first.speaker, it.slides[0])) {
		return nil, ErrOutput
	}
	if r.VoiceId != route.voiceID || r.ModelId != route.modelID || r.SettingsFingerprint != route.fingerprint ||
		!route.legacy && (r.Provider != route.provider || r.TimingSource != route.timingSource) {
		return nil, ErrOutput
	}
	if use == nil || use.Provider != route.provider || use.Model != route.modelID || int64(use.NarrationCharacters) != int64(it.characters) || use.Attempt > call.MaxAttempts {
		return nil, errUsageUnavailable
	}
	if !route.legacy {
		if r.AudioFormat != "audio/mpeg" && r.AudioFormat != "audio/wav" {
			return nil, ErrOutput
		}
		it.meta.audioKey, it.meta.alignmentKey = providerKeys(it.hash, r.AudioFormat)
	}
	return r, nil
}

// dispatchItem walks the already deduplicated candidates within the same credit
// reservation and deadline. A provider reservation is committed before each call.
func (s *Service) dispatchItem(ctx context.Context, owner string, it *item, limits *enginev1.UsageLimits,
	initial pricing.QuotePolicy, quote pricing.Quote, served map[string]*item) (*enginev1.SynthesizeNarrationResponse, []*enginev1.Usage, bool, error) {
	var attempts []*enginev1.Usage
	var attemptNumber uint32
	var last error
	recordFailure := func(code string) {
		attemptNumber++
		attempts = append(attempts, it.failedAttempt(attemptNumber, code))
	}
	if initial.AudioRate(it.meta.provider, it.meta.modelID) != quote.Policy.AudioRate(it.meta.provider, it.meta.modelID) {
		// Pricing activated between pre-admission and quoting. Admit under the
		// immutable quote rather than using an amount from a different version.
		s.releaseHold(ctx, it.meta.provider, it.hold, 0)
		it.hold = providerHold{}
		code, err := s.admitNext(ctx, owner, it, quote.Policy, it.route, false, nil)
		if err != nil {
			return nil, nil, false, err
		}
		if code != "" {
			return nil, nil, false, errBudgetExhausted
		}
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, attempts, false, err
		}
		if shared := served[it.hash]; shared != nil {
			for _, slide := range it.slides {
				shared.slides = append(shared.slides, slide)
				shared.rows[slide] = it.rows[slide]
			}
			return nil, attempts, true, nil
		}
		maxCalls := it.hold.maxAttempts
		if limits.MaxAttempts > 0 {
			maxCalls = min(maxCalls, limits.MaxAttempts)
		}
		started := time.Now()
		r, err := s.callRoute(ctx, it, limits)
		it.latency = latencyMS(time.Since(started))
		if err == nil {
			use := r.Usage
			for i := uint32(1); i < use.Attempt; i++ {
				recordFailure("engine_unavailable")
			}
			attemptNumber++
			use.Component, use.Delivered, use.ArtifactId, use.LatencyMs = "audio", true, it.hash, it.latency
			use.TimingSource, use.FallbackUsed, use.Attempt = it.meta.timingSource, it.route > 0, attemptNumber
			attempts = append(attempts, use)
			return r, attempts, false, nil
		}
		last = err
		if _, _, refused := engine.NotPromoted(err); !refused {
			for i := uint32(0); i < maxCalls; i++ {
				recordFailure(failureCode(err))
			}
		}
		if it.route+1 >= len(it.routes) {
			return nil, attempts, false, last
		}
		code, err := s.admitNext(ctx, owner, it, quote.Policy, it.route+1, len(attempts) > 0, nil)
		if err != nil {
			return nil, attempts, false, err
		}
		if code != "" {
			return nil, attempts, false, last
		}
	}
}

func (it *item) failedAttempt(index uint32, code string) *enginev1.Usage {
	var characters uint32
	if it.characters > 0 && it.characters <= math.MaxUint32 {
		characters = uint32(it.characters)
	}
	return &enginev1.Usage{Provider: it.meta.provider, Model: it.meta.modelID, Component: "audio", Attempt: index,
		NarrationCharacters: characters, UsageUnavailable: true, ErrorCode: code,
		FallbackUsed: it.route > 0, TimingSource: it.meta.timingSource}
}
