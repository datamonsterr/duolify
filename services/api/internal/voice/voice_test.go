package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	learner = "11111111-1111-4111-8111-111111111111"
	lesson  = "22222222-2222-4222-8222-222222222222"
)

type fakeEngine struct {
	mu    sync.Mutex
	calls []*enginev1.TranscribeRequest
	reply *enginev1.TranscribeResponse
	err   error
}

func (e *fakeEngine) Transcribe(_ context.Context, r *enginev1.TranscribeRequest) (*enginev1.TranscribeResponse, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, r)
	return e.reply, e.err
}
func (e *fakeEngine) count() int { e.mu.Lock(); defer e.mu.Unlock(); return len(e.calls) }

type fakeCounters struct {
	mu   sync.Mutex
	n    map[string]int64
	ttl  map[string]time.Duration
	fail bool
}

func newCounters() *fakeCounters {
	return &fakeCounters{n: map[string]int64{}, ttl: map[string]time.Duration{}}
}
func (c *fakeCounters) Incr(_ context.Context, key string, ttl time.Duration) (int64, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fail {
		return 0, 0, errors.New("redis down")
	}
	c.n[key]++
	if _, ok := c.ttl[key]; !ok {
		c.ttl[key] = ttl
	}
	return c.n[key], c.ttl[key], nil
}
func (c *fakeCounters) Decr(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n[key]--
	return nil
}

type fakeLessons struct{ yes bool }

func (l fakeLessons) Entitled(context.Context, string, string) (bool, error) { return l.yes, nil }

type fakePricing struct{ cfg pricing.Config }

func (p fakePricing) Config() pricing.Config { return p.cfg }

type event struct {
	name  string
	props map[string]any
}
type fakeEvents struct {
	mu  sync.Mutex
	got []event
}

func (e *fakeEvents) Capture(_ string, name string, props map[string]any) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.got = append(e.got, event{name, props})
	return nil
}
func (e *fakeEvents) Close() error { return nil }
func (e *fakeEvents) names() []string {
	var out []string
	for _, g := range e.got {
		out = append(out, g.name)
	}
	return out
}

type rig struct {
	svc      *Service
	engine   *fakeEngine
	counters *fakeCounters
	events   *fakeEvents
	cfg      pricing.Config
	lessons  fakeLessons
	now      time.Time
	logs     *bytes.Buffer
}

func approved() pricing.Config {
	return pricing.Config{Version: "usd-voice-preview-2026-11-23", Transcribe: &pricing.TranscriptionConfig{Credits: 0, USDNanosPerSecond: 3330, MaxCostNanos: 500_000, DailyRequests: 60}}
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{engine: &fakeEngine{reply: &enginev1.TranscribeResponse{Text: "hello there", Language: "en", DurationMs: 4000, Model: "qwen/qwen3-asr-0.6b", CostNanos: 13_000}}, counters: newCounters(), events: &fakeEvents{}, cfg: approved(), lessons: fakeLessons{yes: true}, now: time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC), logs: &bytes.Buffer{}}
	r.rebuild()
	return r
}

func (r *rig) rebuild() {
	r.svc = New(Options{Engine: r.engine, Pricing: fakePricing{r.cfg}, Lessons: r.lessons, Counters: r.counters, Events: r.events, Logger: slog.New(slog.NewTextHandler(r.logs, nil)), Now: func() time.Time { return r.now }})
}

func (r *rig) post(t *testing.T, u upload, headers ...string) *httptest.ResponseRecorder {
	t.Helper()
	body, ct := multipartBody(t, u)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/voice/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	req = req.WithContext(accounts.WithLearnerID(req.Context(), learner))
	rec := httptest.NewRecorder()
	r.svc.ServeHTTP(rec, req)
	return rec
}

func good(audio []byte, ms int) upload {
	return upload{audio: audio, purpose: "tutor", lessonID: lesson, durationMs: itoa(ms)}
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	code, _ := body["error"].(string)
	return code
}

func TestTranscribesAnEntitledRecording(t *testing.T) {
	r := newRig(t)
	rec := r.post(t, good(webmStream(4000), 4000), "X-Platform", "android", "X-App-Version", "1.2.3")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	var got response
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Text != "hello there" || got.Language != "en" || got.DurationMs != 4000 || got.ServedModel != "qwen/qwen3-asr-0.6b" || got.FallbackUsed {
		t.Fatalf("response %+v", got)
	}
	if r.engine.count() != 1 {
		t.Fatalf("engine calls %d", r.engine.count())
	}
	call := r.engine.calls[0]
	if call.Container != "webm" || call.Purpose != "tutor" || call.DurationMs != 4000 || len(call.Audio) == 0 {
		t.Fatalf("engine request %+v", call)
	}
	want := []string{"transcription_requested", "transcription_succeeded", "voice_transcription_succeeded", "$ai_generation"}
	if strings.Join(r.events.names(), ",") != strings.Join(want, ",") {
		t.Fatalf("events %v", r.events.names())
	}
	for _, e := range r.events.got {
		if e.props["platform"] != "android" || e.props["source"] != "api" || e.props["app_version"] != "1.2.3" {
			t.Fatalf("%s common fields %v", e.name, e.props)
		}
		for k, v := range e.props {
			if s, ok := v.(string); ok && strings.Contains(s, "hello there") {
				t.Fatalf("%s carries the transcript in %s", e.name, k)
			}
		}
	}
	if r.events.got[2].props["duration_bucket"] != "2_5s" || r.events.got[2].props["purpose"] != "tutor" {
		t.Fatalf("voice event %v", r.events.got[2].props)
	}
	if strings.Contains(r.logs.String(), "hello there") {
		t.Fatal("transcript in logs")
	}
}

func TestMP4IsAcceptedWhateverTheClientCalledIt(t *testing.T) {
	r := newRig(t)
	body, _ := multipartBody(t, good(mp4("isom", 1000, 0), 3000))
	_ = body
	if rec := r.post(t, good(mp4("isom", 1000, 0), 3000)); rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if r.engine.calls[0].Container != "m4a" {
		t.Fatalf("container %q", r.engine.calls[0].Container)
	}
}

func TestRejectionsHappenBeforeAnyEngineCall(t *testing.T) {
	big := append(webmStream(1000), make([]byte, MaxAudioBytes)...)
	cases := []struct {
		name   string
		mutate func(*rig)
		up     upload
		status int
		code   string
	}{
		{"audio over 1 MB", nil, good(big, 4000), 413, "voice_too_large"},
		{"declared 45 s", nil, good(webmStream(4000), 45000), 400, "voice_too_long"},
		{"measured 45 s declared 4 s", nil, good(webmStream(45000), 4000), 400, "voice_too_long"},
		{"measured longer than declared", nil, good(webmStream(12000), 4000), 400, "voice_invalid"},
		{"declared under 0.5 s", nil, good(webmStream(300), 300), 400, "voice_too_short"},
		{"wav container", nil, good(append([]byte("RIFF\x00\x00\x00\x00WAVE"), make([]byte, 64)...), 4000), 415, "voice_unsupported_container"},
		{"plain text", nil, good([]byte("not audio at all, just words"), 4000), 415, "voice_unsupported_container"},
		{"bad purpose", nil, upload{audio: webmStream(2000), purpose: "dictation", lessonID: lesson, durationMs: "2000"}, 400, "voice_invalid"},
		{"bad lesson id", nil, upload{audio: webmStream(2000), purpose: "note", lessonID: "nope", durationMs: "2000"}, 400, "voice_invalid"},
		{"missing duration", nil, upload{audio: webmStream(2000), purpose: "note", lessonID: lesson, durationMs: "2000", omit: "duration_ms"}, 400, "voice_invalid"},
		{"missing audio", nil, upload{purpose: "note", lessonID: lesson, durationMs: "2000", omit: "audio"}, 400, "voice_invalid"},
		{"lesson not theirs", func(r *rig) { r.lessons = fakeLessons{yes: false} }, good(webmStream(2000), 2000), 403, "voice_not_entitled"},
		{"no transcription rate", func(r *rig) { r.cfg.Transcribe = nil }, good(webmStream(2000), 2000), 503, "voice_unavailable"},
		{"rate charges credits", func(r *rig) { r.cfg.Transcribe.Credits = 1 }, good(webmStream(2000), 2000), 503, "voice_unavailable"},
		{"redis down fails closed", func(r *rig) { r.counters.fail = true }, good(webmStream(2000), 2000), 503, "voice_unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			if c.mutate != nil {
				c.mutate(r)
				r.rebuild()
			}
			rec := r.post(t, c.up)
			if rec.Code != c.status || errorCode(t, rec) != c.code {
				t.Fatalf("got %d %s, want %d %s", rec.Code, rec.Body, c.status, c.code)
			}
			if r.engine.count() != 0 {
				t.Fatal("the Engine was called")
			}
			if len(r.events.got) != 0 {
				t.Fatalf("events %v", r.events.names())
			}
		})
	}
}

func TestBodyOverTheLimitIsRefusedByDeclaredLength(t *testing.T) {
	r := newRig(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/voice/transcriptions", io.NopCloser(strings.NewReader("x")))
	req.ContentLength = 5 << 20
	req.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	req = req.WithContext(accounts.WithLearnerID(req.Context(), learner))
	rec := httptest.NewRecorder()
	r.svc.ServeHTTP(rec, req)
	if rec.Code != 413 || errorCode(t, rec) != "voice_too_large" || r.engine.count() != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestRateLimitAndDailyCap(t *testing.T) {
	r := newRig(t)
	r.cfg.Transcribe.DailyRequests = 8
	r.rebuild()
	for i := 1; i <= DefaultRatePerMinute; i++ {
		if rec := r.post(t, good(webmStream(2000), 2000)); rec.Code != 200 {
			t.Fatalf("request %d: %d %s", i, rec.Code, rec.Body)
		}
	}
	rec := r.post(t, good(webmStream(2000), 2000))
	if rec.Code != 429 || errorCode(t, rec) != "voice_rate_limited" || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("%d %s %v", rec.Code, rec.Body, rec.Header())
	}
	if r.engine.count() != DefaultRatePerMinute {
		t.Fatalf("engine calls %d", r.engine.count())
	}
	// Next minute: the rate window is new, the day is not.
	delete(r.counters.n, "voice:rate:"+learner)
	delete(r.counters.ttl, "voice:rate:"+learner)
	for i := 0; i < 2; i++ {
		if rec := r.post(t, good(webmStream(2000), 2000)); rec.Code != 200 {
			t.Fatalf("%d %s", rec.Code, rec.Body)
		}
	}
	rec = r.post(t, good(webmStream(2000), 2000))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 429 || body["error"] != "voice_daily_cap" || body["resets_at"] != "2026-11-24T00:00:00Z" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if r.engine.count() != 8 {
		t.Fatalf("engine calls %d", r.engine.count())
	}
	// Both counters carry an expiry; the day key outlives the day by an hour at most.
	for key, ttl := range r.counters.ttl {
		if ttl <= 0 || ttl > 26*time.Hour {
			t.Fatalf("%s ttl %v", key, ttl)
		}
	}
	if got := r.counters.n["voice:day:"+learner+":20261123"]; got != 8 {
		t.Fatalf("day counter %d: a refused request must give its slot back", got)
	}
}

func TestEngineFailuresAreTypedAndOnlyPreProviderOnesRefundTheDay(t *testing.T) {
	info := func(domain, reason string, code codes.Code) error {
		st, err := status.New(code, reason).WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: domain})
		if err != nil {
			t.Fatal(err)
		}
		return st.Err()
	}
	cases := []struct {
		name   string
		err    error
		status int
		code   string
		class  string
		refund bool
	}{
		{"both models failed", info(transcribeDomain, "transcribe_failed", codes.Unavailable), 502, "voice_failed", "voice_failed", false},
		{"timeouts", info(transcribeDomain, "transcribe_timeout", codes.Unavailable), 502, "voice_failed", "voice_timeout", false},
		{"cost ceiling", info(transcribeDomain, "transcribe_cost_ceiling", codes.Unavailable), 502, "voice_failed", "voice_cost_ceiling", false},
		{"not promoted", info("engine.duolify", "promotion_evidence_missing", codes.Unavailable), 503, "voice_unavailable", "voice_unavailable", true},
		{"engine rejects the audio", info(transcribeDomain, "invalid_audio", codes.InvalidArgument), 400, "voice_invalid", "voice_invalid", true},
		{"engine unreachable", status.Error(codes.Unavailable, "connection refused"), 502, "voice_failed", "voice_failed", false},
		{"deadline", context.DeadlineExceeded, 502, "voice_failed", "voice_timeout", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.engine.reply, r.engine.err = nil, c.err
			rec := r.post(t, good(webmStream(2000), 2000))
			if rec.Code != c.status || errorCode(t, rec) != c.code {
				t.Fatalf("%d %s", rec.Code, rec.Body)
			}
			want := int64(1)
			if c.refund {
				want = 0
			}
			if got := r.counters.n["voice:day:"+learner+":20261123"]; got != want {
				t.Fatalf("daily cap used %d, want %d", got, want)
			}
			last := r.events.got[len(r.events.got)-1]
			if last.name != "$ai_generation" || last.props["error_code"] != c.class || last.props["succeeded"] != false {
				t.Fatalf("last event %+v", last)
			}
			failed := r.events.got[2]
			if failed.name != "voice_transcription_failed" || failed.props["error_class"] != c.class {
				t.Fatalf("failure event %+v", failed)
			}
		})
	}
}

func TestSettlementAgainstTheQuotePolicy(t *testing.T) {
	policy := approved().Transcribe
	for _, c := range []struct {
		name string
		out  *enginev1.TranscribeResponse
		want string
	}{
		{"inside", &enginev1.TranscribeResponse{CostNanos: 500_000, DurationMs: 30_000}, ""},
		{"free", &enginev1.TranscribeResponse{}, ""},
		{"over the cost ceiling", &enginev1.TranscribeResponse{CostNanos: 500_001, DurationMs: 4000}, "voice_over_policy"},
		{"billed for a longer recording", &enginev1.TranscribeResponse{CostNanos: 10, DurationMs: 31_000}, "voice_over_policy"},
	} {
		if got := settle(policy, c.out); got != c.want {
			t.Errorf("%s: %q", c.name, got)
		}
	}
	r := newRig(t)
	r.engine.reply = &enginev1.TranscribeResponse{Text: "dear", CostNanos: 900_000, DurationMs: 4000, Model: "m"}
	rec := r.post(t, good(webmStream(2000), 2000))
	if rec.Code != 502 || strings.Contains(rec.Body.String(), "dear") {
		t.Fatalf("an over-policy answer was delivered: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(r.logs.String(), "voice_over_policy") {
		t.Fatalf("no alertable log line: %s", r.logs)
	}
}

func TestEmptyTranscriptIsASuccessTheClientTreatsAsNothingUnderstood(t *testing.T) {
	r := newRig(t)
	r.engine.reply = &enginev1.TranscribeResponse{Text: "", Language: "", DurationMs: 3000, Model: "qwen/qwen3-asr-0.6b"}
	rec := r.post(t, good(webmStream(2000), 2000))
	var got response
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != 200 || got.Text != "" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if r.events.got[2].props["empty"] != true {
		t.Fatalf("event %v", r.events.got[2].props)
	}
}

func TestOnlyPostIsServedAndSignInIsRequired(t *testing.T) {
	r := newRig(t)
	rec := httptest.NewRecorder()
	r.svc.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/voice/transcriptions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatal(rec.Code)
	}
	body, ct := multipartBody(t, good(webmStream(2000), 2000))
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/voice/transcriptions", body)
	req.Header.Set("Content-Type", ct)
	rec = httptest.NewRecorder()
	r.svc.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || r.engine.count() != 0 {
		t.Fatal(rec.Code)
	}
}

func TestNoTempFilesAreCreated(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	r := newRig(t)
	rec := r.post(t, good(append(webmStream(2000), make([]byte, 900_000)...), 2000))
	if rec.Code != 200 && rec.Code != 400 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	entries, err := readDir(tmp)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temp dir holds %v (%v)", entries, err)
	}
}

func TestOverPolicyAnswerKeepsTheDailySlot(t *testing.T) {
	r := newRig(t)
	r.engine.reply.CostNanos = 600_000
	if rec := r.post(t, good(webmStream(2000), 2000)); rec.Code != 502 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := r.counters.n["voice:day:"+learner+":20261123"]; got != 1 {
		t.Fatalf("over-policy answer was billed; day counter %d", got)
	}
}

type fakeDays struct{ loc *time.Location }

func (d fakeDays) LocalDay(_ context.Context, _ string, at time.Time) (string, time.Time, error) {
	l := at.In(d.loc)
	return l.Format(time.DateOnly), time.Date(l.Year(), l.Month(), l.Day()+1, 0, 0, 0, 0, d.loc), nil
}

func TestDailyCapFollowsTheLearnerLocalDay(t *testing.T) {
	r := newRig(t)
	r.cfg.Transcribe.DailyRequests = 1
	loc := time.FixedZone("UTC+7", 7*3600)
	r.now = time.Date(2026, 11, 23, 18, 0, 0, 0, time.UTC) // 01:00 on the 24th in UTC+7
	r.rebuild()
	r.svc.opts.Days = fakeDays{loc}
	if rec := r.post(t, good(webmStream(2000), 2000)); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := r.counters.n["voice:day:"+learner+":20261124"]; got != 1 {
		t.Fatalf("counter not keyed by local day: %v", r.counters.n)
	}
	delete(r.counters.n, "voice:rate:"+learner)
	rec := r.post(t, good(webmStream(2000), 2000))
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != 429 || body["resets_at"] != "2026-11-24T17:00:00Z" {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}
