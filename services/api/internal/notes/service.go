// Package notes implements persistence and retrieval of raw quick notes.
package notes

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	enginev1 "github.com/datamonsterr/duolify/services/api/gen/duolify/engine/v1"
	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

var (
	// ErrMissingActionKey indicates that the action key was not provided.
	ErrMissingActionKey = errors.New("missing action key")
	// ErrInvalidText indicates that the note text was empty or whitespace.
	ErrInvalidText = errors.New("invalid note text")
	// ErrTextTooLong indicates that the note text exceeded the 2000 character limit.
	ErrTextTooLong = errors.New("note text exceeds 2000 characters")
	// ErrInvalidSource indicates an unknown note source type.
	ErrInvalidSource = errors.New("invalid note source")
	// ErrInvalidSlide indicates that the slide ID was missing or negative.
	ErrInvalidSlide = errors.New("invalid slide id")
	// ErrUnauthorizedSource indicates the learner lacks permission for the referenced lesson.
	ErrUnauthorizedSource = errors.New("unauthorized lesson or slide source")
	// ErrNotFound indicates the requested note could not be found.
	ErrNotFound = errors.New("note not found")
)

// SaveNoteInput contains the fields needed to persist a raw quick note.
type SaveNoteInput struct {
	ActionKey  string  `json:"action_key"`
	CourseID   *string `json:"course_id,omitempty"`
	LessonID   *string `json:"lesson_id,omitempty"`
	SlideID    *int    `json:"slide_id,omitempty"`
	SlideTitle string  `json:"slide_title,omitempty"`
	Source     string  `json:"source"`
	RawText    string  `json:"raw_text"`
	// Chat links a note to the tutor reply it was saved from; the chat package sets it.
	Chat *ChatProvenance `json:"-"`
}

// Validate checks the input fields against boundary constraints.
func (in SaveNoteInput) Validate() error {
	if strings.TrimSpace(in.ActionKey) == "" {
		return ErrMissingActionKey
	}
	trimmed := strings.TrimSpace(in.RawText)
	if trimmed == "" {
		return ErrInvalidText
	}
	if len(in.RawText) > 2000 {
		return ErrTextTooLong
	}
	switch in.Source {
	case "slide", "lesson", "chat":
	default:
		return ErrInvalidSource
	}
	if in.Source == "slide" {
		if in.SlideID == nil || *in.SlideID < 0 || *in.SlideID > math.MaxInt32 {
			return ErrInvalidSlide
		}
	}
	return nil
}

// NoteDTO represents a quick note returned by the API.
type NoteDTO struct {
	ID         string         `json:"id"`
	LearnerID  string         `json:"learner_id"`
	ActionKey  string         `json:"action_key"`
	CourseID   *string        `json:"course_id,omitempty"`
	LessonID   *string        `json:"lesson_id,omitempty"`
	SlideID    *int           `json:"slide_id,omitempty"`
	SlideTitle string         `json:"slide_title"`
	Source     string         `json:"source"`
	RawText    string         `json:"raw_text"`
	RawVersion int            `json:"raw_version,omitempty"`
	Status     string         `json:"status"`
	Reviewed   bool           `json:"reviewed"`
	ReviewedAt *time.Time     `json:"reviewed_at,omitempty"`
	Version    int32          `json:"version"`
	Summary    *string        `json:"summary,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	Links      []string       `json:"links,omitempty"`
	ChatSource *ChatSourceDTO `json:"chat_source,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

// EnrichmentCredits is the trusted financial boundary used by note workers.
type EnrichmentCredits interface {
	ReserveAction(context.Context, string, string, string) (credits.Reservation, error)
	Claim(context.Context, string, string) error
	Limits(context.Context, string, string, string) (*enginev1.UsageLimits, error)
	Settle(context.Context, string, string, []*enginev1.Usage) (credits.Reservation, error)
	SettleWith(context.Context, string, string, []*enginev1.Usage, func(pgx.Tx, credits.Reservation) error) (credits.Reservation, error)
}

const maxRequestBodyBytes = 64 * 1024 // 64 KB limit on notes/bookmarks request bodies

// AnalyticsConfig holds configurable analytics metadata defaults.
type AnalyticsConfig struct {
	Platform   string
	AppVersion string
}

// AnalyticsMetadata holds the resolved platform and application version for analytics.
type AnalyticsMetadata struct {
	Platform   string
	AppVersion string
}

type analyticsContextKey struct{}

// WithAnalyticsMetadata injects analytics metadata into the context.
func WithAnalyticsMetadata(ctx context.Context, meta AnalyticsMetadata) context.Context {
	return context.WithValue(ctx, analyticsContextKey{}, meta)
}

// Service manages quick notes persistence and HTTP endpoints.
type Service struct {
	pool         *pgxpool.Pool
	queries      *db.Queries
	events       analytics.Events
	credits      EnrichmentCredits
	engineClient enginev1.EngineServiceClient
	riverClient  *river.Client[pgx.Tx]
	queueMu      sync.Mutex
	analyticsCfg AnalyticsConfig
}

// ConfigureAnalytics configures default analytics properties for the service.
func (s *Service) ConfigureAnalytics(cfg AnalyticsConfig) {
	s.analyticsCfg = cfg
}

func (s *Service) resolvePlatform(r *http.Request) string {
	if r != nil {
		if p := r.Header.Get("X-Platform"); p != "" {
			return p
		}
	}
	if s.analyticsCfg.Platform != "" {
		return s.analyticsCfg.Platform
	}
	if env := os.Getenv("DUOLIFY_PLATFORM"); env != "" {
		return env
	}
	return "web"
}

func (s *Service) resolveAppVersion(r *http.Request) string {
	if r != nil {
		if v := r.Header.Get("X-App-Version"); v != "" {
			return v
		}
	}
	if s.analyticsCfg.AppVersion != "" {
		return s.analyticsCfg.AppVersion
	}
	if env := os.Getenv("DUOLIFY_APP_VERSION"); env != "" {
		return env
	}
	return "0.1.0"
}

// AnalyticsMetadata resolves analytics metadata from context, headers, config or environment.
func (s *Service) AnalyticsMetadata(ctx context.Context) AnalyticsMetadata {
	if ctx != nil {
		if val, ok := ctx.Value(analyticsContextKey{}).(AnalyticsMetadata); ok && val.Platform != "" && val.AppVersion != "" {
			return val
		}
	}
	return AnalyticsMetadata{
		Platform:   s.resolvePlatform(nil),
		AppVersion: s.resolveAppVersion(nil),
	}
}

// NewService constructs a new notes Service instance.
func NewService(pool *pgxpool.Pool, events analytics.Events) *Service {
	return &Service{
		pool:    pool,
		queries: db.New(pool),
		events:  events,
	}
}

// NewServiceWithEnrichment constructs a new notes Service instance with credit and AI enrichment capabilities.
func NewServiceWithEnrichment(pool *pgxpool.Pool, events analytics.Events, creditsSvc EnrichmentCredits, engineClient enginev1.EngineServiceClient) *Service {
	return &Service{
		pool:         pool,
		queries:      db.New(pool),
		events:       events,
		credits:      creditsSvc,
		engineClient: engineClient,
	}
}

// SaveNote saves or idempotently synchronizes a raw quick note.
func (s *Service) SaveNote(ctx context.Context, learnerID string, input SaveNoteInput) (*NoteDTO, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}

	var coursePgUUID pgtype.UUID
	if input.CourseID != nil && strings.TrimSpace(*input.CourseID) != "" {
		parsed, err := parseUUID(*input.CourseID)
		if err == nil {
			coursePgUUID = parsed
		}
	}

	var lessonPgUUID pgtype.UUID
	if input.LessonID != nil && strings.TrimSpace(*input.LessonID) != "" {
		parsed, err := parseUUID(*input.LessonID)
		if err != nil {
			return nil, ErrUnauthorizedSource
		}
		lessonPgUUID = parsed

		// Validate that the lesson exists and is accessible to this learner
		authorized, err := s.queries.CheckLessonAuthorization(ctx, db.CheckLessonAuthorizationParams{
			ID:        lessonPgUUID,
			LearnerID: learnerUUID,
		})
		if err != nil || !authorized {
			return nil, ErrUnauthorizedSource
		}
	}

	coursePgUUID, err = s.authorizedNoteCourse(ctx, learnerUUID, coursePgUUID, lessonPgUUID)
	if err != nil {
		return nil, err
	}

	var slideID pgtype.Int4
	if input.SlideID != nil && *input.SlideID >= 0 && *input.SlideID <= math.MaxInt32 {
		slideID = pgtype.Int4{Int32: int32(*input.SlideID), Valid: true}
	}

	queue, err := s.NotesQueue(slog.Default())
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if input.Chat != nil {
		if err = lockChatSource(ctx, tx, learnerID, input.Chat); err != nil {
			return nil, err
		}
	}
	note, err := s.queries.WithTx(tx).UpsertQuickNote(ctx, db.UpsertQuickNoteParams{
		LearnerID:  learnerUUID,
		ActionKey:  input.ActionKey,
		CourseID:   coursePgUUID,
		LessonID:   lessonPgUUID,
		SlideID:    slideID,
		SlideTitle: input.SlideTitle,
		Source:     input.Source,
		RawText:    input.RawText,
		Status:     "enrichment_pending",
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if err = tx.Rollback(ctx); err != nil {
			return nil, err
		}
		existing, e := s.queries.GetQuickNoteByActionKey(ctx, db.GetQuickNoteByActionKeyParams{LearnerID: learnerUUID, ActionKey: input.ActionKey})
		if e != nil {
			return nil, e
		}
		return s.GetNote(ctx, learnerID, formatUUID(existing))
	}
	if err != nil {
		return nil, err
	}
	if input.Chat != nil {
		if err = recordChatSource(ctx, tx, learnerID, formatUUID(note.ID), input.LessonID, input.Chat); err != nil {
			return nil, err
		}
	}
	if _, err = queue.InsertTx(ctx, tx, NoteEnrichArgs{LearnerID: learnerID, NoteID: formatUUID(note.ID), ExpectedVersion: int(note.RawVersion)}, nil); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	meta := s.AnalyticsMetadata(ctx)
	_ = EmitQuickNoteCreated(s.events, learnerID, input.Source, len(input.RawText), meta.Platform, meta.AppVersion)

	dto := toDTO(note)
	if input.Chat != nil {
		dto.ChatSource = &ChatSourceDTO{State: "available", ThreadID: &input.Chat.ThreadID, MessageID: &input.Chat.MessageID}
	}
	return &dto, nil
}

// authorizedNoteCourse rechecks persisted context as well as new note input.
func (s *Service) authorizedNoteCourse(ctx context.Context, learner, course, lesson pgtype.UUID) (pgtype.UUID, error) {
	if lesson.Valid {
		derived, err := s.queries.GetCourseIDForLesson(ctx, db.GetCourseIDForLessonParams{ID: lesson, LearnerID: learner})
		if errors.Is(err, pgx.ErrNoRows) {
			return pgtype.UUID{}, ErrUnauthorizedSource
		}
		if err != nil {
			return pgtype.UUID{}, err
		}
		if course.Valid && course != derived {
			return pgtype.UUID{}, ErrUnauthorizedSource
		}
		course = derived
	}
	if course.Valid {
		authorized, err := s.queries.CheckNoteCourseAuthorization(ctx, db.CheckNoteCourseAuthorizationParams{ID: course, LearnerID: learner})
		if err != nil {
			return pgtype.UUID{}, err
		}
		if !authorized {
			return pgtype.UUID{}, ErrUnauthorizedSource
		}
	}
	return course, nil
}

// GetNote retrieves a quick note by ID for an authorized learner.
func (s *Service) GetNote(ctx context.Context, learnerID, noteID string) (*NoteDTO, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}
	noteUUID, err := parseUUID(noteID)
	if err != nil {
		return nil, ErrNotFound
	}

	note, err := s.queries.GetQuickNoteWithEnrichmentByID(ctx, db.GetQuickNoteWithEnrichmentByIDParams{
		ID:        noteUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	dto := toDTOFromEnriched(note)
	one := []NoteDTO{dto}
	if err = s.attachChatSources(ctx, learnerID, one); err != nil {
		return nil, err
	}
	return &one[0], nil
}

// ListNotes lists all quick notes for the given learner, newest first.
func (s *Service) ListNotes(ctx context.Context, learnerID string) ([]NoteDTO, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}

	notes, err := s.queries.ListQuickNotesWithEnrichmentByLearner(ctx, learnerUUID)
	if err != nil {
		return nil, err
	}

	dtos := make([]NoteDTO, len(notes))
	for i, n := range notes {
		dtos[i] = toDTOFromListEnriched(n)
	}
	if err = s.attachChatSources(ctx, learnerID, dtos); err != nil {
		return nil, err
	}
	return dtos, nil
}

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	learnerID := accounts.LearnerID(r.Context())
	if learnerID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "sign in required"})
		return
	}

	meta := AnalyticsMetadata{
		Platform:   s.resolvePlatform(r),
		AppVersion: s.resolveAppVersion(r),
	}
	r = r.WithContext(WithAnalyticsMetadata(r.Context(), meta))

	path := strings.TrimPrefix(r.URL.Path, "/v1/notes")
	path = strings.TrimPrefix(path, "/")

	switch r.Method {
	case http.MethodPost:
		if strings.HasSuffix(path, "/enrich") {
			noteID := strings.TrimSuffix(path, "/enrich")
			if err := s.ProcessEnrichment(r.Context(), learnerID, noteID); err != nil {
				if errors.Is(err, ErrNotFound) {
					respondError(w, http.StatusNotFound, "note not found")
				} else {
					respondError(w, http.StatusInternalServerError, "failed to enrich note")
				}
				return
			}
			refreshed, err := s.GetNote(r.Context(), learnerID, noteID)
			if err != nil {
				respondError(w, http.StatusNotFound, "note not found")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(refreshed)
			return
		}
		if strings.HasSuffix(path, "/review") {
			noteID := strings.TrimSuffix(path, "/review")
			reviewed, err := s.MarkReviewed(r.Context(), learnerID, noteID)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					respondError(w, http.StatusNotFound, "note not found")
					return
				}
				respondError(w, http.StatusInternalServerError, "failed to mark note reviewed")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(reviewed)
			return
		}
		if path != "" && path != "sync" {
			http.NotFound(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		var in SaveNoteInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		note, err := s.SaveNote(r.Context(), learnerID, in)
		if err != nil {
			switch {
			case errors.Is(err, ErrMissingActionKey),
				errors.Is(err, ErrInvalidText),
				errors.Is(err, ErrTextTooLong),
				errors.Is(err, ErrInvalidSource),
				errors.Is(err, ErrInvalidSlide):
				respondError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, ErrUnauthorizedSource):
				respondError(w, http.StatusForbidden, err.Error())
			default:
				respondError(w, http.StatusInternalServerError, "failed to save note")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(note)

	case http.MethodPatch, http.MethodPut:
		if path == "" {
			respondError(w, http.StatusNotFound, "note id required")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		var in UpdateNoteInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		note, err := s.UpdateNote(r.Context(), learnerID, path, in)
		if err != nil {
			switch {
			case errors.Is(err, ErrConflict):
				respondError(w, http.StatusConflict, err.Error())
			case errors.Is(err, ErrNotFound):
				respondError(w, http.StatusNotFound, "note not found")
			case errors.Is(err, ErrInvalidText),
				errors.Is(err, ErrTextTooLong),
				errors.Is(err, ErrInvalidVersion):
				respondError(w, http.StatusBadRequest, err.Error())
			default:
				respondError(w, http.StatusInternalServerError, "failed to update note")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(note)

	case http.MethodDelete:
		if path == "" {
			http.NotFound(w, r)
			return
		}
		err := s.DeleteNote(r.Context(), learnerID, path)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "note not found")
				return
			}
			respondError(w, http.StatusInternalServerError, "failed to delete note")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})

	case http.MethodGet:
		if path == "" {
			q := r.URL.Query()
			var filter FilterNotesInput
			filter.Query = q.Get("q")
			filter.CourseID = q.Get("course_id")
			filter.LessonID = q.Get("lesson_id")
			filter.Tag = q.Get("tag")
			filter.Status = q.Get("status")
			if q.Has("reviewed") {
				val := q.Get("reviewed")
				if val != "true" && val != "false" && val != "1" && val != "0" {
					respondError(w, http.StatusBadRequest, "invalid reviewed filter")
					return
				}
				b := val == "true" || val == "1"
				filter.Reviewed = &b
			}

			notesList, err := s.FilterNotes(r.Context(), learnerID, filter)
			if err != nil {
				if errors.Is(err, ErrInvalidFilter) {
					respondError(w, http.StatusBadRequest, err.Error())
					return
				}
				respondError(w, http.StatusInternalServerError, "failed to list notes")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(notesList)
			return
		}

		if strings.HasSuffix(path, "/export") {
			noteID := strings.TrimSuffix(path, "/export")
			note, err := s.GetNote(r.Context(), learnerID, noteID)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					respondError(w, http.StatusNotFound, "note not found")
					return
				}
				respondError(w, http.StatusInternalServerError, "failed to get note")
				return
			}
			md := FormatNoteMarkdown(*note)
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("Content-Disposition", `attachment; filename="note.md"`)
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(md))
			return
		}

		note, err := s.GetNote(r.Context(), learnerID, path)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				respondError(w, http.StatusNotFound, "note not found")
				return
			}
			respondError(w, http.StatusInternalServerError, "failed to get note")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(note)

	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func respondError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func toDTO(qn db.UpsertQuickNoteRow) NoteDTO {
	dto := NoteDTO{
		ID:         formatUUID(qn.ID),
		LearnerID:  formatUUID(qn.LearnerID),
		ActionKey:  qn.ActionKey,
		SlideTitle: qn.SlideTitle,
		Source:     qn.Source,
		RawText:    qn.RawText,
		RawVersion: int(qn.RawVersion),
		Status:     qn.Status,
		Reviewed:   qn.Reviewed,
		Version:    qn.Version,
		CreatedAt:  qn.CreatedAt.Time,
		UpdatedAt:  qn.UpdatedAt.Time,
	}
	if qn.ReviewedAt.Valid {
		dto.ReviewedAt = &qn.ReviewedAt.Time
	}
	if qn.CourseID.Valid {
		cID := formatUUID(qn.CourseID)
		dto.CourseID = &cID
	}
	if qn.LessonID.Valid {
		lID := formatUUID(qn.LessonID)
		dto.LessonID = &lID
	}
	if qn.SlideID.Valid {
		sID := int(qn.SlideID.Int32)
		dto.SlideID = &sID
	}
	return dto
}

func toDTOFromEnriched(r db.GetQuickNoteWithEnrichmentByIDRow) NoteDTO {
	dto := NoteDTO{
		ID:         formatUUID(r.ID),
		LearnerID:  formatUUID(r.LearnerID),
		ActionKey:  r.ActionKey,
		SlideTitle: r.SlideTitle,
		Source:     r.Source,
		RawText:    r.RawText,
		RawVersion: int(r.RawVersion),
		Status:     r.Status,
		Reviewed:   r.Reviewed,
		Version:    r.Version,
		CreatedAt:  r.CreatedAt.Time,
		UpdatedAt:  r.UpdatedAt.Time,
	}
	if r.ReviewedAt.Valid {
		dto.ReviewedAt = &r.ReviewedAt.Time
	}
	if r.CourseID.Valid {
		cID := formatUUID(r.CourseID)
		dto.CourseID = &cID
	}
	if r.LessonID.Valid {
		lID := formatUUID(r.LessonID)
		dto.LessonID = &lID
	}
	if r.SlideID.Valid {
		sID := int(r.SlideID.Int32)
		dto.SlideID = &sID
	}
	if r.Summary.Valid {
		dto.Summary = &r.Summary.String
	}
	if len(r.Tags) > 0 {
		dto.Tags = r.Tags
	}
	if len(r.Links) > 0 {
		dto.Links = r.Links
	}
	return dto
}

func toDTOFromListEnriched(r db.ListQuickNotesWithEnrichmentByLearnerRow) NoteDTO {
	dto := NoteDTO{
		ID:         formatUUID(r.ID),
		LearnerID:  formatUUID(r.LearnerID),
		ActionKey:  r.ActionKey,
		SlideTitle: r.SlideTitle,
		Source:     r.Source,
		RawText:    r.RawText,
		RawVersion: int(r.RawVersion),
		Status:     r.Status,
		Reviewed:   r.Reviewed,
		Version:    r.Version,
		CreatedAt:  r.CreatedAt.Time,
		UpdatedAt:  r.UpdatedAt.Time,
	}
	if r.ReviewedAt.Valid {
		dto.ReviewedAt = &r.ReviewedAt.Time
	}
	if r.CourseID.Valid {
		cID := formatUUID(r.CourseID)
		dto.CourseID = &cID
	}
	if r.LessonID.Valid {
		lID := formatUUID(r.LessonID)
		dto.LessonID = &lID
	}
	if r.SlideID.Valid {
		sID := int(r.SlideID.Int32)
		dto.SlideID = &sID
	}
	if r.Summary.Valid {
		dto.Summary = &r.Summary.String
	}
	if len(r.Tags) > 0 {
		dto.Tags = r.Tags
	}
	if len(r.Links) > 0 {
		dto.Links = r.Links
	}
	return dto
}

func parseUUID(s string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, err
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

func formatUUID(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	id, err := uuid.FromBytes(u.Bytes[:])
	if err != nil {
		return ""
	}
	return id.String()
}

// EmitQuickNoteCreated captures the quick_note_created analytics event with privacy protection.
func EmitQuickNoteCreated(events analytics.Events, learnerID, source string, length int, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	if platform == "" {
		platform = "web"
	}
	if appVersion == "" {
		appVersion = "0.1.0"
	}
	return events.Capture(learnerID, "quick_note_created", map[string]any{
		"source":      source,
		"length":      length,
		"platform":    platform,
		"app_version": appVersion,
	})
}
