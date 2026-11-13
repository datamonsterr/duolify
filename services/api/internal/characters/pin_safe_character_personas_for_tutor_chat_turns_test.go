package characters_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/datamonsterr/duolify/services/api/internal/characters"
)

func castWith(t *testing.T, mutate func(*characters.PinnedSpeaker)) characters.LessonCastSnapshot {
	t.Helper()
	manifest, _ := characters.ProjectDefaultCastManifest("c", "")
	cast := manifest.Cast
	cast.Speakers = map[string]characters.PinnedSpeaker{}
	for key, spec := range map[string]characters.CharacterSpec{"guide": characters.DefaultGuideFixture(), "supporter": characters.DefaultSupporterFixture()} {
		sp := characters.PinnedSpeaker{SpeakerKey: key, Role: key, CharacterID: "11111111-1111-4111-8111-11111111111" + map[string]string{"guide": "1", "supporter": "2"}[key], CharacterVersion: 3, Spec: spec}
		if key == "guide" {
			mutate(&sp)
		}
		cast.Speakers[key] = sp
	}
	return cast
}

func TestPersonaIsBoundedEscapedDataOnly(t *testing.T) {
	spec := characters.DefaultGuideFixture()
	spec.Info.Name = "Orbit"
	spec.Info.Bio = "Line one\nline two\t{x} `tick` back\\slash"
	spec.Info.Backstory = strings.Repeat("moon ", 400)
	raw, err := characters.BuildTutorPersona(spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > 2000 {
		t.Fatalf("persona exceeds 2000 bytes: %d", len(raw))
	}
	var got map[string]any
	if err = json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "bio", "backstory", "traits", "speaking_style", "persona_prompt"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing key %s", key)
		}
	}
	if len(got) != 6 {
		t.Fatalf("closed key set expected: %v", got)
	}
	for _, forbidden := range []string{"\n", "\t", "{x}", "`", "\\\\"} {
		if strings.Contains(got["bio"].(string), forbidden) {
			t.Fatalf("bio keeps %q: %q", forbidden, got["bio"])
		}
	}
	if n := len([]rune(got["backstory"].(string))); n > 600 {
		t.Fatalf("backstory not bounded: %d", n)
	}
	for _, leaked := range []string{"voice", "palette", "appearance", "character_id", "svg"} {
		if strings.Contains(strings.ToLower(raw), leaked) {
			t.Fatalf("persona carries non-persona field %s", leaked)
		}
	}
}

func TestPersonaRejectsInvalidSpecsAndOversizedText(t *testing.T) {
	spec := characters.DefaultGuideFixture()
	spec.Info.Bio = "<script>alert(1)</script>"
	if _, err := characters.BuildTutorPersona(spec); err == nil {
		t.Fatal("markup persona accepted")
	}
	spec = characters.DefaultGuideFixture()
	spec.Info.Name = strings.Repeat("é", 40)
	spec.Info.Bio = strings.Repeat("é", 280)
	spec.Info.Backstory = strings.Repeat("é", 800)
	spec.Personality.SpeakingStyle = strings.Repeat("é", 200)
	if _, err := characters.BuildTutorPersona(spec); err == nil {
		t.Fatal("multi-byte persona above the 2000-byte bound accepted")
	}
}

func TestTutorSelectorResolvesPinnedSpeakerOrSafeDefault(t *testing.T) {
	ok := castWith(t, func(*characters.PinnedSpeaker) {})
	cases := []struct {
		name      string
		cast      characters.LessonCastSnapshot
		source    string
		requested string
		custom    bool
		wantKey   string
		wantID    string
		fallback  string
	}{
		{"guide by default", ok, "course", "", true, "guide", "11111111-1111-4111-8111-111111111111", ""},
		{"explicit supporter", ok, "lesson", "supporter", true, "supporter", "11111111-1111-4111-8111-111111111112", ""},
		{"unknown speaker falls back to guide", ok, "course", "supporter_2", true, "guide", "11111111-1111-4111-8111-111111111111", "speaker_unavailable"},
		{"invalid persona uses built-in guide", castWith(t, func(s *characters.PinnedSpeaker) { s.Spec.Info.Bio = "<b>x</b>" }), "course", "", true, "guide", "default-guide", "invalid_persona"},
		{"suppressed when custom characters are off", ok, "course", "", false, "guide", "default-guide", "suppressed"},
		{"missing guide uses default cast", characters.LessonCastSnapshot{Speakers: map[string]characters.PinnedSpeaker{}}, "course", "", true, "guide", "default-guide", "persona_unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := characters.ResolveTutorPersona(c.cast, c.source, c.requested, c.custom)
			if p.SpeakerKey != c.wantKey || p.CharacterID != c.wantID || p.Fallback != c.fallback {
				t.Fatalf("got %+v", p)
			}
			if p.Persona == "" || p.Name == "" || p.CharacterVersion < 1 {
				t.Fatalf("safe persona must always be complete: %+v", p)
			}
			if c.fallback != "" && c.fallback != "speaker_unavailable" && strings.Contains(p.Persona, "<b>") {
				t.Fatal("malformed persona reached the Engine payload")
			}
		})
	}
}

func TestTutorSpeakerKeysAreClosed(t *testing.T) {
	for _, key := range []string{"", "guide", "supporter", "supporter_2"} {
		if !characters.ValidTutorSpeaker(key) {
			t.Fatalf("%q must be valid", key)
		}
	}
	for _, key := range []string{"system", "Guide", "supporter_3", "../guide", "guide "} {
		if characters.ValidTutorSpeaker(key) {
			t.Fatalf("%q must be rejected", key)
		}
	}
}
