package chat

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestNoteActionKeyIsStablePerMessage(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	if noteActionKey(id) != "tutor-message:"+id {
		t.Fatal("the action key must be derived from the message id alone")
	}
	if noteActionKey(id) == noteActionKey("22222222-2222-4222-8222-222222222222") {
		t.Fatal("different messages must not share a note")
	}
}

func TestClipNoteStaysWithinTheNoteBoundOnRuneBoundaries(t *testing.T) {
	for name, tc := range map[string]struct {
		in        string
		truncated bool
	}{
		"short":             {"hello", false},
		"exactly at bound":  {strings.Repeat("a", noteMaxBytes), false},
		"one over":          {strings.Repeat("a", noteMaxBytes+1), true},
		"multibyte split":   {strings.Repeat("a", noteMaxBytes-1) + "é", true},
		"all multibyte":     {strings.Repeat("é", 2500), true},
		"emoji at boundary": {strings.Repeat("a", noteMaxBytes-2) + "😀", true},
	} {
		t.Run(name, func(t *testing.T) {
			out, cut := clipNote(tc.in)
			if cut != tc.truncated || len(out) > noteMaxBytes || !utf8.ValidString(out) || !strings.HasPrefix(tc.in, out) {
				t.Fatalf("len=%d cut=%v valid=%v", len(out), cut, utf8.ValidString(out))
			}
			if !cut && out != tc.in {
				t.Fatal("untruncated text must be unchanged")
			}
		})
	}
}

func TestSavableReply(t *testing.T) {
	for name, tc := range map[string]struct {
		role, status string
		want         error
	}{
		"complete reply":     {"assistant", "complete", nil},
		"stopped reply":      {"assistant", "stopped", nil},
		"interrupted prefix": {"assistant", "interrupted", errIncompleteReply},
		"learner question":   {"user", "complete", errNotAReply},
	} {
		t.Run(name, func(t *testing.T) {
			if err := savableReply(tc.role, tc.status); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
