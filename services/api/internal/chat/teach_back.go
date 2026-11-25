package chat

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/engine"
	"github.com/datamonsterr/duolify/services/api/internal/player"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	teachBackFollowUps  = 2
	teachBackDone       = teachBackFollowUps + 1
	teachBackTextRunes  = 2000
	teachBackLease      = 35 * time.Second
	teachBackRunTimeout = 22 * time.Second
)

type teachBackReply struct {
	Turn     int               `json:"turn"`
	FollowUp string            `json:"followUp,omitempty"`
	Summary  *teachBackSummary `json:"summary,omitempty"`
}

type teachBackSummary struct {
	Covered   []teachBackPointView `json:"covered"`
	Missed    []teachBackPointView `json:"missed"`
	SelfCheck bool                 `json:"selfCheck,omitempty"`
}

type teachBackPointView struct {
	Text       string `json:"text"`
	SlideIndex int32  `json:"slideIndex"`
}

type teachBackClaim struct {
	state    teachBackReply
	messages []*enginev1.ChatMessage
	token    string
	expired  bool
}

var errTeachBackTurn = errors.New("teach back turn conflict")

func summarizeTeachBack(points []*enginev1.TeachBackPoint, result *enginev1.TeachBackResponse) *teachBackSummary {
	summary := &teachBackSummary{Covered: []teachBackPointView{}, Missed: []teachBackPointView{}, SelfCheck: result.GetSelfCheck() || !result.GetComplete()}
	covered := make(map[string]bool, len(points))
	for _, id := range result.GetCoveredPointIds() {
		covered[id] = true
	}
	for _, p := range points {
		public := teachBackPointView{Text: p.GetText(), SlideIndex: p.GetSlideIndex()}
		if !summary.SelfCheck && covered[p.GetId()] {
			summary.Covered = append(summary.Covered, public)
		} else {
			summary.Missed = append(summary.Missed, public)
		}
	}
	return summary
}

func (s *Service) teachBack(w http.ResponseWriter, r *http.Request, owner, course, lesson string) {
	ctx := r.Context()
	if !validUUID(lesson) || !s.ownsCourse(ctx, owner, course) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	var title string
	var rawPlan []byte
	err := s.opts.Pool.QueryRow(ctx, `SELECT l.title,COALESCE(to_jsonb(l)->'plan'->'key_points',to_jsonb(l)->'plan'->'keyPoints','[]'::jsonb)
		FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id AND u.learner_id=l.learner_id
		WHERE l.id=$1::uuid AND l.learner_id=$2::uuid AND u.course_id=$3::uuid AND l.status='done'`, lesson, owner, course).Scan(&title, &rawPlan)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if r.Method == http.MethodGet {
		s.readTeachBack(ctx, w, owner, lesson)
		return
	}
	var body struct {
		Turn        *int   `json:"turn"`
		Explanation string `json:"explanation"`
	}
	if !decode(w, r, &body) {
		return
	}
	text := strings.TrimSpace(body.Explanation)
	if body.Turn == nil || *body.Turn < 0 || *body.Turn > teachBackFollowUps || text == "" || len(text) > teachBackTextRunes*utf8.UTFMax || utf8.RuneCountInString(text) > teachBackTextRunes || strings.ContainsRune(text, 0) {
		fail(w, http.StatusBadRequest, "invalid_request")
		return
	}
	var plan []string
	_ = json.Unmarshal(rawPlan, &plan)
	s.submitTeachBack(w, r, owner, course, lesson, title, text, *body.Turn, plan)
}

func (s *Service) readTeachBack(ctx context.Context, w http.ResponseWriter, owner, lesson string) {
	var state teachBackReply
	var saved []byte
	err := s.opts.Pool.QueryRow(ctx, `SELECT response FROM lesson_teach_backs WHERE learner_id=$1::uuid AND lesson_stub_id=$2::uuid`, owner, lesson).Scan(&saved)
	if !errors.Is(err, pgx.ErrNoRows) && (err != nil || json.Unmarshal(saved, &state) != nil) {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	respond(w, http.StatusOK, state)
}

// claimTeachBack takes a short database lease, leaving no open connection across model calls.
// A finished turn is replayed for duplicate submissions. If a worker lost its lease after a
// crash, the next request moves to self-check instead of paying to repeat uncertain work.
func (s *Service) claimTeachBack(ctx context.Context, owner, lesson string, expected int) (teachBackClaim, error) {
	var claim teachBackClaim
	tx, err := s.opts.Pool.Begin(ctx)
	if err != nil {
		return claim, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if expected == 0 {
		if _, err = tx.Exec(ctx, `INSERT INTO lesson_teach_backs(learner_id,lesson_stub_id) VALUES($1::uuid,$2::uuid)
			ON CONFLICT (learner_id,lesson_stub_id) DO NOTHING`, owner, lesson); err != nil {
			return claim, err
		}
	}
	var current int
	var messages, response []byte
	var active bool
	err = tx.QueryRow(ctx, `SELECT turn,messages,response,COALESCE(processing_until>now(),false),COALESCE(processing_until<=now(),false)
		FROM lesson_teach_backs WHERE learner_id=$1::uuid AND lesson_stub_id=$2::uuid FOR UPDATE`, owner, lesson).
		Scan(&current, &messages, &response, &active, &claim.expired)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (expected > current || (expected == current && active))) {
		return claim, errTeachBackTurn
	}
	if err != nil {
		return claim, err
	}
	if expected < current {
		return claim, json.Unmarshal(response, &claim.state)
	}
	if err := json.Unmarshal(messages, &claim.messages); err != nil {
		return claim, err
	}
	claim.token = uuid.NewString()
	if _, err = tx.Exec(ctx, `UPDATE lesson_teach_backs SET processing_token=$3::uuid,processing_until=now()+make_interval(secs=>$4)
		WHERE learner_id=$1::uuid AND lesson_stub_id=$2::uuid`, owner, lesson, claim.token, teachBackLease.Seconds()); err != nil {
		return claim, err
	}
	return claim, tx.Commit(ctx)
}

func (s *Service) submitTeachBack(w http.ResponseWriter, r *http.Request, owner, course, lesson, title, text string, turn int, plan []string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), teachBackRunTimeout)
	defer cancel()
	claim, err := s.claimTeachBack(ctx, owner, lesson, turn)
	if errors.Is(err, errTeachBackTurn) {
		fail(w, http.StatusConflict, "teach_back_turn_conflict")
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if claim.token == "" {
		respond(w, http.StatusOK, claim.state)
		return
	}
	// Release the claim for failures before invoking the model. Once a model was called, retain
	// a failed write's lease so recovery never repeats a possibly paid call.
	release := !claim.expired
	defer func() {
		if release {
			finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
			defer cancel()
			_, _ = s.opts.Pool.Exec(finish, `UPDATE lesson_teach_backs SET processing_token=NULL,processing_until=NULL
				WHERE learner_id=$1::uuid AND lesson_stub_id=$2::uuid AND processing_token=$3::uuid`, owner, lesson, claim.token)
		}
	}()
	points, err := s.teachBackPoints(ctx, owner, lesson, plan)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if len(points) == 0 {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	result := &enginev1.TeachBackResponse{Complete: true, SelfCheck: true}
	messages := append(claim.messages, &enginev1.ChatMessage{Role: "user", Content: text})
	if !claim.expired && s.opts.Engine != nil {
		profile, err := s.opts.Profiles.EffectiveProfile(ctx, owner, course)
		if err != nil {
			fail(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		var persona string
		if s.opts.Personas != nil {
			if pin, err := s.opts.Personas.ResolveTutor(ctx, owner, course, lesson, ""); err == nil {
				persona = pin.Persona
			}
		}
		release = false
		res, err := s.opts.Engine.TeachBack(ctx, &enginev1.TeachBackRequest{
			LessonTitle: title, Points: points, Messages: messages, Learner: profile, TutorPersonaJson: persona,
			FollowUps: u32(int64(turn)), Limits: teachBackLimits(),
		})
		if blocked, ok := engine.Blocked(err); ok {
			release = true
			body := map[string]any{"error": "input_blocked", "reason_code": blocked.ReasonCode}
			if blocked.SafeResponse() {
				body["safe_response"] = SafeResponseCopy(r.Header.Get("Accept-Language"))
			}
			respond(w, http.StatusUnprocessableEntity, body)
			return
		}
		if err == nil && res != nil {
			result = res
		}
	}
	response := teachBackReply{Turn: teachBackDone}
	// Only the API can advance the turn. A malformed follow-up or a provider outage goes to
	// the same self-check as an invalid grade, without delaying Lesson progress.
	if followUp := strings.TrimSpace(result.GetFollowUp()); !result.GetComplete() && turn < teachBackFollowUps && followUp != "" && utf8.RuneCountInString(followUp) <= maxPointRunes && player.PlainText(followUp) {
		response.Turn, response.FollowUp = turn+1, followUp
		messages = append(messages, &enginev1.ChatMessage{Role: "assistant", Content: followUp})
	} else {
		response.Summary = summarizeTeachBack(points, result)
		messages = []*enginev1.ChatMessage{}
	}
	finish, cancelFinish := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Second)
	defer cancelFinish()
	saved, _ := json.Marshal(response)
	transcript, _ := json.Marshal(messages)
	tag, err := s.opts.Pool.Exec(finish, `UPDATE lesson_teach_backs SET turn=$4,messages=$5,response=$6,
			processing_token=NULL,processing_until=NULL,updated_at=now()
		WHERE learner_id=$1::uuid AND lesson_stub_id=$2::uuid AND processing_token=$3::uuid`, owner, lesson, claim.token, response.Turn, transcript, saved)
	if err != nil || tag.RowsAffected() != 1 {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	release = false
	if response.Summary != nil {
		s.emit(finish, owner, "teach_back_completed", map[string]any{"lesson_id": lesson, "coverage": float64(len(response.Summary.Covered)) / float64(len(points))})
	}
	respond(w, http.StatusOK, response)
}

// Each Lesson can use at most three separately bounded calls. These limits cover the
// complete short transcript even when it is written in a multibyte Lesson language.
func teachBackLimits() *enginev1.UsageLimits {
	return &enginev1.UsageLimits{InputTokens: 24576, OutputTokens: 256, TimeoutMs: 12000, MaxAttempts: 2,
		MaxCostNanos: 1500000, InputNanos: 40, OutputNanos: 141, CacheWriteNanos: 48, CacheReadNanos: 10}
}
