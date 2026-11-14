package steering

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	pb "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
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
}

// Service saves screened private Course instructions.
type Service struct{ opts Options }

// New constructs the steering boundary.
func New(opts Options) *Service { return &Service{opts: opts} }

// Input contains learner-provided style and focus preferences.
type Input struct {
	Text  string `json:"text"`
	Scope string `json:"scope"`
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
	err := s.opts.Pool.QueryRow(ctx, `SELECT c.title FROM courses c JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=c.learner_id WHERE c.id=$1::uuid AND c.learner_id=$2::uuid AND c.deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM course_copies cc WHERE cc.copy_course_id=c.id) AND c.visibility='private'`, course, owner).Scan(&title)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", refusal("not_found")
	}
	return title, err
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
	if s.opts.Engine == nil {
		return reject("screening_unavailable")
	}
	screenCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	verdict, screenErr := s.opts.Engine.ScreenInstruction(screenCtx, &pb.ScreenInstructionRequest{Text: in.Text, CourseTopic: topic})
	expired := screenCtx.Err() != nil
	cancel()
	if screenErr != nil || expired || verdict == nil || verdict.ScreeningVersion != "steering-v1" {
		s.emit(owner, "$ai_generation", map[string]any{"feature": "steer_screen", "succeeded": false, "usage_available": false, "error_code": "screening_unavailable"})
		return reject("screening_unavailable")
	}
	for _, a := range verdict.Attempts {
		if a != nil {
			s.emit(owner, "$ai_generation", map[string]any{"feature": "steer_screen", "provider": a.Provider, "model": a.Model, "input_tokens": int(a.InputTokens), "output_tokens": int(a.OutputTokens), "latency_ms": int(a.LatencyMs), "succeeded": screenErr == nil, "fallback_used": a.FallbackUsed})
		}
	}
	if !verdict.Allowed {
		switch verdict.ReasonCode {
		case "prompt_injection", "unsafe_content", "off_topic_for_course", "too_long":
			return reject(verdict.ReasonCode)
		default:
			return reject("screening_unavailable")
		}
	}
	if verdict.ReasonCode != "" || len(verdict.Attempts) == 0 {
		return reject("screening_unavailable")
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// One lock orders adds and job snapshots, even across API instances.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT c.id::text FROM courses c JOIN course_enrollments e ON e.course_id=c.id AND e.learner_id=c.learner_id WHERE c.id=$1::uuid AND c.learner_id=$2::uuid AND c.deleted_at IS NULL AND c.visibility='private' AND NOT EXISTS(SELECT 1 FROM course_copies cc WHERE cc.copy_course_id=c.id) FOR UPDATE OF c`, course, owner).Scan(&locked); err != nil {
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
	usage, _ := json.Marshal(verdict.Attempts)
	err = tx.QueryRow(ctx, `INSERT INTO course_instructions(learner_id,course_id,text,scope,screening_verdict,screening_version,screening_usage) VALUES($1::uuid,$2::uuid,$3,$4,'allow',$5,$6) RETURNING id::text,text,scope,status,version,added_at,applied_lesson_id::text`, owner, course, in.Text, in.Scope, verdict.ScreeningVersion, usage).Scan(&out.ID, &out.Text, &out.Scope, &out.Status, &out.Version, &out.AddedAt, &out.AppliedLessonID)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	s.emit(owner, "course_instruction_added", map[string]any{"course_id": course, "scope": in.Scope, "length_bucket": LengthBucket(utf8.RuneCountInString(in.Text)), "is_course_copy": false})
	return out, nil
}

// Latest supplies this ticket's single-entry status view. E16-02 adds list/edit/delete.
func (s *Service) Latest(ctx context.Context, owner, course string) (*Record, error) {
	if _, err := s.privateCourse(ctx, owner, course); err != nil {
		return nil, err
	}
	var r Record
	err := s.opts.Pool.QueryRow(ctx, `SELECT id::text,text,scope,status,version,added_at,applied_lesson_id::text FROM course_instructions WHERE learner_id=$1::uuid AND course_id=$2::uuid AND status<>'deleted' ORDER BY added_at DESC,id DESC LIMIT 1`, owner, course).Scan(&r.ID, &r.Text, &r.Scope, &r.Status, &r.Version, &r.AddedAt, &r.AppliedLessonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// ServeHTTP handles authenticated add and latest-status requests.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	owner := accounts.LearnerID(r.Context())
	course := r.PathValue("id")
	var value any
	var err error
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/latest"):
		var rec *Record
		rec, err = s.Latest(r.Context(), owner, course)
		value = map[string]any{"instruction": rec}
	case r.Method == http.MethodPost:
		var in Input
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil {
			err = refusal("invalid_input")
		} else {
			value, err = s.Add(r.Context(), owner, course, in)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		code := Code(err)
		status := 422
		switch code {
		case "not_found":
			status = 404
		case "rate_limit":
			status = 429
			w.Header().Set("Retry-After", "3600")
		case "screening_unavailable", "unavailable":
			status = 503
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"reason_code": code, "error": code})
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(value)
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
