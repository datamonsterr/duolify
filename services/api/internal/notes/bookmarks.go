package notes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/accounts"
	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// ErrMissingLesson indicates that the lesson ID was missing or empty.
	ErrMissingLesson = errors.New("missing lesson id")
	// ErrInvalidTargetType indicates that the target type is not 'slide' or 'lesson'.
	ErrInvalidTargetType = errors.New("invalid target type")
	// ErrBookmarkNotFound indicates that the requested bookmark was not found.
	ErrBookmarkNotFound = errors.New("bookmark not found")
	// ErrInvalidActionKey indicates that the action key is invalid or too long.
	ErrInvalidActionKey = errors.New("invalid action key")
	// ErrTitleTooLong indicates that a title exceeds the maximum length.
	ErrTitleTooLong = errors.New("title exceeds maximum length")
	// ErrActionKeyConflict indicates an action_key replay with a different target.
	ErrActionKeyConflict = errors.New("action key conflict")
)

// SaveBookmarkInput contains parameters for creating or synchronizing a bookmark.
type SaveBookmarkInput struct {
	ActionKey   string `json:"action_key"`
	CourseID    string `json:"course_id,omitempty"`
	LessonID    string `json:"lesson_id"`
	SlideID     *int   `json:"slide_id,omitempty"`
	TargetType  string `json:"target_type"`
	CourseTitle string `json:"course_title,omitempty"`
	LessonTitle string `json:"lesson_title,omitempty"`
	SlideTitle  string `json:"slide_title,omitempty"`
}

// Validate verifies constraints for bookmark input.
func (in SaveBookmarkInput) Validate() error {
	if strings.TrimSpace(in.ActionKey) == "" {
		return ErrMissingActionKey
	}
	if len(in.ActionKey) > 128 {
		return ErrInvalidActionKey
	}
	if strings.TrimSpace(in.LessonID) == "" {
		return ErrMissingLesson
	}
	if len(in.CourseTitle) > 500 || len(in.LessonTitle) > 500 || len(in.SlideTitle) > 500 {
		return ErrTitleTooLong
	}
	switch in.TargetType {
	case "slide":
		if in.SlideID == nil || *in.SlideID < 0 || *in.SlideID > 10000 {
			return ErrInvalidSlide
		}
	case "lesson":
		if in.SlideID != nil {
			return ErrInvalidSlide
		}
	default:
		return ErrInvalidTargetType
	}
	return nil
}

// BookmarkDTO represents a persisted bookmark returned over the API.
type BookmarkDTO struct {
	ID          string    `json:"id"`
	LearnerID   string    `json:"learner_id"`
	ActionKey   string    `json:"action_key"`
	CourseID    *string   `json:"course_id,omitempty"`
	CourseTitle string    `json:"course_title"`
	LessonID    *string   `json:"lesson_id,omitempty"`
	LessonTitle string    `json:"lesson_title"`
	SlideID     *int      `json:"slide_id,omitempty"`
	SlideTitle  string    `json:"slide_title"`
	TargetType  string    `json:"target_type"`
	Available   bool      `json:"available"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// SaveBookmark persists or idempotently returns an existing bookmark.
func (s *Service) SaveBookmark(ctx context.Context, learnerID string, input SaveBookmarkInput) (*BookmarkDTO, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}

	parsedLesson, err := parseUUID(input.LessonID)
	if err != nil {
		return nil, ErrUnauthorizedSource
	}
	lessonPgUUID := parsedLesson

	// Verify learner authorization for this lesson
	authorized, err := s.queries.CheckLessonAuthorization(ctx, db.CheckLessonAuthorizationParams{
		ID:        lessonPgUUID,
		LearnerID: learnerUUID,
	})
	if err != nil || !authorized {
		return nil, ErrUnauthorizedSource
	}

	// Authoritatively derive course and titles on the server (do not trust client fields)
	info, err := s.queries.GetLessonContext(ctx, lessonPgUUID)
	if err != nil {
		return nil, ErrUnauthorizedSource
	}
	coursePgUUID := info.CourseID
	courseTitle := info.CourseTitle
	lessonTitle := info.LessonTitle
	if len(courseTitle) > 500 {
		courseTitle = courseTitle[:500]
	}
	if len(lessonTitle) > 500 {
		lessonTitle = lessonTitle[:500]
	}

	var slideTitle string
	var slideID pgtype.Int4
	if input.TargetType == "slide" && input.SlideID != nil && *input.SlideID >= 0 && *input.SlideID <= 10000 {
		slideID = pgtype.Int4{Int32: int32(*input.SlideID), Valid: true}
		slideTitle = fmt.Sprintf("Slide %d", *input.SlideID+1)
	}

	// 1. Check if bookmark with action_key already exists
	existingByActionKey, err := s.queries.GetBookmarkByActionKey(ctx, db.GetBookmarkByActionKeyParams{
		LearnerID: learnerUUID,
		ActionKey: input.ActionKey,
	})
	if err == nil {
		// An action_key replay with a different target must not return the old row silently (return a conflict)
		sameLesson := existingByActionKey.LessonID == lessonPgUUID
		sameTargetType := existingByActionKey.TargetType == input.TargetType
		sameSlide := (!existingByActionKey.SlideID.Valid && !slideID.Valid) ||
			(existingByActionKey.SlideID.Valid && slideID.Valid && existingByActionKey.SlideID.Int32 == slideID.Int32)
		if !sameLesson || !sameTargetType || !sameSlide {
			return nil, ErrActionKeyConflict
		}
		dto := toBookmarkDTO(existingByActionKey, true)
		return &dto, nil
	}

	// 2. Check if bookmark with the same target already exists (duplicate bookmark)
	existingByTarget, err := s.queries.GetBookmarkByTarget(ctx, db.GetBookmarkByTargetParams{
		LearnerID: learnerUUID,
		LessonID:  lessonPgUUID,
		SlideID:   slideID,
	})
	if err == nil {
		dto := toBookmarkDTO(existingByTarget, true)
		return &dto, nil
	}

	// 3. Upsert bookmark
	created, err := s.queries.UpsertBookmark(ctx, db.UpsertBookmarkParams{
		LearnerID:   learnerUUID,
		ActionKey:   input.ActionKey,
		CourseID:    coursePgUUID,
		LessonID:    lessonPgUUID,
		SlideID:     slideID,
		TargetType:  input.TargetType,
		CourseTitle: courseTitle,
		LessonTitle: lessonTitle,
		SlideTitle:  slideTitle,
	})
	if err != nil {
		// Handle unique violation race (Postgres code 23505) -> returns existing/409, not 500
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if existing, getErr := s.queries.GetBookmarkByActionKey(ctx, db.GetBookmarkByActionKeyParams{
				LearnerID: learnerUUID,
				ActionKey: input.ActionKey,
			}); getErr == nil {
				sameLesson := existing.LessonID == lessonPgUUID
				sameTargetType := existing.TargetType == input.TargetType
				sameSlide := (!existing.SlideID.Valid && !slideID.Valid) ||
					(existing.SlideID.Valid && slideID.Valid && existing.SlideID.Int32 == slideID.Int32)
				if !sameLesson || !sameTargetType || !sameSlide {
					return nil, ErrActionKeyConflict
				}
				dto := toBookmarkDTO(existing, true)
				return &dto, nil
			}
			if existing, getErr := s.queries.GetBookmarkByTarget(ctx, db.GetBookmarkByTargetParams{
				LearnerID: learnerUUID,
				LessonID:  lessonPgUUID,
				SlideID:   slideID,
			}); getErr == nil {
				dto := toBookmarkDTO(existing, true)
				return &dto, nil
			}
			return nil, ErrConflict
		}
		return nil, err
	}

	meta := s.AnalyticsMetadata(ctx)
	_ = EmitBookmarkAdded(s.events, learnerID, input.TargetType, formatUUID(coursePgUUID), input.LessonID, input.SlideID, meta.Platform, meta.AppVersion)

	dto := toBookmarkDTO(created, true)
	return &dto, nil
}

// DeleteBookmark removes a bookmark by ID.
func (s *Service) DeleteBookmark(ctx context.Context, learnerID, bookmarkID string) error {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return errors.New("invalid learner id")
	}
	bmUUID, err := parseUUID(bookmarkID)
	if err != nil {
		return ErrBookmarkNotFound
	}

	deleted, err := s.queries.DeleteBookmarkByID(ctx, db.DeleteBookmarkByIDParams{
		ID:        bmUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // idempotent deletion
		}
		return err
	}

	var slideID *int
	if deleted.SlideID.Valid {
		val := int(deleted.SlideID.Int32)
		slideID = &val
	}
	meta := s.AnalyticsMetadata(ctx)
	_ = EmitBookmarkRemoved(s.events, learnerID, deleted.TargetType, formatUUID(deleted.CourseID), formatUUID(deleted.LessonID), slideID, meta.Platform, meta.AppVersion)
	return nil
}

// DeleteBookmarkByTarget removes a bookmark by target pointer.
func (s *Service) DeleteBookmarkByTarget(ctx context.Context, learnerID, lessonID string, slideID *int) error {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return errors.New("invalid learner id")
	}

	var lessonPgUUID pgtype.UUID
	if lessonID != "" {
		parsed, err := parseUUID(lessonID)
		if err == nil {
			lessonPgUUID = parsed
		}
	}

	var slidePgInt4 pgtype.Int4
	if slideID != nil && *slideID >= 0 && *slideID <= math.MaxInt32 {
		slidePgInt4 = pgtype.Int4{Int32: int32(*slideID), Valid: true}
	}

	deleted, err := s.queries.DeleteBookmarkByTarget(ctx, db.DeleteBookmarkByTargetParams{
		LearnerID: learnerUUID,
		LessonID:  lessonPgUUID,
		SlideID:   slidePgInt4,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	meta := s.AnalyticsMetadata(ctx)
	_ = EmitBookmarkRemoved(s.events, learnerID, deleted.TargetType, formatUUID(deleted.CourseID), formatUUID(deleted.LessonID), slideID, meta.Platform, meta.AppVersion)
	return nil
}

// GetBookmark retrieves a bookmark by ID.
func (s *Service) GetBookmark(ctx context.Context, learnerID, bookmarkID string) (*BookmarkDTO, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}
	bmUUID, err := parseUUID(bookmarkID)
	if err != nil {
		return nil, ErrBookmarkNotFound
	}

	row, err := s.queries.GetBookmarkByID(ctx, db.GetBookmarkByIDParams{
		ID:        bmUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrBookmarkNotFound
		}
		return nil, err
	}

	dto := BookmarkDTO{
		ID:          formatUUID(row.ID),
		LearnerID:   formatUUID(row.LearnerID),
		ActionKey:   row.ActionKey,
		CourseTitle: row.CourseTitle,
		LessonTitle: row.LessonTitle,
		SlideTitle:  row.SlideTitle,
		TargetType:  row.TargetType,
		Available:   row.Available.Valid && row.Available.Bool,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}
	if row.CourseID.Valid {
		cID := formatUUID(row.CourseID)
		dto.CourseID = &cID
	}
	if row.LessonID.Valid {
		lID := formatUUID(row.LessonID)
		dto.LessonID = &lID
	}
	if row.SlideID.Valid {
		sID := int(row.SlideID.Int32)
		dto.SlideID = &sID
	}
	return &dto, nil
}

// ListBookmarks lists bookmarks for the learner, optionally filtered by course.
func (s *Service) ListBookmarks(ctx context.Context, learnerID string, courseID *string) ([]BookmarkDTO, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}

	if courseID != nil && strings.TrimSpace(*courseID) != "" {
		coursePgUUID, err := parseUUID(*courseID)
		if err != nil {
			return nil, ErrInvalidFilter
		}
		rows, err := s.queries.ListBookmarksByLearnerAndCourse(ctx, db.ListBookmarksByLearnerAndCourseParams{
			LearnerID: learnerUUID,
			CourseID:  coursePgUUID,
		})
		if err != nil {
			return nil, err
		}
		dtos := make([]BookmarkDTO, len(rows))
		for i, r := range rows {
			dtos[i] = BookmarkDTO{
				ID:          formatUUID(r.ID),
				LearnerID:   formatUUID(r.LearnerID),
				ActionKey:   r.ActionKey,
				CourseTitle: r.CourseTitle,
				LessonTitle: r.LessonTitle,
				SlideTitle:  r.SlideTitle,
				TargetType:  r.TargetType,
				Available:   r.Available.Valid && r.Available.Bool,
				CreatedAt:   r.CreatedAt.Time,
				UpdatedAt:   r.UpdatedAt.Time,
			}
			if r.CourseID.Valid {
				cID := formatUUID(r.CourseID)
				dtos[i].CourseID = &cID
			}
			if r.LessonID.Valid {
				lID := formatUUID(r.LessonID)
				dtos[i].LessonID = &lID
			}
			if r.SlideID.Valid {
				sID := int(r.SlideID.Int32)
				dtos[i].SlideID = &sID
			}
		}
		return dtos, nil
	}

	rows, err := s.queries.ListBookmarksByLearner(ctx, learnerUUID)
	if err != nil {
		return nil, err
	}
	dtos := make([]BookmarkDTO, len(rows))
	for i, r := range rows {
		dtos[i] = BookmarkDTO{
			ID:          formatUUID(r.ID),
			LearnerID:   formatUUID(r.LearnerID),
			ActionKey:   r.ActionKey,
			CourseTitle: r.CourseTitle,
			LessonTitle: r.LessonTitle,
			SlideTitle:  r.SlideTitle,
			TargetType:  r.TargetType,
			Available:   r.Available.Valid && r.Available.Bool,
			CreatedAt:   r.CreatedAt.Time,
			UpdatedAt:   r.UpdatedAt.Time,
		}
		if r.CourseID.Valid {
			cID := formatUUID(r.CourseID)
			dtos[i].CourseID = &cID
		}
		if r.LessonID.Valid {
			lID := formatUUID(r.LessonID)
			dtos[i].LessonID = &lID
		}
		if r.SlideID.Valid {
			sID := int(r.SlideID.Int32)
			dtos[i].SlideID = &sID
		}
	}
	return dtos, nil
}

// ServeBookmarksHTTP handles /v1/bookmarks HTTP requests.
func (s *Service) ServeBookmarksHTTP(w http.ResponseWriter, r *http.Request) {
	learnerID := accounts.LearnerID(r.Context())
	if learnerID == "" {
		respondError(w, http.StatusUnauthorized, "sign in required")
		return
	}

	meta := AnalyticsMetadata{
		Platform:   s.resolvePlatform(r),
		AppVersion: s.resolveAppVersion(r),
	}
	r = r.WithContext(WithAnalyticsMetadata(r.Context(), meta))

	path := strings.TrimPrefix(r.URL.Path, "/v1/bookmarks")
	path = strings.TrimPrefix(path, "/")

	switch r.Method {
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		if path == "remove" {
			var in struct {
				ID       string `json:"id,omitempty"`
				LessonID string `json:"lesson_id,omitempty"`
				SlideID  *int   `json:"slide_id,omitempty"`
			}
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
					return
				}
				respondError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			if in.SlideID != nil && (*in.SlideID < 0 || *in.SlideID > 10000) {
				respondError(w, http.StatusBadRequest, "invalid slide id")
				return
			}
			if in.ID == "" && in.LessonID == "" {
				respondError(w, http.StatusBadRequest, "missing id or lesson_id")
				return
			}
			if in.ID != "" {
				if err := s.DeleteBookmark(r.Context(), learnerID, in.ID); err != nil {
					if errors.Is(err, ErrBookmarkNotFound) {
						respondError(w, http.StatusNotFound, "bookmark not found")
						return
					}
					respondError(w, http.StatusInternalServerError, "failed to delete bookmark")
					return
				}
			} else if in.LessonID != "" {
				if err := s.DeleteBookmarkByTarget(r.Context(), learnerID, in.LessonID, in.SlideID); err != nil {
					respondError(w, http.StatusInternalServerError, "failed to delete bookmark")
					return
				}
			}
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"removed": true})
			return
		}

		if path != "" && path != "sync" {
			http.NotFound(w, r)
			return
		}
		var in SaveBookmarkInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				respondError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		bm, err := s.SaveBookmark(r.Context(), learnerID, in)
		if err != nil {
			switch {
			case errors.Is(err, ErrMissingActionKey),
				errors.Is(err, ErrMissingLesson),
				errors.Is(err, ErrInvalidTargetType),
				errors.Is(err, ErrInvalidSlide),
				errors.Is(err, ErrInvalidActionKey),
				errors.Is(err, ErrTitleTooLong):
				respondError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, ErrUnauthorizedSource):
				respondError(w, http.StatusForbidden, err.Error())
			case errors.Is(err, ErrActionKeyConflict),
				errors.Is(err, ErrConflict):
				respondError(w, http.StatusConflict, err.Error())
			default:
				respondError(w, http.StatusInternalServerError, "failed to save bookmark")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bm)

	case http.MethodGet:
		if path == "" {
			courseIDParam := r.URL.Query().Get("course_id")
			var courseID *string
			if courseIDParam != "" {
				courseID = &courseIDParam
			}
			list, err := s.ListBookmarks(r.Context(), learnerID, courseID)
			if err != nil {
				if errors.Is(err, ErrInvalidFilter) {
					respondError(w, http.StatusBadRequest, err.Error())
					return
				}
				respondError(w, http.StatusInternalServerError, "failed to list bookmarks")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(list)
			return
		}

		bm, err := s.GetBookmark(r.Context(), learnerID, path)
		if err != nil {
			if errors.Is(err, ErrBookmarkNotFound) {
				respondError(w, http.StatusNotFound, "bookmark not found")
				return
			}
			respondError(w, http.StatusInternalServerError, "failed to get bookmark")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(bm)

	case http.MethodDelete:
		if path == "" {
			respondError(w, http.StatusBadRequest, "missing bookmark id")
			return
		}
		err := s.DeleteBookmark(r.Context(), learnerID, path)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "failed to delete bookmark")
			return
		}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"removed": true})

	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func toBookmarkDTO(r db.Bookmark, available bool) BookmarkDTO {
	dto := BookmarkDTO{
		ID:          formatUUID(r.ID),
		LearnerID:   formatUUID(r.LearnerID),
		ActionKey:   r.ActionKey,
		CourseTitle: r.CourseTitle,
		LessonTitle: r.LessonTitle,
		SlideTitle:  r.SlideTitle,
		TargetType:  r.TargetType,
		Available:   available,
		CreatedAt:   r.CreatedAt.Time,
		UpdatedAt:   r.UpdatedAt.Time,
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
	return dto
}

// EmitBookmarkAdded emits the bookmark_added analytics event.
func EmitBookmarkAdded(events analytics.Events, learnerID, targetType, courseID, lessonID string, slideID *int, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	props := map[string]any{
		"target_type": targetType,
		"source":      "api",
		"platform":    platform,
		"app_version": appVersion,
	}
	if courseID != "" {
		props["course_id"] = courseID
	}
	if lessonID != "" {
		props["lesson_id"] = lessonID
	}
	if slideID != nil {
		props["slide_id"] = *slideID
	}
	return events.Capture(learnerID, "bookmark_added", props)
}

// EmitBookmarkRemoved emits the bookmark_removed analytics event.
func EmitBookmarkRemoved(events analytics.Events, learnerID, targetType, courseID, lessonID string, slideID *int, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	props := map[string]any{
		"target_type": targetType,
		"source":      "api",
		"platform":    platform,
		"app_version": appVersion,
	}
	if courseID != "" {
		props["course_id"] = courseID
	}
	if lessonID != "" {
		props["lesson_id"] = lessonID
	}
	if slideID != nil {
		props["slide_id"] = *slideID
	}
	return events.Capture(learnerID, "bookmark_removed", props)
}

// EmitBookmarkOpened emits the bookmark_opened analytics event.
func EmitBookmarkOpened(events analytics.Events, learnerID, targetType, courseID, lessonID string, slideID *int, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	props := map[string]any{
		"target_type": targetType,
		"source":      "api",
		"platform":    platform,
		"app_version": appVersion,
	}
	if courseID != "" {
		props["course_id"] = courseID
	}
	if lessonID != "" {
		props["lesson_id"] = lessonID
	}
	if slideID != nil {
		props["slide_id"] = *slideID
	}
	return events.Capture(learnerID, "bookmark_opened", props)
}
