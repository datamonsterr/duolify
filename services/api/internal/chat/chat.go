// Package chat serves the paid, quoted Course-level tutor: quote, SSE reply, thread reads and deletion.
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/pricing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	shortMessageBytes = 2000
	longMessageBytes  = 4000
	recentMessages    = 10
	// Streaming deadlines travel in proto UsageLimits; the Engine enforces them.
	firstTokenTimeoutMs = 15000
	idleTimeoutMs       = 5000
	// A pending turn this old belongs to a crashed request; its hold is released by reservation maintenance.
	stalePending = 5 * time.Minute
)

// Engine opens the tutor stream.
type Engine interface {
	Chat(context.Context, *enginev1.ChatRequest) (enginev1.EngineService_ChatClient, error)
}

// Financial is the E7 reservation boundary.
type Financial interface {
	ReserveAction(ctx context.Context, owner, quoteID, action string) (credits.Reservation, error)
	Limits(ctx context.Context, owner, id, action string) (*enginev1.UsageLimits, error)
	Claim(ctx context.Context, owner, id string) error
	Release(ctx context.Context, owner, id string) (credits.Reservation, error)
	Settle(ctx context.Context, owner, id string, usage []*enginev1.Usage) (credits.Reservation, error)
	SettleWith(ctx context.Context, owner, id string, usage []*enginev1.Usage, commit func(pgx.Tx, credits.Reservation) error) (credits.Reservation, error)
}

// Profiles projects the effective Learner profile for a Course.
type Profiles interface {
	EffectiveProfile(ctx context.Context, learnerID, courseID string) (*enginev1.LearnerProfile, error)
}

// Options wires the service; every dependency is required except Events and Logger.
type Options struct {
	Pool      *pgxpool.Pool
	Engine    Engine
	Financial Financial
	Profiles  Profiles
	Events    analytics.Events
	Logger    *slog.Logger
}

// Service owns tutor threads, messages and turns.
type Service struct {
	opts    Options
	flights flights
}

// New builds the tutor service.
func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Service{opts: o}
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]any{"error": code})
}

func classAction(class string) string {
	switch class {
	case "short":
		return "tutor_short"
	case "long":
		return "tutor_long"
	}
	return ""
}

func estimateTokens(bytes int) int64 { return int64((bytes + 3) / 4) }

func validateMessage(class, message string) error {
	limit := shortMessageBytes
	switch class {
	case "short":
	case "long":
		limit = longMessageBytes
	default:
		return errors.New("invalid class")
	}
	if !utf8.ValidString(message) || strings.TrimSpace(message) == "" || strings.ContainsRune(message, 0) || len(message) > limit {
		return errors.New("invalid message")
	}
	return nil
}

func validUUID(s string) bool { _, err := uuid.Parse(s); return err == nil }

// ServeHTTP expects the caller to wrap it in authentication.
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	owner := accounts.LearnerID(r.Context())
	if owner == "" {
		fail(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.opts.Pool == nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	id := r.PathValue("id")
	if !validUUID(id) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	path := r.URL.Path
	switch {
	case r.PathValue("key") != "" && r.Method == http.MethodPost && strings.HasSuffix(path, "/stop"):
		s.stop(w, r, owner, id, r.PathValue("key"))
	case r.PathValue("key") != "" && r.Method == http.MethodGet:
		s.turn(w, r, owner, id, r.PathValue("key"))
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/tutor/quote"):
		s.quote(w, r, owner, id)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/tutor/messages"):
		s.send(w, r, owner, id)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/tutor/thread"):
		s.courseThread(w, r, owner, id)
	case r.Method == http.MethodGet && strings.HasSuffix(path, "/messages"):
		s.threadMessages(w, r, owner, id)
	case r.Method == http.MethodDelete:
		s.deleteThread(w, r, owner, id)
	default:
		fail(w, http.StatusNotFound, "not_found")
	}
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	var extra json.RawMessage
	if dec.Decode(into) != nil || dec.Decode(&extra) != io.EOF {
		fail(w, http.StatusBadRequest, "invalid_request")
		return false
	}
	return true
}

func (s *Service) ownsCourse(ctx context.Context, owner, course string) bool {
	var ok bool
	return s.opts.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM courses c JOIN course_enrollments e ON e.course_id=c.id WHERE c.id=$1::uuid AND e.learner_id=$2::uuid AND c.learner_id=$2::uuid AND c.deleted_at IS NULL)`, course, owner).Scan(&ok) == nil && ok
}

func (s *Service) quote(w http.ResponseWriter, r *http.Request, owner, course string) {
	var in struct {
		Class   string `json:"class"`
		Message string `json:"message"`
	}
	if !decode(w, r, &in) {
		return
	}
	if validateMessage(in.Class, in.Message) != nil {
		fail(w, http.StatusBadRequest, "invalid_message")
		return
	}
	if !s.ownsCourse(r.Context(), owner, course) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	thread, err := s.latestThread(r.Context(), owner, course)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	req, err := s.assemble(r.Context(), owner, course, thread, in.Message, 1<<30)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	q, err := pricing.NewQuotes(s.opts.Pool).Create(r.Context(), owner, pricing.QuoteInput{
		Action:        classAction(in.Class),
		InputTokens:   estimateTokens(len(in.Message)),
		ContextTokens: estimateTokens(max(0, promptBytes(req)-len(in.Message))),
	})
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "quote_unavailable")
		return
	}
	respond(w, http.StatusOK, q)
}

type messageDTO struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type threadDTO struct {
	ID        string    `json:"id"`
	CourseID  string    `json:"course_id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Service) latestThread(ctx context.Context, owner, course string) (string, error) {
	var id string
	err := s.opts.Pool.QueryRow(ctx, `SELECT id::text FROM tutor_threads WHERE learner_id=$1::uuid AND course_id=$2::uuid ORDER BY created_at DESC,id DESC LIMIT 1`, owner, course).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func (s *Service) readThread(ctx context.Context, owner, threadID string) (*threadDTO, []messageDTO, error) {
	var t threadDTO
	if err := s.opts.Pool.QueryRow(ctx, `SELECT id::text,course_id::text,created_at FROM tutor_threads WHERE id=$1::uuid AND learner_id=$2::uuid`, threadID, owner).Scan(&t.ID, &t.CourseID, &t.CreatedAt); err != nil {
		return nil, nil, err
	}
	rows, err := s.opts.Pool.Query(ctx, `SELECT id::text,role,content,status,created_at FROM tutor_messages WHERE thread_id=$1::uuid AND learner_id=$2::uuid ORDER BY seq`, threadID, owner)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	messages := []messageDTO{}
	for rows.Next() {
		var m messageDTO
		if err = rows.Scan(&m.ID, &m.Role, &m.Content, &m.Status, &m.CreatedAt); err != nil {
			return nil, nil, err
		}
		messages = append(messages, m)
	}
	return &t, messages, rows.Err()
}

func (s *Service) courseThread(w http.ResponseWriter, r *http.Request, owner, course string) {
	if !s.ownsCourse(r.Context(), owner, course) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	id, err := s.latestThread(r.Context(), owner, course)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if id == "" {
		respond(w, http.StatusOK, map[string]any{"thread": nil, "messages": []messageDTO{}})
		return
	}
	t, messages, err := s.readThread(r.Context(), owner, id)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	respond(w, http.StatusOK, map[string]any{"thread": t, "messages": messages})
}

func (s *Service) threadMessages(w http.ResponseWriter, r *http.Request, owner, thread string) {
	t, messages, err := s.readThread(r.Context(), owner, thread)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	respond(w, http.StatusOK, map[string]any{"thread": t, "messages": messages})
}

func (s *Service) deleteThread(w http.ResponseWriter, r *http.Request, owner, thread string) {
	tag, err := s.opts.Pool.Exec(r.Context(), `DELETE FROM tutor_threads WHERE id=$1::uuid AND learner_id=$2::uuid`, thread, owner)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// emit sends identifiers and counts only, honouring the learner's analytics opt-out.
func (s *Service) emit(ctx context.Context, owner, event string, props map[string]any) {
	if s.opts.Events == nil {
		return
	}
	var optOut bool
	if s.opts.Pool.QueryRow(ctx, `SELECT analytics_opt_out FROM learner_profiles WHERE learner_id=$1::uuid`, owner).Scan(&optOut) != nil || optOut {
		return
	}
	props["source"], props["platform"], props["app_version"] = "api", "web", "0.1.0"
	if err := s.opts.Events.Capture(owner, event, props); err != nil {
		s.opts.Logger.Warn("tutor analytics capture failed", "event", event)
	}
}
