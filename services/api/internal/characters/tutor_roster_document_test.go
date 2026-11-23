package characters_test

import (
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

func TestTutorRosterCarriesDocumentOnlyForAvailableTutors(t *testing.T) {
	t.Setenv("FEATURE_CUSTOM_CHARACTERS", "true")
	doc := &characters.SpecV2{}
	cast := castWith(t, func(sp *characters.PinnedSpeaker) { sp.Document = doc })
	roster := characters.BuildTutorRoster(cast, "course")
	if roster.Tutors[0].Document != doc {
		t.Fatalf("guide should carry its v2 document: %+v", roster.Tutors[0])
	}
	if roster.Tutors[1].Document != nil {
		t.Fatal("a speaker without a document carries none")
	}
	cast = castWith(t, func(sp *characters.PinnedSpeaker) { sp.Document = doc; sp.Suppressed = true })
	roster = characters.BuildTutorRoster(cast, "course")
	if roster.Tutors[0].Available || roster.Tutors[0].Document != nil {
		t.Fatalf("a suppressed tutor must not leak its document: %+v", roster.Tutors[0])
	}
}
