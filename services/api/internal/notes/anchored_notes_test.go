package notes_test

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/notes"
)

func anchoredInput(a *notes.AnchorInput) notes.SaveNoteInput {
	lesson, slide := "11111111-1111-4111-8111-111111111111", 2
	return notes.SaveNoteInput{ActionKey: "k", Source: "slide", RawText: "note", LessonID: &lesson, SlideID: &slide, Anchor: a}
}

func TestAnchorValidation(t *testing.T) {
	rect := func(x, y, w, h float64) *notes.AnchorRect { return &notes.AnchorRect{X: x, Y: y, W: w, H: h} }
	cases := []struct {
		name string
		in   notes.SaveNoteInput
		ok   bool
	}{
		{"time only", anchoredInput(&notes.AnchorInput{PlaybackMs: 12000, Input: "typed"}), true},
		{"region", anchoredInput(&notes.AnchorInput{PlaybackMs: 0, Input: "voice", Rect: rect(0.1, 0.2, 0.5, 0.4)}), true},
		{"full stage", anchoredInput(&notes.AnchorInput{Input: "typed", Rect: rect(0, 0, 1, 1)}), true},
		{"negative time", anchoredInput(&notes.AnchorInput{PlaybackMs: -1, Input: "typed"}), false},
		{"day-long time", anchoredInput(&notes.AnchorInput{PlaybackMs: 90_000_000, Input: "typed"}), false},
		{"unknown input", anchoredInput(&notes.AnchorInput{Input: "telepathy"}), false},
		{"no input", anchoredInput(&notes.AnchorInput{}), false},
		{"outside stage", anchoredInput(&notes.AnchorInput{Input: "typed", Rect: rect(0.8, 0, 0.5, 0.5)}), false},
		{"empty rect", anchoredInput(&notes.AnchorInput{Input: "typed", Rect: rect(0.1, 0.1, 0, 0.5)}), false},
		{"negative origin", anchoredInput(&notes.AnchorInput{Input: "typed", Rect: rect(-0.1, 0, 0.5, 0.5)}), false},
		{"NaN", anchoredInput(&notes.AnchorInput{Input: "typed", Rect: rect(math.NaN(), 0, 0.5, 0.5)}), false},
	}
	for _, c := range cases {
		err := c.in.Validate()
		if c.ok && err != nil || !c.ok && !errors.Is(err, notes.ErrInvalidAnchor) {
			t.Errorf("%s: err=%v", c.name, err)
		}
	}
	chat := anchoredInput(&notes.AnchorInput{Input: "typed"})
	chat.Source = "chat"
	if err := chat.Validate(); !errors.Is(err, notes.ErrInvalidAnchor) {
		t.Errorf("anchor on a chat note must be rejected: %v", err)
	}
	noLesson := anchoredInput(&notes.AnchorInput{Input: "typed"})
	noLesson.LessonID = nil
	if err := noLesson.Validate(); !errors.Is(err, notes.ErrInvalidAnchor) {
		t.Errorf("anchor without a Lesson must be rejected: %v", err)
	}
}

func TestAnchorJSONShape(t *testing.T) {
	var in notes.SaveNoteInput
	body := `{"action_key":"a","source":"slide","slide_id":1,"lesson_id":"l","raw_text":"x","anchor":{"playback_ms":4500,"rect":{"x":0.1,"y":0.2,"w":0.3,"h":0.4},"input":"voice"}}`
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatal(err)
	}
	if in.Anchor == nil || in.Anchor.PlaybackMs != 4500 || in.Anchor.Rect.H != 0.4 || in.Anchor.Input != "voice" {
		t.Fatalf("decoded %+v", in.Anchor)
	}
}
