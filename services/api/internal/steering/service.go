package steering

import (
	"context"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Screener classifies instruction data at platform expense.
type Screener interface {
	ScreenInstruction(context.Context, *pb.ScreenInstructionRequest) (*pb.ScreenInstructionResponse, error)
}

// Options supplies stateless service dependencies.
type Options struct {
	Pool   *pgxpool.Pool
	Engine Screener
	Events analytics.Events
	// ScreenTimeout bounds the classifier call; zero means the 10 second default.
	ScreenTimeout time.Duration
	// Price supplies the per-lesson price shown in the fork warning (E16-05); nil omits it.
	Price LessonPrice
}

// Service saves screened private Course instructions.
type Service struct{ opts Options }

// New constructs the steering boundary.
func New(opts Options) *Service { return &Service{opts: opts} }

// Input contains learner-provided style and focus preferences.
type Input struct {
	Text  string `json:"text"`
	Scope string `json:"scope"`
	// ConfirmFork is the Learner's explicit yes to the fork warning on a published copy (E16-05).
	ConfirmFork bool `json:"confirm_fork,omitempty"`
}

// Record is an owned instruction and its current application state.
type Record struct {
	ID              string    `json:"id"`
	Text            string    `json:"text"`
	Scope           string    `json:"scope"`
	Status          string    `json:"status"`
	Version         uint32    `json:"version"`
	AddedAt         time.Time `json:"added_at"`
	AppliedLessonID *string   `json:"applied_lesson_id"`
	// Delivery says how an active instruction meets generation: ready, running (pinned by a live job) or waits.
	Delivery string `json:"delivery"`
}
type refusal string

func (e refusal) Error() string { return string(e) }

// Code returns a closed error code without exposing provider or database details.
func Code(err error) string {
	var r refusal
	if errors.As(err, &r) {
		return string(r)
	}
	return "unavailable"
}
func (s *Service) emit(owner, event string, props map[string]any) {
	if s.opts.Events == nil {
		return
	}
	props["source"] = "api"
	props["platform"] = "web"
	props["app_version"] = "1.0.0"
	_ = s.opts.Events.Capture(owner, event, props)
}
func (s *Service) privateCourse(ctx context.Context, owner, course string) (string, error) {
	if _, e := uuid.Parse(course); e != nil {
		return "", refusal("not_found")
	}
	var title string
	err := s.opts.Pool.QueryRow(ctx, `SELECT c.title FROM courses c JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=c.learner_id WHERE c.id=$1::uuid AND c.learner_id=$2::uuid AND c.deleted_at IS NULL AND c.visibility='private'`, course, owner).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", refusal("not_found")
	}
	return title, err
}

func (s *Service) screenTimeout() time.Duration {
	if s.opts.ScreenTimeout > 0 {
		return s.opts.ScreenTimeout
	}
	return 10 * time.Second
}

// screen runs the platform-paid classifier and fails closed; it returns a reason code on refusal.
func (s *Service) screen(ctx context.Context, owner, topic, text string) (*pb.ScreenInstructionResponse, string) {
	if s.opts.Engine == nil {
		return nil, "screening_unavailable"
	}
	screenCtx, cancel := context.WithTimeout(ctx, s.screenTimeout())
	verdict, screenErr := s.opts.Engine.ScreenInstruction(screenCtx, &pb.ScreenInstructionRequest{Text: text, CourseTopic: topic})
	expired := screenCtx.Err() != nil
	cancel()
	if screenErr != nil || expired || verdict == nil || verdict.ScreeningVersion != "steering-v1" {
		s.emit(owner, "$ai_generation", map[string]any{"feature": "steer_screen", "succeeded": false, "usage_available": false, "error_code": "screening_unavailable"})
		return nil, "screening_unavailable"
	}
	for _, a := range verdict.Attempts {
		if a != nil {
			s.emit(owner, "$ai_generation", map[string]any{"feature": "steer_screen", "provider": a.Provider, "model": a.Model, "input_tokens": int(a.InputTokens), "output_tokens": int(a.OutputTokens), "latency_ms": int(a.LatencyMs), "succeeded": true, "fallback_used": a.FallbackUsed})
		}
	}
	if !verdict.Allowed {
		switch verdict.ReasonCode {
		case "prompt_injection", "unsafe_content", "off_topic_for_course", "too_long":
			return nil, verdict.ReasonCode
		default:
			return nil, "screening_unavailable"
		}
	}
	if verdict.ReasonCode != "" || len(verdict.Attempts) == 0 {
		return nil, "screening_unavailable"
	}
	return verdict, ""
}

// Add screens before saving and serializes capacity checks with generation snapshots.
func (s *Service) Add(ctx context.Context, owner, course string, in Input) (Record, error) {
	var out Record
	topic, err := s.privateCourse(ctx, owner, course)
	if err != nil {
		return out, err
	}
	var maxChars, maxActive, maxTotal, perHour int
	if err = s.opts.Pool.QueryRow(ctx, `SELECT max_characters,max_active,max_total_characters,adds_per_hour FROM steering_config WHERE singleton`).Scan(&maxChars, &maxActive, &maxTotal, &perHour); err != nil {
		return out, err
	}
	reject := func(code string) (Record, error) {
		s.emit(owner, "course_instruction_rejected", map[string]any{"course_id": course, "reason_code": code})
		return out, refusal(code)
	}
	if in.Scope != "next_lesson" && in.Scope != "rest_of_course" {
		return reject("invalid_scope")
	}
	if reason := ValidateText(in.Text, maxChars); reason != "" {
		return reject(reason)
	}
	// A published copy forks only after an explicit confirmation; declining saves nothing, not even a screening call.
	info, err := s.forkInfo(ctx, owner, course)
	if err != nil {
		return out, err
	}
	if info != nil && info.Required && !in.ConfirmFork {
		return out, refusal("fork_confirmation_required")
	}
	verdict, code := s.screen(ctx, owner, topic, in.Text)
	if code != "" {
		return reject(code)
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// One lock orders adds and job snapshots, even across API instances.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT c.id::text FROM courses c JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=c.learner_id WHERE c.id=$1::uuid AND c.learner_id=$2::uuid AND c.deleted_at IS NULL AND c.visibility='private' FOR UPDATE OF c`, course, owner).Scan(&locked); err != nil {
		return out, err
	}
	var count, total, rate int
	if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE status='active'),COALESCE(sum(char_length(text)) FILTER(WHERE status='active'),0),count(*) FILTER(WHERE added_at>clock_timestamp()-interval '1 hour') FROM course_instructions WHERE course_id=$1::uuid AND learner_id=$2::uuid`, course, owner).Scan(&count, &total, &rate); err != nil {
		return out, err
	}
	if rate >= perHour {
		return reject("rate_limit")
	}
	if count >= maxActive {
		return reject("active_limit")
	}
	if total+utf8.RuneCountInString(in.Text) > maxTotal {
		return reject("total_length_limit")
	}
	forkIndex, forkedNow, err := s.forkTx(ctx, tx, owner, course)
	if err != nil {
		return out, err
	}
	usage, _ := json.Marshal(verdict.Attempts)
	err = tx.QueryRow(ctx, `INSERT INTO course_instructions(learner_id,course_id,text,scope,screening_verdict,screening_version,screening_usage) VALUES($1::uuid,$2::uuid,$3,$4,'allow',$5,$6) RETURNING id::text,text,scope,status,version,added_at,applied_lesson_id::text`, owner, course, in.Text, in.Scope, verdict.ScreeningVersion, usage).Scan(&out.ID, &out.Text, &out.Scope, &out.Status, &out.Version, &out.AddedAt, &out.AppliedLessonID)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	if forkedNow {
		s.emit(owner, "course_forked_from_published", map[string]any{"course_id": course, "fork_lesson_index": forkIndex})
	}
	s.emit(owner, "course_instruction_added", map[string]any{"course_id": course, "scope": in.Scope, "length_bucket": LengthBucket(utf8.RuneCountInString(in.Text)), "is_course_copy": info != nil})
	return out, nil
}

// Purge removes abuse-review records 30 days after soft deletion. Snapshots remain independent.
func (s *Service) Purge(ctx context.Context) error {
	_, err := s.opts.Pool.Exec(ctx, `DELETE FROM course_instructions WHERE status='deleted' AND deleted_at<now()-interval '30 days'`)
	return err
}

// Maintain runs ephemeral cleanup scheduling; all records remain in Postgres.
func (s *Service) Maintain(ctx context.Context) {
	_ = s.Purge(ctx)
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.Purge(ctx)
		}
	}
}
