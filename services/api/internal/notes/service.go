// Package notes implements persistence and retrieval of raw quick notes.
package notes

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
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
	ID         string    `json:"id"`
	LearnerID  string    `json:"learner_id"`
	ActionKey  string    `json:"action_key"`
	CourseID   *string   `json:"course_id,omitempty"`
	LessonID   *string   `json:"lesson_id,omitempty"`
	SlideID    *int      `json:"slide_id,omitempty"`
	SlideTitle string    `json:"slide_title"`
	Source     string    `json:"source"`
	RawText    string    `json:"raw_text"`
	Status     string    `json:"status"`
	Reviewed   bool      `json:"reviewed"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Service manages quick notes persistence and HTTP endpoints.
type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
	events  analytics.Events
}

// NewService constructs a new notes Service instance.
func NewService(pool *pgxpool.Pool, events analytics.Events) *Service {
	return &Service{
		pool:    pool,
		queries: db.New(pool),
		events:  events,
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
	if input.LessonID != nil && strings.TrimSpace(*input.LessonID) != "" && *input.LessonID != "sky" {
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

	var slideID pgtype.Int4
	if input.SlideID != nil && *input.SlideID >= 0 && *input.SlideID <= math.MaxInt32 {
		slideID = pgtype.Int4{Int32: int32(*input.SlideID), Valid: true}
	}

	note, err := s.queries.UpsertQuickNote(ctx, db.UpsertQuickNoteParams{
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
	if err != nil {
		return nil, err
	}

	_ = EmitQuickNoteCreated(s.events, learnerID, input.Source, len(input.RawText), "web", "0.1.0")

	dto := toDTO(note)
	return &dto, nil
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

	note, err := s.queries.GetQuickNoteByID(ctx, db.GetQuickNoteByIDParams{
		ID:        noteUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	dto := toDTO(note)
	return &dto, nil
}

// ListNotes lists all quick notes for the given learner, newest first.
func (s *Service) ListNotes(ctx context.Context, learnerID string) ([]NoteDTO, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}

	notes, err := s.queries.ListQuickNotesByLearner(ctx, learnerUUID)
	if err != nil {
		return nil, err
	}

	dtos := make([]NoteDTO, len(notes))
	for i, n := range notes {
		dtos[i] = toDTO(n)
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

	path := strings.TrimPrefix(r.URL.Path, "/v1/notes")
	path = strings.TrimPrefix(path, "/")

	switch r.Method {
	case http.MethodPost:
		if path != "" && path != "sync" {
			http.NotFound(w, r)
			return
		}
		var in SaveNoteInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
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

	case http.MethodGet:
		if path == "" {
			notesList, err := s.ListNotes(r.Context(), learnerID)
			if err != nil {
				respondError(w, http.StatusInternalServerError, "failed to list notes")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(notesList)
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

func toDTO(qn db.QuickNote) NoteDTO {
	dto := NoteDTO{
		ID:         formatUUID(qn.ID),
		LearnerID:  formatUUID(qn.LearnerID),
		ActionKey:  qn.ActionKey,
		SlideTitle: qn.SlideTitle,
		Source:     qn.Source,
		RawText:    qn.RawText,
		Status:     qn.Status,
		Reviewed:   qn.Reviewed,
		CreatedAt:  qn.CreatedAt.Time,
		UpdatedAt:  qn.UpdatedAt.Time,
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
