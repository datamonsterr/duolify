package notes

import (
	"context"
	"errors"
	"math"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidAnchor indicates a Lesson-moment anchor that is malformed or not allowed for the note.
var ErrInvalidAnchor = errors.New("invalid note anchor")

const maxAnchorPlaybackMs = 24 * 60 * 60 * 1000

// AnchorRect is a region in 0..1 coordinates of the 16:9 stage.
type AnchorRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// AnchorInput pins a note to a moment of a Lesson. The Slide is the note's own slide_id.
type AnchorInput struct {
	PlaybackMs int         `json:"playback_ms"`
	Rect       *AnchorRect `json:"rect,omitempty"`
	// Input records whether the text was typed or dictated.
	Input string `json:"input"`
}

// AnchorDTO is returned with a note. SourceState is "unavailable" once the Lesson was removed:
// the note stays readable and carries no Lesson to open.
type AnchorDTO struct {
	PlaybackMs  int         `json:"playback_ms"`
	Rect        *AnchorRect `json:"rect,omitempty"`
	Input       string      `json:"input"`
	SourceState string      `json:"source_state"`
}

func finiteUnit(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1 }

func (a AnchorInput) validate(in SaveNoteInput) error {
	if in.Source != "slide" || in.SlideID == nil || in.LessonID == nil || *in.LessonID == "" {
		return ErrInvalidAnchor
	}
	if a.PlaybackMs < 0 || a.PlaybackMs > maxAnchorPlaybackMs {
		return ErrInvalidAnchor
	}
	if a.Input != "typed" && a.Input != "voice" {
		return ErrInvalidAnchor
	}
	if r := a.Rect; r != nil {
		const eps = 1e-6
		if !finiteUnit(r.X) || !finiteUnit(r.Y) || !finiteUnit(r.W) || !finiteUnit(r.H) ||
			r.W <= 0 || r.H <= 0 || r.X+r.W > 1+eps || r.Y+r.H > 1+eps {
			return ErrInvalidAnchor
		}
	}
	return nil
}

func recordAnchor(ctx context.Context, tx pgx.Tx, learnerID, noteID string, a *AnchorInput) error {
	var x, y, w, h *float64
	if a.Rect != nil {
		x, y, w, h = &a.Rect.X, &a.Rect.Y, &a.Rect.W, &a.Rect.H
	}
	_, err := tx.Exec(ctx, `INSERT INTO quick_note_anchors(note_id,learner_id,playback_ms,rect_x,rect_y,rect_w,rect_h,input_kind) VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8) ON CONFLICT (note_id) DO NOTHING`,
		noteID, learnerID, a.PlaybackMs, x, y, w, h, a.Input)
	return err
}

// attachAnchors adds the Lesson-moment anchor to notes that have one.
func (s *Service) attachAnchors(ctx context.Context, learnerID string, dtos []NoteDTO) error {
	ids := make([]string, 0, len(dtos))
	for _, d := range dtos {
		if d.Source == "slide" {
			ids = append(ids, d.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.pool.Query(ctx, `SELECT note_id::text,playback_ms,rect_x,rect_y,rect_w,rect_h,input_kind FROM quick_note_anchors WHERE learner_id=$1::uuid AND note_id=ANY($2::uuid[])`, learnerID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := map[string]*AnchorDTO{}
	for rows.Next() {
		var id string
		a := &AnchorDTO{}
		var x, y, w, h *float64
		if err = rows.Scan(&id, &a.PlaybackMs, &x, &y, &w, &h, &a.Input); err != nil {
			return err
		}
		if x != nil && y != nil && w != nil && h != nil {
			a.Rect = &AnchorRect{X: *x, Y: *y, W: *w, H: *h}
		}
		found[id] = a
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range dtos {
		if a := found[dtos[i].ID]; a != nil {
			a.SourceState = "available"
			if dtos[i].LessonID == nil {
				a.SourceState = "unavailable"
			}
			dtos[i].Anchor = a
		}
	}
	return nil
}
