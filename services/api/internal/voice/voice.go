// Package voice serves POST /v1/voice/transcriptions: one short recording in, a transcript out.
//
// The recording lives in memory for the length of the request. It is read from the multipart stream
// without temp files, is never logged, queued, stored or sent to analytics, and is dropped when the
// handler returns. Only the transcript leaves, and only as the response: the Tutor chat message or
// Quick note it becomes is created by the client through the normal endpoints.
//
// Every limit (size, duration, container, entitlement, rate, daily cap, pricing) rejects before any
// Engine call.
package voice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Limits of one recording (spec: voice-input, "Transcription service").
const (
	MaxDurationMs = 30_000
	MinDurationMs = 500
	// MaxAudioBytes is 1 MB. At the 32 kbps the client requests, 30 s is about 120 KB.
	MaxAudioBytes = 1_000_000
	// maxBodyBytes covers the audio plus the multipart framing and the three small fields.
	maxBodyBytes = MaxAudioBytes + 16<<10
	// durationSlackMs is how far a measured length may exceed the declared one before it is a lie
	// (encoder padding and the final partial cluster).
	durationSlackMs = 750
	// DefaultRatePerMinute is the per-Learner burst limit.
	DefaultRatePerMinute = 6
	// engineTimeout bounds the Engine call: two 8 s model attempts plus margin.
	engineTimeout = 20 * time.Second
	// transcribeDomain is the ErrorInfo domain of the Engine's typed transcription failures.
	transcribeDomain = "engine.duolify.transcribe"
)

// Engine is the one Engine call the endpoint makes.
type Engine interface {
	Transcribe(context.Context, *enginev1.TranscribeRequest) (*enginev1.TranscribeResponse, error)
}

// Pricing exposes the active pricing version, whose `transcribe` rate gates the feature.
type Pricing interface{ Config() pricing.Config }

// Lessons answers whether a Learner may use a Lesson (it is theirs).
type Lessons interface {
	Entitled(ctx context.Context, learner, lesson string) (bool, error)
}

// Days answers which local day a Learner is in at an instant and when it ends.
type Days interface {
	LocalDay(ctx context.Context, learner string, at time.Time) (day string, resets time.Time, err error)
}

// Counters are the Redis-backed limit counters. Keys always carry an expiry.
type Counters interface {
	// Incr adds one to key, sets ttl when the key is new, and returns the count and remaining ttl.
	Incr(ctx context.Context, key string, ttl time.Duration) (count int64, remaining time.Duration, err error)
	// Decr gives one back (a failed request does not spend the daily cap).
	Decr(ctx context.Context, key string) error
}

// Options wires the service.
type Options struct {
	Engine   Engine
	Pricing  Pricing
	Lessons  Lessons
	Counters Counters
	// Days resolves the Learner's local day for the daily cap. Nil falls back to UTC days.
	Days          Days
	Events        analytics.Events
	Logger        *slog.Logger
	Now           func() time.Time
	RatePerMinute int
}

// Service is the transcription endpoint.
type Service struct{ opts Options }

// New returns the service; Engine, Pricing, Lessons and Counters are required.
func New(o Options) *Service {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.RatePerMinute <= 0 {
		o.RatePerMinute = DefaultRatePerMinute
	}
	return &Service{opts: o}
}

type response struct {
	Text         string `json:"text"`
	Language     string `json:"language"`
	DurationMs   int    `json:"duration_ms"`
	ServedModel  string `json:"served_model"`
	FallbackUsed bool   `json:"fallback_used"`
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, code string, extra map[string]any) {
	body := map[string]any{"error": code}
	for k, v := range extra {
		body[k] = v
	}
	respond(w, status, body)
}

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	versionExpr = regexp.MustCompile(`^[A-Za-z0-9_./:+-]{1,100}$`)
)

// request is one parsed upload. audio is memory only.
type request struct {
	audio      []byte
	purpose    string
	lessonID   string
	durationMs int
}

// DurationBucket is the closed bucket analytics and logs use instead of a length.
func DurationBucket(ms int) string {
	switch {
	case ms < 2000:
		return "lt_2s"
	case ms < 5000:
		return "2_5s"
	case ms < 15000:
		return "5_15s"
	default:
		return "15_30s"
	}
}

// ServeHTTP handles POST /v1/voice/transcriptions for an authenticated Learner.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		fail(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
		return
	}
	learner := accounts.LearnerID(r.Context())
	if learner == "" {
		fail(w, http.StatusUnauthorized, "sign_in_required", nil)
		return
	}
	// Voice needs an owner-approved transcription rate, free to the Learner (no Credits path exists).
	policy := s.policy()
	if policy == nil {
		fail(w, http.StatusServiceUnavailable, "voice_unavailable", nil)
		return
	}
	if r.ContentLength > maxBodyBytes {
		fail(w, http.StatusRequestEntityTooLarge, "voice_too_large", nil)
		return
	}
	now := s.opts.Now().UTC()
	// Burst limit first: it protects the parser too.
	count, remaining, err := s.opts.Counters.Incr(r.Context(), "voice:rate:"+learner, time.Minute)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "voice limit counter failed", "event", "voice_counter_failed")
		fail(w, http.StatusServiceUnavailable, "voice_unavailable", nil)
		return
	}
	if count > int64(s.opts.RatePerMinute) {
		w.Header().Set("Retry-After", strconv.Itoa(int(max(remaining, time.Second)/time.Second)))
		fail(w, http.StatusTooManyRequests, "voice_rate_limited", map[string]any{"retry_after_seconds": int(max(remaining, time.Second) / time.Second)})
		return
	}

	req, code, status := readRequest(w, r)
	if code != "" {
		fail(w, status, code, nil)
		return
	}
	container, ok := sniff(req.audio)
	if !ok {
		fail(w, http.StatusUnsupportedMediaType, "voice_unsupported_container", nil)
		return
	}
	if measured, known := measuredDurationMs(container, req.audio); known && measured > float64(req.durationMs+durationSlackMs) {
		code, status := "voice_too_long", http.StatusBadRequest
		if measured <= MaxDurationMs+durationSlackMs {
			// Within the limit but longer than declared: the declared length is what is billed against.
			code, status = "voice_invalid", http.StatusBadRequest
		}
		fail(w, status, code, nil)
		return
	}
	entitled, err := s.opts.Lessons.Entitled(r.Context(), learner, req.lessonID)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "voice lesson check failed", "event", "voice_lesson_check_failed")
		fail(w, http.StatusServiceUnavailable, "voice_unavailable", nil)
		return
	}
	if !entitled {
		fail(w, http.StatusForbidden, "voice_not_entitled", nil)
		return
	}

	// Daily cap, counted per Learner local day; the key expires with the day.
	day, resets := now.Format("20060102"), time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	if s.opts.Days != nil {
		d, next, err := s.opts.Days.LocalDay(r.Context(), learner, now)
		if err != nil {
			s.opts.Logger.ErrorContext(r.Context(), "voice local day failed", "event", "voice_counter_failed")
			fail(w, http.StatusServiceUnavailable, "voice_unavailable", nil)
			return
		}
		day, resets = strings.ReplaceAll(d, "-", ""), next.UTC()
	}
	dayKey := "voice:day:" + learner + ":" + day
	used, _, err := s.opts.Counters.Incr(r.Context(), dayKey, resets.Sub(now)+time.Hour)
	if err != nil {
		s.opts.Logger.ErrorContext(r.Context(), "voice limit counter failed", "event", "voice_counter_failed")
		fail(w, http.StatusServiceUnavailable, "voice_unavailable", nil)
		return
	}
	if used > policy.DailyRequests {
		_ = s.opts.Counters.Decr(context.WithoutCancel(r.Context()), dayKey)
		fail(w, http.StatusTooManyRequests, "voice_daily_cap", map[string]any{"resets_at": resets.Format(time.RFC3339), "daily_limit": policy.DailyRequests})
		return
	}

	meta := s.metadata(r, learner, req, container)
	meta.emit("transcription_requested", map[string]any{})
	ctx, cancel := context.WithTimeout(r.Context(), engineTimeout)
	defer cancel()
	started := s.opts.Now()
	out, err := s.opts.Engine.Transcribe(ctx, &enginev1.TranscribeRequest{
		Audio:      req.audio,
		Container:  string(container),
		DurationMs: uint32(req.durationMs), //nolint:gosec // validated to 500..30000 above
		Purpose:    req.purpose,
	})
	latency := int(s.opts.Now().Sub(started) / time.Millisecond)
	if err != nil {
		httpStatus, code, class := classify(err)
		// Only failures known to happen before any provider call give the slot back; a timeout, cost
		// ceiling, cancel or provider failure may already have been billed.
		if class == "voice_invalid" || class == "voice_unavailable" {
			_ = s.opts.Counters.Decr(context.WithoutCancel(r.Context()), dayKey)
		}
		meta.failed(class, code, latency)
		fail(w, httpStatus, code, nil)
		return
	}
	if class := settle(policy, out); class != "" {
		// The provider answered above the approved ceiling: deliver nothing and say so loudly.
		meta.failed(class, "voice_failed", latency)
		s.opts.Logger.ErrorContext(r.Context(), "transcription outside the pricing policy", "event", "voice_over_policy", "cost_nanos", out.CostNanos, "max_cost_nanos", policy.MaxCostNanos, "duration_ms", out.DurationMs, "pricing_version", s.opts.Pricing.Config().Version)
		fail(w, http.StatusBadGateway, "voice_failed", nil)
		return
	}
	meta.succeeded(out, latency)
	respond(w, http.StatusOK, response{Text: out.Text, Language: out.Language, DurationMs: int(out.DurationMs), ServedModel: out.Model, FallbackUsed: out.FallbackUsed})
}

// policy is the active transcription rate, or nil when voice is not available: no approved rate, a
// rate that charges Credits (there is no charging path), or no daily allowance.
func (s *Service) policy() *pricing.TranscriptionConfig {
	cfg := s.opts.Pricing.Config().Transcribe
	if cfg == nil || cfg.Credits != 0 || cfg.DailyRequests <= 0 || cfg.MaxCostNanos <= 0 {
		return nil
	}
	return cfg
}

// settle checks an Engine answer against the quote policy of the active pricing version. It returns
// an error class when the answer must not be delivered, else "".
func settle(policy *pricing.TranscriptionConfig, out *enginev1.TranscribeResponse) string {
	if out.GetCostNanos() > uint64(policy.MaxCostNanos) { //nolint:gosec // policy() guarantees a positive ceiling
		return "voice_over_policy"
	}
	// The provider bills by the second: more than the limit plus slack means a longer recording got through.
	if out.GetDurationMs() > MaxDurationMs+durationSlackMs {
		return "voice_over_policy"
	}
	return ""
}

// readRequest streams the multipart body: no ParseMultipartForm, so nothing can spill to a temp file.
func readRequest(w http.ResponseWriter, r *http.Request) (req request, code string, status int) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return req, "voice_invalid", http.StatusBadRequest
	}
	reader := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxBodyBytes), params["boundary"])
	seen := map[string]bool{}
	for parts := 0; ; parts++ {
		if parts > 8 {
			return req, "voice_invalid", http.StatusBadRequest
		}
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return req, "voice_too_large", http.StatusRequestEntityTooLarge
		}
		if err != nil {
			return req, "voice_invalid", http.StatusBadRequest
		}
		name := part.FormName()
		if seen[name] {
			return req, "voice_invalid", http.StatusBadRequest
		}
		seen[name] = true
		if name == "audio" {
			data, err := io.ReadAll(io.LimitReader(part, MaxAudioBytes+1))
			var tooBig *http.MaxBytesError
			switch {
			case errors.As(err, &tooBig) || len(data) > MaxAudioBytes:
				return req, "voice_too_large", http.StatusRequestEntityTooLarge
			case err != nil || len(data) == 0:
				return req, "voice_invalid", http.StatusBadRequest
			}
			req.audio = data
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, 129))
		if err != nil || len(value) > 128 {
			return req, "voice_invalid", http.StatusBadRequest
		}
		switch name {
		case "purpose":
			req.purpose = string(value)
		case "lesson_id":
			req.lessonID = strings.ToLower(string(value))
		case "duration_ms":
			n, err := strconv.Atoi(string(value))
			if err != nil {
				return req, "voice_invalid", http.StatusBadRequest
			}
			req.durationMs = n
		}
	}
	switch {
	case req.audio == nil || (req.purpose != "tutor" && req.purpose != "note") || !uuidPattern.MatchString(req.lessonID) || !seen["duration_ms"]:
		return req, "voice_invalid", http.StatusBadRequest
	case req.durationMs > MaxDurationMs:
		return req, "voice_too_long", http.StatusBadRequest
	case req.durationMs < MinDurationMs:
		return req, "voice_too_short", http.StatusBadRequest
	}
	return req, "", 0
}

// classify maps an Engine error to the HTTP answer and the analytics error class.
func classify(err error) (httpStatus int, code, class string) {
	if _, _, ok := engine.NotPromoted(err); ok {
		return http.StatusServiceUnavailable, "voice_unavailable", "voice_unavailable"
	}
	st, isStatus := status.FromError(err)
	if !isStatus {
		if errors.Is(err, context.DeadlineExceeded) {
			return http.StatusBadGateway, "voice_failed", "voice_timeout"
		}
		return http.StatusBadGateway, "voice_failed", "voice_failed"
	}
	reason := ""
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetDomain() == transcribeDomain {
			reason = info.GetReason()
		}
	}
	switch {
	case st.Code() == codes.InvalidArgument || reason == "invalid_audio":
		return http.StatusBadRequest, "voice_invalid", "voice_invalid"
	case reason == "transcribe_timeout" || st.Code() == codes.DeadlineExceeded:
		return http.StatusBadGateway, "voice_failed", "voice_timeout"
	case reason == "transcribe_cost_ceiling":
		return http.StatusBadGateway, "voice_failed", "voice_cost_ceiling"
	}
	return http.StatusBadGateway, "voice_failed", "voice_failed"
}

// eventMeta carries what every voice event shares, and nothing about the audio or the words.
type eventMeta struct {
	s        *Service
	learner  string
	purpose  string
	bucket   string
	platform string
	version  string
	ctx      context.Context
}

func (s *Service) metadata(r *http.Request, learner string, req request, _ Container) eventMeta {
	platform := r.Header.Get("X-Platform")
	switch platform {
	case "web", "desktop", "ios", "android":
	default:
		platform = "web"
	}
	version := r.Header.Get("X-App-Version")
	if !versionExpr.MatchString(version) {
		version = "0.1.0"
	}
	return eventMeta{s: s, learner: learner, purpose: req.purpose, bucket: DurationBucket(req.durationMs), platform: platform, version: version, ctx: context.WithoutCancel(r.Context())}
}

func (m eventMeta) emit(event string, props map[string]any) {
	if m.s.opts.Events == nil {
		return
	}
	props["purpose"], props["duration_bucket"] = m.purpose, m.bucket
	props["source"], props["platform"], props["app_version"] = "api", m.platform, m.version
	if err := m.s.opts.Events.Capture(m.learner, event, props); err != nil {
		m.s.opts.Logger.WarnContext(m.ctx, "voice analytics capture failed", "event", event)
	}
}

// succeeded logs and emits the outcome of a delivered answer. Neither the transcript nor its length
// is recorded; `empty` says whether anything was heard.
func (m eventMeta) succeeded(out *enginev1.TranscribeResponse, latencyMs int) {
	empty := strings.TrimSpace(out.GetText()) == ""
	m.s.opts.Logger.InfoContext(m.ctx, "voice transcription", "event", "voice_transcription", "learner_id", m.learner, "purpose", m.purpose, "duration_bucket", m.bucket, "language", out.GetLanguage(), "model", out.GetModel(), "fallback_used", out.GetFallbackUsed(), "outcome", "ok", "empty", empty, "latency_ms", latencyMs, "cost_nanos", out.GetCostNanos())
	common := map[string]any{"model": out.GetModel(), "fallback_used": out.GetFallbackUsed()}
	m.emit("transcription_succeeded", clone(common))
	m.emit("voice_transcription_succeeded", map[string]any{"language": out.GetLanguage(), "served_model": out.GetModel(), "fallback_used": out.GetFallbackUsed(), "empty": empty})
	if out.GetFallbackUsed() {
		m.emit("transcription_fallback_used", clone(common))
	}
	m.generation(map[string]any{"succeeded": true, "usage_available": true, "model": out.GetModel(), "fallback_used": out.GetFallbackUsed(), "latency_ms": latencyMs, "provider_cost_nanos": out.GetCostNanos()})
}

func (m eventMeta) failed(class, code string, latencyMs int) {
	m.s.opts.Logger.WarnContext(m.ctx, "voice transcription", "event", "voice_transcription", "learner_id", m.learner, "purpose", m.purpose, "duration_bucket", m.bucket, "outcome", code, "error_class", class, "latency_ms", latencyMs)
	m.emit("transcription_failed", map[string]any{"error_class": class})
	m.emit("voice_transcription_failed", map[string]any{"error_class": class})
	m.generation(map[string]any{"succeeded": false, "usage_available": false, "latency_ms": latencyMs, "error_code": class})
}

// generation maps the call to the shared `$ai_generation` shape, with no content.
func (m eventMeta) generation(props map[string]any) {
	if m.s.opts.Events == nil {
		return
	}
	props["feature"], props["provider"] = "transcribe", "openrouter"
	props["source"], props["platform"], props["app_version"] = "api", m.platform, m.version
	_ = m.s.opts.Events.Capture(m.learner, "$ai_generation", props)
}

func clone(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
