package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
	"github.com/jackc/pgx/v5"
)

// noteMaxBytes is the Quick note bound; a longer reply is clipped, never refused.
const noteMaxBytes = 2000

var (
	errNotAReply       = errors.New("not an assistant reply")
	errIncompleteReply = errors.New("reply was cut off")
)

// NoteSaver is the Quick notes boundary: chat reuses the E9 store instead of keeping a second one.
type NoteSaver interface {
	SaveNote(ctx context.Context, learnerID string, in notes.SaveNoteInput) (*notes.NoteDTO, error)
}

// noteActionKey makes a save idempotent: one reply is one note, however often the request repeats.
func noteActionKey(messageID string) string { return "tutor-message:" + messageID }

// clipNote shortens text to the note bound on a rune boundary and reports whether it did.
func clipNote(text string) (string, bool) {
	if len(text) <= noteMaxBytes {
		return text, false
	}
	cut := noteMaxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut], true
}

func savableReply(role, status string) error {
	if role != "assistant" {
		return errNotAReply
	}
	if status == "interrupted" {
		return errIncompleteReply
	}
	return nil
}

type saveReplyInput struct {
	LessonID *string `json:"lesson_id"`
}

// saveReply stores one of the Learner's own tutor replies as a raw Quick note. It never reserves
// Credits: enrichment is a separate, separately quoted action owned by quick notes.
func (s *Service) saveReply(w http.ResponseWriter, r *http.Request, owner, message string) {
	if s.opts.Notes == nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	var in saveReplyInput
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<10))
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_request")
		return
	}
	if raw = bytes.TrimSpace(raw); len(raw) > 0 && !bytes.Equal(raw, []byte("null")) {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil {
			fail(w, http.StatusBadRequest, "invalid_request")
			return
		}
	}
	if in.LessonID != nil && !validUUID(*in.LessonID) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	var thread, course, role, status, content string
	err = s.opts.Pool.QueryRow(r.Context(), `SELECT m.thread_id::text,t.course_id::text,m.role,m.status,m.content FROM tutor_messages m JOIN tutor_threads t ON t.id=m.thread_id
WHERE m.id=$1::uuid AND m.learner_id=$2::uuid AND t.learner_id=$2::uuid`, message, owner).Scan(&thread, &course, &role, &status, &content)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "not_found")
		return
	}
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	switch savableReply(role, status) {
	case errNotAReply:
		fail(w, http.StatusNotFound, "not_found")
		return
	case errIncompleteReply:
		fail(w, http.StatusConflict, "incomplete_reply")
		return
	}
	text, truncated := clipNote(strings.TrimSpace(content))
	note, err := s.opts.Notes.SaveNote(r.Context(), owner, notes.SaveNoteInput{
		ActionKey: noteActionKey(message),
		CourseID:  &course,
		LessonID:  in.LessonID,
		Source:    "chat",
		RawText:   text,
		Chat:      &notes.ChatProvenance{ThreadID: thread, MessageID: message},
	})
	switch {
	case errors.Is(err, notes.ErrUnauthorizedSource):
		// The lesson is not the Learner's in this Course, or the thread was deleted meanwhile.
		fail(w, http.StatusNotFound, "not_found")
		return
	case err != nil:
		s.opts.Logger.Warn("tutor reply note save failed", "error", err)
		fail(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	respond(w, http.StatusOK, struct {
		*notes.NoteDTO
		Truncated bool `json:"truncated"`
	}{note, truncated})
}
