package notes

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/datamonsterr/duolify/services/api/internal/analytics"
	"github.com/datamonsterr/duolify/services/api/internal/store/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	// ErrInvalidVersion indicates an invalid expected revision version.
	ErrInvalidVersion = errors.New("invalid expected version")
	// ErrConflict indicates the note was modified on another device or concurrent session.
	ErrConflict = errors.New("note conflict: modified on another device")
	// ErrInvalidFilter indicates an invalid filter was supplied.
	ErrInvalidFilter = errors.New("invalid filter")
)

// UpdateNoteInput contains the parameters to edit a note's raw text.
type UpdateNoteInput struct {
	RawText         string `json:"raw_text"`
	ExpectedVersion int32  `json:"expected_version"`
}

// Validate verifies that the raw text is non-empty, within size bounds, and version is positive.
func (in UpdateNoteInput) Validate() error {
	trimmed := strings.TrimSpace(in.RawText)
	if trimmed == "" {
		return ErrInvalidText
	}
	if len(in.RawText) > 2000 {
		return ErrTextTooLong
	}
	if in.ExpectedVersion < 1 {
		return ErrInvalidVersion
	}
	return nil
}

// FilterNotesInput contains query parameters for searching and filtering quick notes.
type FilterNotesInput struct {
	Query    string
	CourseID string
	LessonID string
	Tag      string
	Status   string
	Reviewed *bool
}

// UpdateNote updates raw note text with optimistic concurrency version checking.
func (s *Service) UpdateNote(ctx context.Context, learnerID, noteID string, input UpdateNoteInput) (*NoteDTO, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}
	noteUUID, err := parseUUID(noteID)
	if err != nil {
		return nil, ErrNotFound
	}

	current, err := s.queries.GetQuickNoteWithEnrichmentByID(ctx, db.GetQuickNoteWithEnrichmentByIDParams{
		ID:        noteUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if current.Version != input.ExpectedVersion {
		return nil, ErrConflict
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
	updated, err := s.queries.WithTx(tx).UpdateQuickNoteRawText(ctx, db.UpdateQuickNoteRawTextParams{
		ID:        noteUUID,
		LearnerID: learnerUUID,
		RawText:   input.RawText,
		Version:   input.ExpectedVersion,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConflict
		}
		return nil, err
	}

	if _, err = queue.InsertTx(ctx, tx, NoteEnrichArgs{NoteID: noteID, LearnerID: learnerID, ExpectedVersion: int(updated.RawVersion)}, nil); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetNote(ctx, learnerID, noteID)
}

// MarkReviewed marks a quick note as reviewed and records the review timestamp.
func (s *Service) MarkReviewed(ctx context.Context, learnerID, noteID string) (*NoteDTO, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}
	noteUUID, err := parseUUID(noteID)
	if err != nil {
		return nil, ErrNotFound
	}

	_, err = s.queries.MarkQuickNoteReviewed(ctx, db.MarkQuickNoteReviewedParams{
		ID:        noteUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	_ = EmitQuickNoteReviewed(s.events, learnerID, noteID, "web", "0.1.0")

	return s.GetNote(ctx, learnerID, noteID)
}

// DeleteNote removes a quick note owned by the learner.
func (s *Service) DeleteNote(ctx context.Context, learnerID, noteID string) error {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return errors.New("invalid learner id")
	}
	noteUUID, err := parseUUID(noteID)
	if err != nil {
		return ErrNotFound
	}

	_, err = s.queries.GetQuickNoteByID(ctx, db.GetQuickNoteByIDParams{
		ID:        noteUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}

	err = s.queries.DeleteQuickNote(ctx, db.DeleteQuickNoteParams{
		ID:        noteUUID,
		LearnerID: learnerUUID,
	})
	if err != nil {
		return err
	}

	_ = EmitQuickNoteDeleted(s.events, learnerID, noteID, "web", "0.1.0")
	return nil
}

// FilterNotes queries notes for the learner matching multi-facet filters and search query.
func (s *Service) FilterNotes(ctx context.Context, learnerID string, filter FilterNotesInput) ([]NoteDTO, error) {
	learnerUUID, err := parseUUID(learnerID)
	if err != nil {
		return nil, errors.New("invalid learner id")
	}

	params := db.FilterQuickNotesParams{
		LearnerID: learnerUUID,
	}

	if strings.TrimSpace(filter.CourseID) != "" {
		cUUID, err := parseUUID(filter.CourseID)
		if err != nil {
			return nil, ErrInvalidFilter
		}
		params.CourseID = cUUID
	}

	if strings.TrimSpace(filter.LessonID) != "" {
		lUUID, err := parseUUID(filter.LessonID)
		if err != nil {
			return nil, ErrInvalidFilter
		}
		params.LessonID = lUUID
	}

	if filter.Reviewed != nil {
		params.Reviewed = pgtype.Bool{Bool: *filter.Reviewed, Valid: true}
	}

	if strings.TrimSpace(filter.Status) != "" {
		params.Status = pgtype.Text{String: filter.Status, Valid: true}
	}

	if strings.TrimSpace(filter.Tag) != "" {
		params.Tag = pgtype.Text{String: strings.TrimPrefix(filter.Tag, "#"), Valid: true}
	}

	if strings.TrimSpace(filter.Query) != "" {
		params.SearchQuery = pgtype.Text{String: filter.Query, Valid: true}
	}

	rows, err := s.queries.FilterQuickNotes(ctx, params)
	if err != nil {
		return nil, err
	}

	dtos := make([]NoteDTO, len(rows))
	for i, r := range rows {
		dtos[i] = toDTOFromFilter(r)
	}
	return dtos, nil
}

// FormatNoteMarkdown generates a cleanly separated Markdown representation of the note.
func FormatNoteMarkdown(note NoteDTO) string {
	var b strings.Builder
	title := note.SlideTitle
	if strings.TrimSpace(title) == "" {
		title = "Quick Note"
	}
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Your original note\n\n")
	b.WriteString(note.RawText + "\n")

	hasEnrichment := (note.Summary != nil && strings.TrimSpace(*note.Summary) != "") || len(note.Tags) > 0 || len(note.Links) > 0
	if hasEnrichment {
		b.WriteString("\n## AI additions\n\n")
		if note.Summary != nil && strings.TrimSpace(*note.Summary) != "" {
			b.WriteString("Summary: " + *note.Summary + "\n\n")
		}
		if len(note.Tags) > 0 {
			b.WriteString("### Tags\n")
			tagStrings := make([]string, len(note.Tags))
			for i, t := range note.Tags {
				if strings.HasPrefix(t, "#") {
					tagStrings[i] = t
				} else {
					tagStrings[i] = "#" + t
				}
			}
			b.WriteString(strings.Join(tagStrings, " ") + "\n\n")
		}
		if len(note.Links) > 0 {
			b.WriteString("### Related links\n")
			for _, l := range note.Links {
				b.WriteString("- " + l + "\n")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// EmitQuickNoteReviewed emits the quick_note_reviewed event with privacy protection.
func EmitQuickNoteReviewed(events analytics.Events, learnerID, noteID, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	if platform == "" {
		platform = "web"
	}
	if appVersion == "" {
		appVersion = "0.1.0"
	}
	return events.Capture(learnerID, "quick_note_reviewed", map[string]any{
		"source":      "api",
		"platform":    platform,
		"app_version": appVersion,
		"note_id":     noteID,
	})
}

// EmitQuickNoteDeleted emits the quick_note_deleted event with privacy protection.
func EmitQuickNoteDeleted(events analytics.Events, learnerID, noteID, platform, appVersion string) error {
	if events == nil {
		return nil
	}
	if platform == "" {
		platform = "web"
	}
	if appVersion == "" {
		appVersion = "0.1.0"
	}
	return events.Capture(learnerID, "quick_note_deleted", map[string]any{
		"source":      "api",
		"platform":    platform,
		"app_version": appVersion,
		"note_id":     noteID,
	})
}

func toDTOFromFilter(r db.FilterQuickNotesRow) NoteDTO {
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
