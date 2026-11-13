package notes

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ChatProvenance names the tutor reply a note is saved from. It is set only by the chat package,
// never decoded from a request body.
type ChatProvenance struct {
	ThreadID  string
	MessageID string
}

// ChatSourceDTO tells the client whether the originating reply still exists. An unavailable source
// carries no ids: the thread was deleted and the note stays readable on its own.
type ChatSourceDTO struct {
	State     string  `json:"state"`
	ThreadID  *string `json:"thread_id,omitempty"`
	MessageID *string `json:"message_id,omitempty"`
}

// lockChatSource takes a shared lock on the Learner's own thread so a concurrent delete either
// waits for this save (and then marks it unavailable) or runs first and refuses it.
func lockChatSource(ctx context.Context, tx pgx.Tx, learnerID string, c *ChatProvenance) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM tutor_threads t JOIN tutor_messages m ON m.thread_id=t.id
WHERE t.id=$1::uuid AND m.id=$2::uuid AND t.learner_id=$3::uuid AND m.learner_id=$3::uuid AND m.role='assistant' FOR SHARE OF t`, c.ThreadID, c.MessageID, learnerID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnauthorizedSource
	}
	return err
}

func recordChatSource(ctx context.Context, tx pgx.Tx, learnerID, noteID string, lessonID *string, c *ChatProvenance) error {
	_, err := tx.Exec(ctx, `INSERT INTO quick_note_chat_sources(note_id,learner_id,thread_id,message_id,lesson_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid) ON CONFLICT (note_id) DO NOTHING`, noteID, learnerID, c.ThreadID, c.MessageID, lessonID)
	return err
}

// attachChatSources adds provenance to notes saved from tutor chat.
func (s *Service) attachChatSources(ctx context.Context, learnerID string, dtos []NoteDTO) error {
	ids := make([]string, 0, len(dtos))
	for _, d := range dtos {
		if d.Source == "chat" {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT note_id::text,unavailable,thread_id::text,message_id::text FROM quick_note_chat_sources WHERE learner_id=$1::uuid AND note_id=ANY($2::uuid[])`, learnerID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[string]*ChatSourceDTO{}
	for rows.Next() {
		var id string
		var gone bool
		var thread, message *string
		if err = rows.Scan(&id, &gone, &thread, &message); err != nil {
			return err
		}
		if gone {
			found[id] = &ChatSourceDTO{State: "unavailable"}
		} else {
			found[id] = &ChatSourceDTO{State: "available", ThreadID: thread, MessageID: message}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range dtos {
		dtos[i].ChatSource = found[dtos[i].ID]
	}
	return nil
}
