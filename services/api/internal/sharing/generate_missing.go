package sharing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/lessontest"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
)

// Gate states of the next missing Lesson of a Course copy.
const (
	StateComplete   = "complete"   // every Lesson has content
	StateLocked     = "locked"     // the previous Lesson test is not passed yet
	StateReady      = "ready"      // a quote can be requested
	StateGenerating = "generating" // a generation is already queued or running
)

// Refusals of the missing-Lesson flow. Each maps to one neutral wire code.
var (
	ErrCopyNotFound         = errors.New("copy_not_found")
	ErrConsentRequired      = errors.New("consent_required")
	ErrQuoteStale           = errors.New("quote_stale")
	ErrPreviousNotPassed    = errors.New("previous_lesson_not_passed")
	ErrInsufficientCredits  = errors.New("insufficient_credits")
	ErrNothingToGenerate    = errors.New("nothing_to_generate")
	ErrGenerationInProgress = errors.New("generation_in_progress")
)

// Quoter is the Pricing quote store (pricing.Quotes).
type Quoter interface {
	Create(ctx context.Context, owner string, in pricing.QuoteInput) (pricing.Quote, error)
	Get(ctx context.Context, owner, id string) (pricing.Quote, error)
	Accept(ctx context.Context, owner, id string) error
}

// Wallet reads the learner's spendable balance (credits.Service).
type Wallet interface {
	GetWallet(ctx context.Context, learnerID string) (*credits.WalletResponse, error)
}

// Preparer queues the gated generation through the existing pipeline (lessontest.Service).
type Preparer interface {
	Prepare(ctx context.Context, owner, lessonID, quoteID string, inTx func(pgx.Tx, string) error) (string, error)
}

// GenerationDeps wires the reused private-generation pipeline into the Course copy flow.
type GenerationDeps struct {
	Quotes  Quoter
	Wallet  Wallet
	Lessons Preparer
}

// WithGeneration returns a copy of the service that can generate missing Lessons.
func (s *Service) WithGeneration(d GenerationDeps) *Service { c := *s; c.gen = d; return &c }

// CopyLesson is one Lesson of a copy in reading order.
type CopyLesson struct {
	ID         string
	Status     string
	HasContent bool
}

// MissingStatus reports ready/total and the gate for the next missing Lesson.
type MissingStatus struct {
	CourseID     string `json:"course_id"`
	Version      int    `json:"version"`
	Total        int    `json:"total"`
	Ready        int    `json:"ready"`
	Missing      int    `json:"missing"`
	State        string `json:"state"`
	NextLessonID string `json:"next_lesson_id,omitempty"`
}

// ClassifyCopy derives the counts and gate state from the copy's Lessons in order.
func ClassifyCopy(lessons []CopyLesson) MissingStatus {
	st := MissingStatus{Total: len(lessons), State: StateComplete}
	var next *CopyLesson
	for i := range lessons {
		if lessons[i].HasContent {
			st.Ready++
		} else if next == nil {
			next = &lessons[i]
		}
	}
	st.Missing = st.Total - st.Ready
	if next == nil {
		return st
	}
	st.NextLessonID = next.ID
	switch next.Status {
	case "generating":
		st.State = StateGenerating
	case "ready_to_generate":
		st.State = StateReady
	default:
		st.State = StateLocked
	}
	return st
}

// MissingQuote is the explicit, price-versioned quote the learner consents to.
type MissingQuote struct {
	QuoteID        string `json:"quote_id"`
	LessonID       string `json:"lesson_id"`
	PricingVersion string `json:"pricing_version"`
	ContentCredits int64  `json:"content_credits"`
	AudioCredits   int64  `json:"audio_credits"`
	TotalCredits   int64  `json:"total_credits"`
	Balance        int64  `json:"balance"`
	Sufficient     bool   `json:"sufficient"`
	ExpiresAt      string `json:"expires_at,omitempty"`
}

// BuildMissingQuote summarizes the quote's component ceilings against the balance.
func BuildMissingQuote(q pricing.Quote, lessonID string, balance int64) MissingQuote {
	total := q.ContentCeiling + q.AudioCeiling
	out := MissingQuote{QuoteID: q.ID, LessonID: lessonID, PricingVersion: q.PricingVersion, ContentCredits: q.ContentCeiling,
		AudioCredits: q.AudioCeiling, TotalCredits: total, Balance: balance, Sufficient: balance >= total}
	if !q.ExpiresAt.IsZero() {
		out.ExpiresAt = q.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}

// CheckQuote accepts only an unexpired lesson_content quote at the active Pricing config version.
func CheckQuote(q pricing.Quote, activeVersion string) error {
	if q.Action != "lesson_content" || q.Expired || activeVersion == "" || q.PricingVersion != activeVersion {
		return ErrQuoteStale
	}
	return nil
}

// GenerateMissingInput is the consent request. The actor is never part of it.
type GenerateMissingInput struct {
	LessonID string `json:"lesson_id"`
	QuoteID  string `json:"quote_id"`
	Consent  bool   `json:"consent"`
}

// Generation identifies the queued job.
type Generation struct {
	ID    string `json:"generation_id"`
	State string `json:"state"`
}

// copyStatus loads the actor's own copy; anything else is the neutral ErrCopyNotFound.
func (s *Service) copyStatus(ctx context.Context, actor, courseID string) (MissingStatus, error) {
	if !validID(actor) || !validID(courseID) {
		return MissingStatus{}, ErrCopyNotFound
	}
	var version int
	err := s.pool.QueryRow(ctx, `SELECT cc.version FROM course_copies cc JOIN courses c ON c.id=cc.copy_course_id
		WHERE cc.copy_course_id=$1 AND cc.learner_id=$2 AND c.learner_id=$2 AND c.deleted_at IS NULL`, courseID, actor).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return MissingStatus{}, ErrCopyNotFound
	}
	if err != nil {
		return MissingStatus{}, err
	}
	rows, err := s.pool.Query(ctx, `SELECT l.id::text, l.status, EXISTS(SELECT 1 FROM generated_lessons g WHERE g.lesson_stub_id=l.id)
		FROM course_lesson_stubs l JOIN course_units u ON u.id=l.unit_id WHERE u.course_id=$1 ORDER BY u.position, l.position`, courseID)
	if err != nil {
		return MissingStatus{}, err
	}
	defer rows.Close()
	var lessons []CopyLesson
	for rows.Next() {
		var l CopyLesson
		if err = rows.Scan(&l.ID, &l.Status, &l.HasContent); err != nil {
			return MissingStatus{}, err
		}
		lessons = append(lessons, l)
	}
	if err = rows.Err(); err != nil {
		return MissingStatus{}, err
	}
	st := ClassifyCopy(lessons)
	st.CourseID, st.Version = courseID, version
	return st, nil
}

// MissingStatus shows how many Lessons of the copy are ready and what gates the next one.
func (s *Service) MissingStatus(ctx context.Context, actor, courseID string) (MissingStatus, error) {
	return s.copyStatus(ctx, actor, courseID)
}

func gateError(st MissingStatus) error {
	switch st.State {
	case StateComplete:
		return ErrNothingToGenerate
	case StateGenerating:
		return ErrGenerationInProgress
	case StateLocked:
		return ErrPreviousNotPassed
	}
	return nil
}

// activeConfig reads the active Pricing config version and its Lesson narration bound.
func (s *Service) activeConfig(ctx context.Context) (version string, narration int64, err error) {
	row, err := db.New(s.pool).ActiveQuoteConfig(ctx)
	if err != nil {
		return "", 0, err
	}
	c, err := pricing.Parse(row.Config)
	if err != nil {
		return "", 0, err
	}
	var limits map[string]json.RawMessage
	var lesson map[string]int64
	if json.Unmarshal(c.Limits, &limits) == nil {
		_ = json.Unmarshal(limits["lesson_content"], &lesson)
	}
	return c.Version, lesson["narration_characters"], nil
}

// QuoteMissing creates a fresh lesson_content quote with narration at the active version. Nothing is
// reserved and nothing is charged; the previous Lesson test must already be passed.
func (s *Service) QuoteMissing(ctx context.Context, actor, courseID string) (MissingQuote, error) {
	if s.gen.Quotes == nil || s.gen.Wallet == nil {
		return MissingQuote{}, errors.New("generation not configured")
	}
	st, err := s.copyStatus(ctx, actor, courseID)
	if err != nil {
		return MissingQuote{}, err
	}
	if err = gateError(st); err != nil {
		return MissingQuote{}, err
	}
	_, chars, err := s.activeConfig(ctx)
	if err != nil {
		return MissingQuote{}, err
	}
	q, err := s.gen.Quotes.Create(ctx, actor, pricing.QuoteInput{Action: "lesson_content", NarrationCharacters: chars})
	if err != nil {
		return MissingQuote{}, fmt.Errorf("create quote: %w", err)
	}
	wallet, err := s.gen.Wallet.GetWallet(ctx, actor)
	if err != nil {
		return MissingQuote{}, err
	}
	return BuildMissingQuote(q, st.NextLessonID, wallet.Available), nil
}

// GenerateMissing queues the next missing Lesson for the actor's own copy. Every refusal happens
// before the Engine could be called; the River worker reserves, generates and settles.
func (s *Service) GenerateMissing(ctx context.Context, actor, courseID string, in GenerateMissingInput) (Generation, error) {
	if s.gen.Quotes == nil || s.gen.Wallet == nil || s.gen.Lessons == nil {
		return Generation{}, errors.New("generation not configured")
	}
	st, err := s.copyStatus(ctx, actor, courseID)
	if err != nil {
		return Generation{}, err
	}
	if !in.Consent {
		return Generation{}, ErrConsentRequired
	}
	if err = gateError(st); err != nil {
		return Generation{}, err
	}
	if in.LessonID != st.NextLessonID {
		return Generation{}, ErrNothingToGenerate
	}
	active, _, err := s.activeConfig(ctx)
	if err != nil {
		return Generation{}, err
	}
	q, err := s.gen.Quotes.Get(ctx, actor, in.QuoteID)
	if err != nil {
		return Generation{}, ErrQuoteStale
	}
	if err = CheckQuote(q, active); err != nil {
		return Generation{}, err
	}
	var used bool
	if err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lesson_generations WHERE quote_id=$1)`, in.QuoteID).Scan(&used); err != nil {
		return Generation{}, err
	}
	if used {
		return Generation{}, ErrQuoteStale
	}
	wallet, err := s.gen.Wallet.GetWallet(ctx, actor)
	if err != nil {
		return Generation{}, err
	}
	if wallet.Available < q.ContentCeiling+q.AudioCeiling {
		return Generation{}, ErrInsufficientCredits
	}
	if err = s.gen.Quotes.Accept(ctx, actor, in.QuoteID); err != nil {
		return Generation{}, ErrQuoteStale
	}
	id, err := s.gen.Lessons.Prepare(ctx, actor, in.LessonID, in.QuoteID, func(tx pgx.Tx, generationID string) error {
		_, e := tx.Exec(ctx, `INSERT INTO copy_generation_consents(generation_id, learner_id, copy_course_id, lesson_id, quote_id, pricing_version)
			VALUES($1,$2,$3,$4,$5,$6)`, generationID, actor, courseID, in.LessonID, in.QuoteID, q.PricingVersion)
		return e
	})
	switch {
	case err == nil:
		return Generation{ID: id, State: "queued"}, nil
	case errors.Is(err, lessontest.ErrQuoteInvalid):
		return Generation{}, ErrQuoteStale
	case errors.Is(err, lessontest.ErrPredecessorNotPassed):
		return Generation{}, ErrPreviousNotPassed
	case errors.Is(err, lessontest.ErrGenerationInProgress), errors.Is(err, lessontest.ErrNotReadyToGenerate):
		return Generation{}, ErrGenerationInProgress
	case errors.Is(err, lessontest.ErrLessonNotFound):
		return Generation{}, ErrCopyNotFound
	}
	return Generation{}, err
}

// MissingGenerator is what the routes need from the service.
type MissingGenerator interface {
	MissingStatus(ctx context.Context, actor, courseID string) (MissingStatus, error)
	QuoteMissing(ctx context.Context, actor, courseID string) (MissingQuote, error)
	GenerateMissing(ctx context.Context, actor, courseID string, in GenerateMissingInput) (Generation, error)
}

// MissingHandler serves the missing-Lesson routes. The actor always comes from the session.
type MissingHandler struct{ g MissingGenerator }

// NewMissingHandler wires the routes.
func NewMissingHandler(g MissingGenerator) http.Handler { return &MissingHandler{g: g} }

func (h *MissingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	actor := accounts.LearnerID(r.Context())
	if actor == "" {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	course := r.PathValue("id")
	switch {
	case r.Method == http.MethodGet:
		st, err := h.g.MissingStatus(r.Context(), actor, course)
		h.reply(w, r, http.StatusOK, st, err)
	case strings.HasSuffix(r.URL.Path, "/quote"):
		q, err := h.g.QuoteMissing(r.Context(), actor, course)
		h.reply(w, r, http.StatusCreated, q, err)
	default:
		var in GenerateMissingInput
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
			return
		}
		gen, err := h.g.GenerateMissing(r.Context(), actor, course, in)
		h.reply(w, r, http.StatusAccepted, gen, err)
	}
}

func (h *MissingHandler) reply(w http.ResponseWriter, r *http.Request, ok int, v any, err error) {
	if err == nil {
		write(w, ok, v)
		return
	}
	codes := []struct {
		err    error
		status int
	}{
		{ErrCopyNotFound, http.StatusNotFound}, {ErrConsentRequired, http.StatusBadRequest},
		{ErrQuoteStale, http.StatusConflict}, {ErrPreviousNotPassed, http.StatusConflict},
		{ErrNothingToGenerate, http.StatusConflict}, {ErrGenerationInProgress, http.StatusConflict},
		{ErrInsufficientCredits, http.StatusPaymentRequired},
	}
	for _, c := range codes {
		if errors.Is(err, c.err) {
			write(w, c.status, map[string]string{"error": c.err.Error()})
			return
		}
	}
	slog.ErrorContext(r.Context(), "missing lesson request failed", "error", err)
	write(w, http.StatusServiceUnavailable, map[string]string{"error": "sharing_unavailable"})
}

func mountMissing(mux *http.ServeMux, s *Service, protect func(http.Handler) http.Handler) {
	h := protect(NewMissingHandler(s))
	for _, p := range []string{
		"GET /v1/courses/{id}/missing-lessons", "POST /v1/courses/{id}/missing-lessons/quote",
		"POST /v1/courses/{id}/missing-lessons/generate",
	} {
		mux.Handle(p, h)
	}
}
