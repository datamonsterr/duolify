package notes

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/datamonsterr/duolify/services/api/internal/credits"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// FreeFormatTurnsPerDay is how many Voice quick notes a Learner gets formatted for free each local
// day. It resets on the same Learner-local clock as the voice daily cap.
const FreeFormatTurnsPerDay = 5

// ErrFormatNotAvailable means the note is not a Voice note, or it already holds a formatting grant,
// or it is already formatted at its current text.
var ErrFormatNotAvailable = errors.New("format not available for this note")

// FormatTurns is the Learner's free formatting allowance for the current local day.
type FormatTurns struct {
	Limit    int       `json:"limit"`
	Used     int       `json:"used"`
	Left     int       `json:"left"`
	ResetsAt time.Time `json:"resets_at"`
}

func (s *Service) localDay(ctx context.Context, learnerID string) (string, time.Time, error) {
	return credits.LocalDay(ctx, s.pool, learnerID, time.Now())
}

// FormatTurns reports the free turns left today.
func (s *Service) FormatTurns(ctx context.Context, learnerID string) (FormatTurns, error) {
	day, next, err := s.localDay(ctx, learnerID)
	if err != nil {
		return FormatTurns{}, err
	}
	var used int
	err = s.pool.QueryRow(ctx, `SELECT used FROM quick_note_format_turns WHERE learner_id=$1::uuid AND day=$2::date`, learnerID, day).Scan(&used)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return FormatTurns{}, err
	}
	return FormatTurns{Limit: FreeFormatTurnsPerDay, Used: used, Left: max(0, FreeFormatTurnsPerDay-used), ResetsAt: next.UTC()}, nil
}

// takeFreeTurn gives the note a free grant and counts it, atomically. The grant column makes it
// idempotent per note (a note never takes a second turn); the counter upsert is the atomic limit.
// It returns false when the note already has a grant or the day's turns are used.
func takeFreeTurn(ctx context.Context, tx pgx.Tx, learnerID, noteID, day string) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE quick_notes SET format_grant='free',format_grant_day=$3::date WHERE id=$1::uuid AND learner_id=$2::uuid AND format_grant IS NULL`, noteID, learnerID, day)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	var used int
	err = tx.QueryRow(ctx, `INSERT INTO quick_note_format_turns(learner_id,day,used) VALUES($1::uuid,$2::date,1)
 ON CONFLICT (learner_id,day) DO UPDATE SET used=quick_note_format_turns.used+1 WHERE quick_note_format_turns.used<$3
 RETURNING used`, learnerID, day, FreeFormatTurnsPerDay).Scan(&used)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = tx.Exec(ctx, `UPDATE quick_notes SET format_grant=NULL,format_grant_day=NULL WHERE id=$1::uuid AND learner_id=$2::uuid`, noteID, learnerID)
		return false, err
	}
	return err == nil, err
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// refundFormatGrant ends a note's grant without formatting it. A free grant gives its turn back,
// once: the grant column is cleared in the same statement, so a repeat refunds nothing.
func refundFormatGrant(ctx context.Context, q execer, learnerID, noteID string) (bool, error) {
	// RETURNING shows the cleared day, so the day to give back is read first, in the same statement.
	tag, err := q.Exec(ctx, `WITH old AS (SELECT learner_id,format_grant_day FROM quick_notes WHERE id=$1::uuid AND learner_id=$2::uuid AND format_grant='free' FOR UPDATE),
 clr AS (UPDATE quick_notes SET format_grant=NULL,format_grant_day=NULL WHERE id=$1::uuid AND learner_id=$2::uuid AND format_grant='free' RETURNING id)
 UPDATE quick_note_format_turns t SET used=GREATEST(t.used-1,0) FROM old,clr WHERE t.learner_id=old.learner_id AND t.day=old.format_grant_day`, noteID, learnerID)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		return true, nil
	}
	_, err = q.Exec(ctx, `UPDATE quick_notes SET format_grant=NULL,format_grant_day=NULL WHERE id=$1::uuid AND learner_id=$2::uuid`, noteID, learnerID)
	return false, err
}

// voiceGrant reports whether the note was dictated (its anchor says so) and its current grant.
func (s *Service) voiceGrant(ctx context.Context, learnerID, noteID string) (voice bool, grant string, err error) {
	var g *string
	err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM quick_note_anchors a WHERE a.note_id=n.id AND a.input_kind='voice'),n.format_grant FROM quick_notes n WHERE n.id=$1::uuid AND n.learner_id=$2::uuid`, noteID, learnerID).Scan(&voice, &g)
	if g != nil {
		grant = *g
	}
	return voice, grant, err
}

// FormatNote is the Learner's explicit Format request for a saved Voice note. It uses a free turn
// when one is left and otherwise asks the worker to charge the existing quick_note price. Calling
// it again while a grant is held, or for a note already formatted at its current text, is refused.
func (s *Service) FormatNote(ctx context.Context, learnerID, noteID string) (*NoteDTO, error) {
	current, err := s.GetNote(ctx, learnerID, noteID)
	if err != nil {
		return nil, err
	}
	if current.Anchor == nil || current.Anchor.Input != "voice" || current.FormattedBody != "" || current.Status == "enriching" || current.Status == "enrichment_pending" {
		return nil, ErrFormatNotAvailable
	}
	queue, err := s.NotesQueue(slog.Default())
	if err != nil {
		return nil, err
	}
	day, _, err := s.localDay(ctx, learnerID)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	took, err := takeFreeTurn(ctx, tx, learnerID, noteID, day)
	if err != nil {
		return nil, err
	}
	kind := "free"
	if !took {
		kind = "paid"
		tag, e := tx.Exec(ctx, `UPDATE quick_notes SET format_grant='paid' WHERE id=$1::uuid AND learner_id=$2::uuid AND format_grant IS NULL`, noteID, learnerID)
		if e != nil {
			return nil, e
		}
		if tag.RowsAffected() != 1 {
			return nil, ErrFormatNotAvailable
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE quick_notes SET status='enrichment_pending',updated_at=now() WHERE id=$1::uuid AND learner_id=$2::uuid AND status IN ('raw_only','enrichment_failed','enriched')`, noteID, learnerID); err != nil {
		return nil, err
	}
	if _, err = queue.InsertTx(ctx, tx, NoteEnrichArgs{LearnerID: learnerID, NoteID: noteID, ExpectedVersion: current.RawVersion}, nil); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.emitFormatGranted(ctx, learnerID, noteID, kind)
	return s.GetNote(ctx, learnerID, noteID)
}

func (s *Service) emitFormatGranted(ctx context.Context, learnerID, noteID, kind string) {
	if s.events == nil {
		return
	}
	left := -1
	if t, err := s.FormatTurns(ctx, learnerID); err == nil {
		left = t.Left
	}
	meta := s.AnalyticsMetadata(ctx)
	props := map[string]any{"source": "api", "platform": meta.Platform, "app_version": meta.AppVersion, "note_id": noteID, "kind": kind}
	if left >= 0 {
		props["free_left"] = left
	}
	_ = s.events.Capture(learnerID, "quick_note_format_granted", props)
}

// attachFormatting adds the title and cleaned body (only while they match the current raw text)
// and the Format offer for a Voice note saved raw.
func (s *Service) attachFormatting(ctx context.Context, learnerID string, dtos []NoteDTO) error {
	ids := make([]string, 0, len(dtos))
	for _, d := range dtos {
		ids = append(ids, d.ID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT n.id::text,e.title,e.formatted_body FROM quick_notes n JOIN quick_note_enrichments e ON e.note_id=n.id AND e.version=n.raw_version WHERE n.learner_id=$1::uuid AND n.id=ANY($2::uuid[]) AND e.formatted_body<>''`, learnerID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type pair struct{ title, body string }
	found := map[string]pair{}
	for rows.Next() {
		var id string
		var p pair
		if err = rows.Scan(&id, &p.title, &p.body); err != nil {
			return err
		}
		found[id] = p
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range dtos {
		if p, ok := found[dtos[i].ID]; ok {
			dtos[i].Title, dtos[i].FormattedBody = p.title, p.body
			continue
		}
		a := dtos[i].Anchor
		if a != nil && a.Input == "voice" && dtos[i].Status == "raw_only" {
			dtos[i].FormatOffer = true
		}
	}
	return nil
}
