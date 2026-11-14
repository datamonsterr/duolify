package steering

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/jackc/pgx/v5"
)

// Limits are the configured steering bounds shown to the Learner.
type Limits struct {
	MaxCharacters      int `json:"max_characters"`
	MaxActive          int `json:"max_active"`
	MaxTotalCharacters int `json:"max_total_characters"`
	AddsPerHour        int `json:"adds_per_hour"`
}

// ListResult is the owner's non-deleted instructions, oldest first (newer wins on conflict).
type ListResult struct {
	Instructions     []Record `json:"instructions"`
	Limits           Limits   `json:"limits"`
	ActiveCount      int      `json:"active_count"`
	ActiveCharacters int      `json:"active_characters"`
	// Fork is set only for a published copy (E16-05).
	Fork *ForkInfo `json:"fork,omitempty"`
}

// EditInput changes text and/or scope of an active instruction. ExpectedVersion guards lost updates.
type EditInput struct {
	ExpectedVersion *uint32 `json:"expected_version"`
	Text            *string `json:"text"`
	Scope           *string `json:"scope"`
}

// Validate runs the checks that need no storage; it returns a closed reason code or "".
func (in EditInput) Validate(maxChars int) string {
	if in.ExpectedVersion == nil || (in.Text == nil && in.Scope == nil) {
		return "invalid_input"
	}
	if in.Scope != nil && *in.Scope != "next_lesson" && *in.Scope != "rest_of_course" {
		return "invalid_scope"
	}
	if in.Text != nil {
		return ValidateText(*in.Text, maxChars)
	}
	return ""
}

// Delivery tells how an instruction meets generation. Only active instructions have one.
func Delivery(scope, status string, inRunningJob, jobRunning bool) string {
	switch {
	case status != "active":
		return ""
	case inRunningJob:
		return "running"
	case jobRunning && scope == "next_lesson":
		return "waits"
	}
	return "ready"
}

const recordColumns = `i.id::text,i.text,i.scope,i.status,i.version,i.added_at,i.applied_lesson_id::text`

func (s *Service) limits(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (l Limits, err error) {
	err = q.QueryRow(ctx, `SELECT max_characters,max_active,max_total_characters,adds_per_hour FROM steering_config WHERE singleton`).Scan(&l.MaxCharacters, &l.MaxActive, &l.MaxTotalCharacters, &l.AddsPerHour)
	return
}

// Latest supplies the single-entry status view kept from E16-01.
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

// List returns every non-deleted instruction with its delivery state and the configured limits.
func (s *Service) List(ctx context.Context, owner, course string) (ListResult, error) {
	out := ListResult{Instructions: []Record{}}
	if _, err := s.privateCourse(ctx, owner, course); err != nil {
		return out, err
	}
	var err error
	if out.Limits, err = s.limits(ctx, s.opts.Pool); err != nil {
		return out, err
	}
	if out.Fork, err = s.forkInfo(ctx, owner, course); err != nil {
		return out, err
	}
	rows, err := s.opts.Pool.Query(ctx, `SELECT `+recordColumns+`,
 EXISTS(SELECT 1 FROM lesson_instruction_snapshots sn JOIN lesson_generations g ON g.id=sn.generation_id WHERE sn.instruction_id=i.id AND sn.version=i.version AND g.state IN ('queued','generating','validated')),
 EXISTS(SELECT 1 FROM lesson_generations g WHERE g.course_id=i.course_id AND g.learner_id=i.learner_id AND g.state IN ('queued','generating','validated') AND g.instruction_snapshot_version=1)
 FROM course_instructions i WHERE i.learner_id=$1::uuid AND i.course_id=$2::uuid AND i.status<>'deleted' ORDER BY i.added_at,i.id`, owner, course)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var r Record
		var pinned, running bool
		if err = rows.Scan(&r.ID, &r.Text, &r.Scope, &r.Status, &r.Version, &r.AddedAt, &r.AppliedLessonID, &pinned, &running); err != nil {
			return out, err
		}
		r.Delivery = Delivery(r.Scope, r.Status, pinned, running)
		if r.Status == "active" {
			out.ActiveCount++
			out.ActiveCharacters += utf8.RuneCountInString(r.Text)
		}
		out.Instructions = append(out.Instructions, r)
	}
	return out, rows.Err()
}

// Edit changes future use only: the text is re-screened when it changes, the row version moves
// forward, added_at (conflict precedence) stays, and job snapshots keep their own copy.
func (s *Service) Edit(ctx context.Context, owner, course, id string, in EditInput) (Record, error) {
	var out Record
	topic, err := s.privateCourse(ctx, owner, course)
	if err != nil {
		return out, err
	}
	lim, err := s.limits(ctx, s.opts.Pool)
	if err != nil {
		return out, err
	}
	reject := func(code string) (Record, error) {
		s.emit(owner, "course_instruction_rejected", map[string]any{"course_id": course, "reason_code": code})
		return out, refusal(code)
	}
	if code := in.Validate(lim.MaxCharacters); code != "" {
		if code == "invalid_input" {
			return out, refusal(code)
		}
		return reject(code)
	}
	var verdictUsage []byte
	var screeningVersion string
	if in.Text != nil {
		verdict, code := s.screen(ctx, owner, topic, *in.Text)
		if code != "" {
			return reject(code)
		}
		verdictUsage, _ = json.Marshal(verdict.Attempts)
		screeningVersion = verdict.ScreeningVersion
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// The Course lock orders edits, adds and job snapshots across API instances.
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM courses WHERE id=$1::uuid AND learner_id=$2::uuid FOR UPDATE`, course, owner).Scan(&locked); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT `+recordColumns+` FROM course_instructions i WHERE i.id=$1::uuid AND i.course_id=$2::uuid AND i.learner_id=$3::uuid AND i.status<>'deleted' FOR UPDATE`, id, course, owner).Scan(&out.ID, &out.Text, &out.Scope, &out.Status, &out.Version, &out.AddedAt, &out.AppliedLessonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, refusal("not_found")
	}
	if err != nil {
		return Record{}, err
	}
	if out.Status != "active" {
		return Record{}, refusal("not_editable")
	}
	if out.Version != *in.ExpectedVersion {
		return Record{}, refusal("version_conflict")
	}
	newText, newScope := out.Text, out.Scope
	if in.Text != nil {
		newText = *in.Text
	}
	if in.Scope != nil {
		newScope = *in.Scope
	}
	if newText == out.Text && newScope == out.Scope {
		out.Delivery = "ready"
		return out, nil
	}
	var others int
	if err = tx.QueryRow(ctx, `SELECT COALESCE(sum(char_length(text)),0) FROM course_instructions WHERE course_id=$1::uuid AND learner_id=$2::uuid AND status='active' AND id<>$3::uuid`, course, owner, id).Scan(&others); err != nil {
		return Record{}, err
	}
	if others+utf8.RuneCountInString(newText) > lim.MaxTotalCharacters {
		return reject("total_length_limit")
	}
	textChanged, scopeChanged := newText != out.Text, newScope != out.Scope
	if verdictUsage == nil {
		verdictUsage = []byte("null")
	}
	err = tx.QueryRow(ctx, `UPDATE course_instructions SET text=$2,scope=$3,version=version+1,
 screening_version=CASE WHEN $4::boolean THEN $5 ELSE screening_version END,
 screening_usage=CASE WHEN $4::boolean THEN $6::jsonb ELSE screening_usage END
 WHERE id=$1::uuid RETURNING id::text,text,scope,status,version,added_at,applied_lesson_id::text`, id, newText, newScope, textChanged, screeningVersion, verdictUsage).Scan(&out.ID, &out.Text, &out.Scope, &out.Status, &out.Version, &out.AddedAt, &out.AppliedLessonID)
	if err != nil {
		return Record{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Record{}, err
	}
	changed := "scope"
	switch {
	case textChanged && scopeChanged:
		changed = "both"
	case textChanged:
		changed = "text"
	}
	s.emit(owner, "course_instruction_edited", map[string]any{"course_id": course, "scope": newScope, "length_bucket": LengthBucket(utf8.RuneCountInString(newText)), "changed": changed})
	out.Delivery = "ready"
	return out, nil
}

// Delete hides the instruction from every future job (soft delete, purged after 30 days).
// Snapshots of started jobs and lessons that used it are untouched.
func (s *Service) Delete(ctx context.Context, owner, course, id string) error {
	if _, err := s.privateCourse(ctx, owner, course); err != nil {
		return err
	}
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var locked string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM courses WHERE id=$1::uuid AND learner_id=$2::uuid FOR UPDATE`, course, owner).Scan(&locked); err != nil {
		return err
	}
	var scope, status string
	err = tx.QueryRow(ctx, `UPDATE course_instructions SET status='deleted',deleted_at=clock_timestamp() WHERE id=$1::uuid AND course_id=$2::uuid AND learner_id=$3::uuid AND status<>'deleted' RETURNING scope,status`, id, course, owner).Scan(&scope, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return refusal("not_found")
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.emit(owner, "course_instruction_deleted", map[string]any{"course_id": course, "scope": scope})
	return nil
}

func statusFor(code string) int {
	switch code {
	case "not_found":
		return 404
	case "rate_limit":
		return 429
	case "version_conflict", "not_editable", "fork_confirmation_required":
		return 409
	case "quote_invalid", "protected_lesson_changed", "invalid_proposal":
		return 409
	case "insufficient_credits":
		return 402
	case "replan_unavailable", "generation_failed":
		return 503
	case "screening_unavailable", "unavailable":
		return 503
	}
	return 422
}

// ServeHTTP handles list, add, edit, delete and latest-status requests for the authenticated owner.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if strings.Contains(r.URL.Path, "/replan") {
		s.serveReplan(w, r)
		return
	}
	owner := accounts.LearnerID(r.Context())
	course, id := r.PathValue("id"), r.PathValue("instructionId")
	var value any
	var err error
	created := false
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/latest"):
		var rec *Record
		rec, err = s.Latest(r.Context(), owner, course)
		value = map[string]any{"instruction": rec}
	case r.Method == http.MethodGet && id == "":
		value, err = s.List(r.Context(), owner, course)
	case r.Method == http.MethodPost && id == "":
		var in Input
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil {
			err = refusal("invalid_input")
		} else {
			value, err = s.Add(r.Context(), owner, course, in)
			created = err == nil
		}
	case r.Method == http.MethodPatch && id != "":
		var in EditInput
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || in.ExpectedVersion == nil || (in.Text == nil && in.Scope == nil) {
			err = refusal("invalid_input")
		} else {
			value, err = s.Edit(r.Context(), owner, course, id, in)
		}
	case r.Method == http.MethodDelete && id != "":
		if err = s.Delete(r.Context(), owner, course, id); err == nil {
			value = map[string]string{"id": id, "status": "deleted"}
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		code := Code(err)
		if code == "rate_limit" {
			w.Header().Set("Retry-After", "3600")
		}
		w.WriteHeader(statusFor(code))
		_ = json.NewEncoder(w).Encode(map[string]string{"reason_code": code, "error": code})
		return
	}
	if created {
		w.WriteHeader(http.StatusCreated)
	}
	_ = json.NewEncoder(w).Encode(value)
}
