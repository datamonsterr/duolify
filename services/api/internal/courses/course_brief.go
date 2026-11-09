// Package courses implements learner-owned course brief confirmation, independent of paid generation.
package courses

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Brief contains explicit learner data only; no transcript or inferred profile.
type Brief struct {
	Topic             string `json:"topic"`
	Goal              string `json:"goal"`
	IncludeTopics     string `json:"include_topics"`
	SkipTopics        string `json:"skip_topics"`
	Level             string `json:"level"`
	Maths             string `json:"maths"`
	ViewingConditions string `json:"viewing_conditions"`
	LessonLength      string `json:"lesson_length"`
	Language          string `json:"language"`
}

// Validate bounds every field and requires only a topic for explicit confirmation.
func (b Brief) Validate(confirm bool) error {
	for _, v := range []string{b.Topic, b.Goal, b.IncludeTopics, b.SkipTopics, b.Level, b.Maths, b.ViewingConditions, b.LessonLength, b.Language} {
		if !utf8.ValidString(v) || utf8.RuneCountInString(v) > 500 || strings.ContainsRune(v, 0) {
			return fmt.Errorf("brief field exceeds limit")
		}
	}
	if confirm && strings.TrimSpace(b.Topic) == "" {
		return fmt.Errorf("topic required")
	}
	return nil
}

// Hash binds confirmation to the exact ordered canonical content.
func (b Brief) Hash() string { raw, _ := json.Marshal(b); return fmt.Sprintf("%x", sha256.Sum256(raw)) }
func (b Brief) proto() *enginev1.CourseBrief {
	return &enginev1.CourseBrief{Topic: b.Topic, Goal: b.Goal, IncludeTopics: b.IncludeTopics, SkipTopics: b.SkipTopics, Level: b.Level, Maths: b.Maths, ViewingConditions: b.ViewingConditions, LessonLength: b.LessonLength, Language: b.Language}
}
func fromProto(b *enginev1.CourseBrief) Brief {
	return Brief{Topic: b.GetTopic(), Goal: b.GetGoal(), IncludeTopics: b.GetIncludeTopics(), SkipTopics: b.GetSkipTopics(), Level: b.GetLevel(), Maths: b.GetMaths(), ViewingConditions: b.GetViewingConditions(), LessonLength: b.GetLessonLength(), Language: b.GetLanguage()}
}

// Engine exposes only clarification: it cannot preview, enroll or reserve Credits.
type Engine interface {
	CourseBriefChat(context.Context, *enginev1.CourseBriefChatRequest) (*enginev1.CourseBriefChatResponse, error)
}

// Catalogue provides the immutable Pricing config snapshot.
type Catalogue interface{ Config() pricing.Config }

// Options wires the platform-funded assistant; forms need only Postgres.
type Options struct {
	Pool         *pgxpool.Pool
	Engine       Engine
	Pricing      Catalogue
	Events       analytics.Events
	Environment  string
	LiveApproved bool
}

// Service owns request-local clarification and immutable revisions.
type Service struct{ opts Options }

// New constructs a brief service with no learner wallet dependency.
func New(opts Options) *Service { return &Service{opts: opts} }

type bounds struct {
	InputTokens            uint32 `json:"input_tokens"`
	OutputTokens           uint32 `json:"output_tokens"`
	AttemptTimeoutMS       uint32 `json:"attempt_timeout_ms"`
	MaxAttempts            uint32 `json:"max_attempts"`
	MaxCostNanos           uint64 `json:"max_cost_nanos"`
	InputNanosPerToken     uint64 `json:"input_nanos_per_token"`
	OutputNanosPerToken    uint64 `json:"output_nanos_per_token"`
	RequestsPerHour        int    `json:"requests_per_hour"`
	PlatformRequestsPerDay int    `json:"platform_requests_per_day"`
}

func (s *Service) policy(ctx context.Context) (pricing.Config, bounds, bool) {
	var c pricing.Config
	var b bounds
	if s.opts.Pricing == nil || s.opts.Engine == nil || s.opts.Pool == nil {
		return c, b, false
	}
	c = s.opts.Pricing.Config()
	action, ok := c.Actions["course_brief_chat"]
	if !ok || action.Credits != 0 {
		return c, b, false
	}
	var limits struct {
		Brief       bounds  `json:"course_brief_chat"`
		PlatformUSD float64 `json:"platform_usd_per_day"`
	}
	if json.Unmarshal(c.Limits, &limits) != nil {
		return c, b, false
	}
	b = limits.Brief
	if b.InputTokens == 0 || b.OutputTokens == 0 || b.MaxAttempts == 0 || b.MaxAttempts > 3 || b.AttemptTimeoutMS == 0 || b.AttemptTimeoutMS > 3000 || b.RequestsPerHour <= 0 || b.PlatformRequestsPerDay <= 0 || b.MaxCostNanos == 0 || b.InputNanosPerToken == 0 || b.OutputNanosPerToken == 0 || limits.PlatformUSD <= 0 {
		return c, b, false
	}
	if float64(b.MaxCostNanos)*float64(b.PlatformRequestsPerDay) > limits.PlatformUSD*1e9 {
		return c, b, false
	}
	var approved bool
	if err := s.opts.Pool.QueryRow(ctx, "SELECT approved FROM pricing_versions WHERE version=$1", c.Version).Scan(&approved); err != nil {
		return c, b, false
	}
	enabled := s.opts.Environment == "development" || s.opts.Environment == "test" || (s.opts.Environment == "production" && approved && s.opts.LiveApproved)
	return c, b, enabled
}
func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int) {
	respond(w, code, map[string]string{"error": "Brief unavailable. Your answers are kept; use the form or retry."})
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		return false
	}
	var extra any
	return d.Decode(&extra) == io.EOF
}

// ServeHTTP requires the existing authentication middleware and never trusts client owner IDs.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		fail(w, 401)
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/v1/course-brief/config":
		c, _, enabled := s.policy(r.Context())
		var credits *int
		if a, ok := c.Actions["course_brief_chat"]; ok {
			credits = &a.Credits
		}
		respond(w, 200, map[string]any{"enabled": enabled, "credits": credits, "pricing_version": c.Version})
	case r.Method == "POST" && r.URL.Path == "/v1/course-brief/chat":
		s.chat(w, r, owner)
	case r.Method == "POST" && r.URL.Path == "/v1/course-brief/confirm":
		s.confirm(w, r, owner)
	case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/course-brief/revisions/"):
		s.get(w, r, owner, strings.TrimPrefix(r.URL.Path, "/v1/course-brief/revisions/"))
	default:
		fail(w, 404)
	}
}
func (s *Service) chat(w http.ResponseWriter, r *http.Request, owner string) {
	c, b, enabled := s.policy(r.Context())
	if !enabled {
		fail(w, 503)
		return
	}
	var in struct {
		Brief       Brief  `json:"brief"`
		CurrentStep string `json:"current_step"`
		Answer      string `json:"answer"`
		Skipped     bool   `json:"skipped"`
		IsFollowUp  bool   `json:"is_follow_up"`
	}
	if !decode(w, r, &in) || in.Brief.Validate(false) != nil || utf8.RuneCountInString(in.Answer) > 500 {
		fail(w, 400)
		return
	}
	steps := map[string]bool{"topic": true, "goal": true, "include_topics": true, "skip_topics": true, "level": true, "maths": true, "viewing_conditions": true, "lesson_length": true, "language": true}
	if !steps[in.CurrentStep] || (!in.Skipped && strings.TrimSpace(in.Answer) == "") {
		fail(w, 400)
		return
	}
	if !s.limit(r.Context(), "brief:"+owner, b.RequestsPerHour, time.Hour) || !s.limit(r.Context(), "brief-platform", b.PlatformRequestsPerDay, 24*time.Hour) {
		fail(w, 429)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	resp, err := s.opts.Engine.CourseBriefChat(ctx, &enginev1.CourseBriefChatRequest{Brief: in.Brief.proto(), CurrentStep: in.CurrentStep, Answer: in.Answer, Skipped: in.Skipped, IsFollowUp: in.IsFollowUp, Limits: &enginev1.CourseBriefLimits{InputTokens: b.InputTokens, OutputTokens: b.OutputTokens, AttemptTimeoutMs: b.AttemptTimeoutMS, MaxAttempts: b.MaxAttempts, MaxCostNanos: b.MaxCostNanos, InputNanosPerToken: b.InputNanosPerToken, OutputNanosPerToken: b.OutputNanosPerToken}})
	if err != nil || resp == nil {
		fail(w, 503)
		return
	}
	s.attemptEvents(ctx, owner, c.Version, resp.Attempts)
	if resp.Unavailable || resp.Brief == nil || fromProto(resp.Brief).Validate(false) != nil || (!steps[resp.NextStep] && resp.NextStep != "confirm") || len(resp.Message) > 1000 {
		fail(w, 503)
		return
	}
	replies := make([]map[string]string, 0, len(resp.QuickReplies))
	for _, q := range resp.QuickReplies {
		replies = append(replies, map[string]string{"id": q.Id, "label": q.Label})
	}
	respond(w, 200, map[string]any{"brief": fromProto(resp.Brief), "message": resp.Message, "quick_replies": replies, "next_step": resp.NextStep, "is_follow_up": resp.IsFollowUp, "is_complete": resp.IsComplete})
}
func (s *Service) limit(ctx context.Context, key string, maximum int, window time.Duration) bool {
	// Shared Postgres buckets remain stateless across API replicas. Only hashed identifiers persist.
	key = fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	tag, err := s.opts.Pool.Exec(ctx, `INSERT INTO auth_rate_limits(key_hash,count,expires_at) VALUES($1,1,now()+$3::interval)
 ON CONFLICT(key_hash) DO UPDATE SET count=CASE WHEN auth_rate_limits.expires_at<=now() THEN 1 ELSE auth_rate_limits.count+1 END,
 expires_at=CASE WHEN auth_rate_limits.expires_at<=now() THEN EXCLUDED.expires_at ELSE auth_rate_limits.expires_at END
 WHERE auth_rate_limits.expires_at<=now() OR auth_rate_limits.count<$2`, key, maximum, fmt.Sprintf("%f seconds", window.Seconds()))
	return err == nil && tag.RowsAffected() == 1
}
func (s *Service) attemptEvents(ctx context.Context, owner, version string, attempts []*enginev1.BriefAttempt) {
	if s.opts.Events == nil {
		return
	}
	var optOut bool
	if s.opts.Pool.QueryRow(ctx, "SELECT analytics_opt_out FROM learner_profiles WHERE learner_id::text=$1", owner).Scan(&optOut) != nil || optOut {
		return
	}
	for i, a := range attempts {
		u := a.GetUsage()
		provider, model := u.GetProvider(), u.GetModel()
		if u == nil {
			provider, model = a.GetProvider(), a.GetModel()
		}
		_ = s.opts.Events.Capture(owner, "$ai_generation", map[string]any{"source": "api", "platform": "web", "app_version": "0.1.0", "task": "course_brief_chat", "pricing_version": version, "attempt": i + 1, "provider": provider, "model": model, "input_tokens": u.GetInputTokens(), "output_tokens": u.GetOutputTokens(), "latency_ms": u.GetLatencyMs(), "fallback_used": a.FallbackUsed, "succeeded": a.Succeeded, "usage_available": u != nil})
	}
}
func (s *Service) confirm(w http.ResponseWriter, r *http.Request, owner string) {
	var in struct {
		Brief     Brief `json:"brief"`
		Confirmed bool  `json:"confirmed"`
	}
	if !decode(w, r, &in) || !in.Confirmed || in.Brief.Validate(true) != nil {
		fail(w, 400)
		return
	}
	if s.opts.Pool == nil {
		fail(w, 503)
		return
	}
	if !s.limit(r.Context(), "brief-confirm:"+owner, 60, time.Hour) {
		fail(w, 429)
		return
	}
	q := db.New(s.opts.Pool)
	if q.PurgeExpiredCourseBriefs(r.Context()) != nil {
		fail(w, 503)
		return
	}
	raw, _ := json.Marshal(in.Brief)
	var id pgtype.UUID
	if id.Scan(owner) != nil {
		fail(w, 401)
		return
	}
	result, err := q.InsertCourseBrief(r.Context(), db.InsertCourseBriefParams{LearnerID: id, Brief: raw, ContentHash: in.Brief.Hash()})
	if err != nil {
		fail(w, 503)
		return
	}
	respond(w, 201, map[string]any{"id": result.ID, "brief": in.Brief, "content_hash": result.ContentHash, "created_at": result.CreatedAt.Time, "expires_at": result.ExpiresAt.Time})
}
func (s *Service) get(w http.ResponseWriter, r *http.Request, owner, id string) {
	if s.opts.Pool == nil {
		fail(w, 503)
		return
	}
	result, err := db.New(s.opts.Pool).GetCourseBrief(r.Context(), db.GetCourseBriefParams{ID: id, LearnerID: owner})
	if err != nil {
		fail(w, 404)
		return
	}
	respond(w, 200, map[string]any{"id": result.ID, "brief": json.RawMessage(result.Brief), "content_hash": result.ContentHash, "created_at": result.CreatedAt.Time, "expires_at": result.ExpiresAt.Time})
}
