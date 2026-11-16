package sharing_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/sharing"
)

const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestSnapshotAllowlistAcceptsNarrationAudioRefs(t *testing.T) {
	l := lesson("Phases", "<p>a</p>")
	l.NarrationAudio = []sharing.NarrationAudioRef{{SlideID: "s1", ContentHash: hashA, DurationMS: 3000, Characters: 40}}
	raw, err := json.Marshal(sharing.Snapshot{Title: "T", Units: []sharing.SnapshotUnit{{Title: "U", Lessons: []sharing.SnapshotLesson{l}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err = sharing.CheckAllowlist(raw); err != nil {
		t.Fatalf("narration audio refs rejected: %v", err)
	}
	// Hashes only: never object keys, voices, models or text.
	for _, banned := range []string{"audio_key", "alignment_key", "voice_id", "model_id", "lessons/narration"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("snapshot ref leaks %q", banned)
		}
	}
	bad := strings.Replace(string(raw), `"content_hash"`, `"audio_key"`, 1)
	if sharing.CheckAllowlist([]byte(bad)) == nil {
		t.Error("audio_key must stay outside the allowlist")
	}
}

func TestReusableNarrationKeepsOnlyValidRefsForExistingSlides(t *testing.T) {
	l := lesson("Phases", "<p>a</p>", "<p>b</p>")
	l.NarrationAudio = []sharing.NarrationAudioRef{
		{SlideID: "s1", ContentHash: hashA, DurationMS: 3000},
		{SlideID: "s1", ContentHash: strings.Repeat("b", 64), DurationMS: 1000}, // duplicate slide: first wins
		{SlideID: "gone", ContentHash: hashA, DurationMS: 3000},                 // slide not in the lesson
		{SlideID: "s2", ContentHash: "not-a-hash", DurationMS: 3000},
		{SlideID: "s2", ContentHash: strings.Repeat("c", 64), DurationMS: 0}, // no duration, not playable
	}
	got := sharing.ReusableNarration(l)
	if len(got) != 1 || got[0].SlideID != "s1" || got[0].ContentHash != hashA {
		t.Fatalf("refs=%+v", got)
	}
}
