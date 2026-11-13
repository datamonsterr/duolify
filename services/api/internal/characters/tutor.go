package characters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	// maxTutorField and maxTutorPersonaBytes mirror the Engine's closed persona contract.
	maxTutorField        = 600
	maxTutorPersonaBytes = 2000
)

var errPersonaTooLarge = errors.New("persona exceeds the bounded size")

// tutorSpeakerKeys are the only speaker keys a Cast can carry, in roster order.
var tutorSpeakerKeys = []string{"guide", "supporter", "supporter_2"}

// ValidTutorSpeaker accepts the empty value (the main guide) and the three closed Cast speaker keys.
func ValidTutorSpeaker(key string) bool {
	if key == "" {
		return true
	}
	for _, k := range tutorSpeakerKeys {
		if k == key {
			return true
		}
	}
	return false
}

// TutorPersona is the resolved, pinned persona for one Tutor chat turn. Persona is the bounded JSON
// data sent to the Engine; it is never stored with the turn, only the references are.
type TutorPersona struct {
	SpeakerKey       string
	CharacterID      string
	CharacterVersion int
	CastSource       string
	CastVersion      int
	Name             string
	Fallback         string
	Persona          string
}

// TutorChoice is one entry of the learner-facing tutor picker.
type TutorChoice struct {
	SpeakerKey       string `json:"speaker_key"`
	Role             string `json:"role"`
	Name             string `json:"name"`
	CharacterVersion int    `json:"character_version"`
	Available        bool   `json:"available"`
}

// TutorRoster lists the effective Cast for the picker.
type TutorRoster struct {
	CastSource     string        `json:"cast_source"`
	CastVersion    int           `json:"cast_version"`
	DefaultSpeaker string        `json:"default_speaker"`
	Tutors         []TutorChoice `json:"tutors"`
}

// plainPersonaText turns learner text into bounded one-line data: control characters and the markup,
// template and escape characters the Engine refuses become spaces, runs of spaces collapse.
func plainPersonaText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || strings.ContainsRune("<>{}`\\", r) {
			r = ' '
		}
		b.WriteRune(r)
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > maxTutorField {
		out = strings.TrimSpace(string([]rune(out)[:maxTutorField]))
	}
	return out
}

// BuildTutorPersona validates a spec and serializes only its info and personality as bounded data.
func BuildTutorPersona(spec CharacterSpec) (string, error) {
	if err := ValidateSpec(&spec); err != nil {
		return "", err
	}
	traits := make([]string, 0, len(spec.Personality.Traits))
	for _, t := range spec.Personality.Traits {
		if clean := plainPersonaText(t); clean != "" {
			traits = append(traits, clean)
		}
	}
	doc := struct {
		Name          string   `json:"name"`
		Bio           string   `json:"bio"`
		Backstory     string   `json:"backstory"`
		Traits        []string `json:"traits"`
		SpeakingStyle string   `json:"speaking_style"`
		PersonaPrompt string   `json:"persona_prompt"`
	}{plainPersonaText(spec.Info.Name), plainPersonaText(spec.Info.Bio), plainPersonaText(spec.Info.Backstory), traits,
		plainPersonaText(spec.Personality.SpeakingStyle), plainPersonaText(spec.Personality.PersonaPrompt)}
	if doc.Name == "" {
		return "", errors.New("persona name is empty")
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	raw := strings.TrimSpace(out.String())
	if len(raw) > maxTutorPersonaBytes {
		return "", errPersonaTooLarge
	}
	return raw, nil
}

func isBuiltinCharacter(id string) bool {
	if _, ok := builtIn(id); ok {
		return true
	}
	return id == "char-provisional-glint" || id == "char-provisional-moss"
}

func builtinTutorName(id string) string {
	switch id {
	case "char-provisional-glint", "default-guide":
		return DefaultGuideFixture().Info.Name
	case "char-provisional-moss", "default-supporter":
		return DefaultSupporterFixture().Info.Name
	case "default-companion":
		return DefaultCompanionFixture().Info.Name
	}
	return ""
}

// safeDefaultPersona is the approved built-in persona of the speaker's role.
func safeDefaultPersona(key, source string, castVersion int, reason string) TutorPersona {
	id := map[string]string{"guide": "default-guide", "supporter": "default-supporter", "supporter_2": "default-companion"}[key]
	if id == "" {
		key, id = "guide", "default-guide"
	}
	spec, _ := builtIn(id)
	raw, _ := BuildTutorPersona(spec)
	return TutorPersona{SpeakerKey: key, CharacterID: id, CharacterVersion: 1, CastSource: source, CastVersion: castVersion, Name: spec.Info.Name, Fallback: reason, Persona: raw}
}

// ResolveTutorPersona picks the requested speaker (default: the main guide) from a Cast and validates
// its persona. Anything unusable resolves to the built-in default of the same role, so a malformed
// persona never leaves the API. custom reports whether custom Characters are enabled.
func ResolveTutorPersona(cast LessonCastSnapshot, source, requested string, custom bool) TutorPersona {
	key, reason := requested, ""
	if key == "" {
		key = "guide"
	}
	speaker, ok := cast.Speakers[key]
	if !ok && key != "guide" {
		key, reason = "guide", "speaker_unavailable"
		speaker, ok = cast.Speakers[key]
	}
	if !ok {
		return safeDefaultPersona("guide", source, cast.CastVersion, "persona_unavailable")
	}
	if !custom && !isBuiltinCharacter(speaker.CharacterID) {
		return safeDefaultPersona(key, source, cast.CastVersion, "suppressed")
	}
	raw, err := BuildTutorPersona(speaker.Spec)
	if err != nil {
		code := "invalid_persona"
		if errors.Is(err, errPersonaTooLarge) {
			code = "persona_too_large"
		}
		return safeDefaultPersona(key, source, cast.CastVersion, code)
	}
	return TutorPersona{SpeakerKey: key, CharacterID: speaker.CharacterID, CharacterVersion: speaker.CharacterVersion, CastSource: source,
		CastVersion: cast.CastVersion, Name: plainPersonaText(speaker.Spec.Info.Name), Fallback: reason, Persona: raw}
}

// tutorCast loads the effective Cast server-side: the Lesson's pinned snapshot inside a Lesson that has
// one, otherwise the Course's current approved Cast (or the Default cast).
func (s *Service) tutorCast(ctx context.Context, owner, course, lessonID string) (LessonCastSnapshot, string, error) {
	if err := s.checkCourse(ctx, owner, course); err != nil {
		return LessonCastSnapshot{}, "", err
	}
	if lessonID != "" {
		var raw []byte
		err := s.pool.QueryRow(ctx, `SELECT c.snapshot FROM generated_lessons g JOIN lesson_cast_snapshots c ON c.course_id=g.course_id AND c.learner_id=g.learner_id AND c.lesson_key=g.manifest->>'cast_key' WHERE g.lesson_stub_id::text=$1 AND g.learner_id=$2 AND g.course_id=$3`, lessonID, owner, course).Scan(&raw)
		if err == nil {
			var cast LessonCastSnapshot
			if json.Unmarshal(raw, &cast) != nil {
				cast = LessonCastSnapshot{}
			}
			return cast, "lesson", nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return LessonCastSnapshot{}, "", err
		}
	}
	cast, err := s.effectiveCast(ctx, owner, course)
	if err != nil {
		return cast, "", err
	}
	source := "default"
	if cast.CastSource == "custom" {
		source = "course"
	}
	return cast, source, nil
}

// selectedCharacter is the Character id the learner chose for a speaker key in the Course Cast.
func (s *Service) selectedCharacter(ctx context.Context, owner, course, key string) string {
	sel, err := s.ReadCast(ctx, owner, course)
	if err != nil {
		return ""
	}
	switch key {
	case "guide":
		return sel.Guide
	case "supporter":
		if len(sel.Supporting) > 0 {
			return sel.Supporting[0]
		}
	case "supporter_2":
		if len(sel.Supporting) > 1 {
			return sel.Supporting[1]
		}
	}
	return ""
}

// ResolveTutor resolves and validates the persona for one turn. Ownership of the Course is checked here;
// the caller already verified the Lesson belongs to the same learner and Course.
func (s *Service) ResolveTutor(ctx context.Context, owner, course, lessonID, requested string) (TutorPersona, error) {
	if !ValidTutorSpeaker(requested) {
		return TutorPersona{}, ErrNotFound
	}
	cast, source, err := s.tutorCast(ctx, owner, course, lessonID)
	if err != nil {
		return TutorPersona{}, err
	}
	p := ResolveTutorPersona(cast, source, requested, CustomCharactersEnabled())
	if p.Fallback == "" && source == "course" && cast.FallbackUsed {
		if want := s.selectedCharacter(ctx, owner, course, p.SpeakerKey); want != "" && want != p.CharacterID {
			p = safeDefaultPersona(p.SpeakerKey, source, cast.CastVersion, "persona_unavailable")
		}
	}
	return p, nil
}

// TutorRoster lists the tutors the learner can pick for a Course, or for a Lesson's pinned Cast.
func (s *Service) TutorRoster(ctx context.Context, owner, course, lessonID string) (TutorRoster, error) {
	cast, source, err := s.tutorCast(ctx, owner, course, lessonID)
	if err != nil {
		return TutorRoster{}, err
	}
	roster := TutorRoster{CastSource: source, CastVersion: cast.CastVersion, DefaultSpeaker: "guide", Tutors: []TutorChoice{}}
	for _, key := range tutorSpeakerKeys {
		speaker, present := cast.Speakers[key]
		if !present && key != "guide" {
			continue
		}
		p := ResolveTutorPersona(cast, source, key, CustomCharactersEnabled())
		role := speaker.Role
		if role == "" {
			role = map[bool]string{true: "guide", false: "supporter"}[key == "guide"]
		}
		roster.Tutors = append(roster.Tutors, TutorChoice{SpeakerKey: key, Role: role, Name: p.Name, CharacterVersion: p.CharacterVersion, Available: p.Fallback == ""})
	}
	return roster, nil
}

// TutorLabel names the Character a past turn was pinned to. It reads the immutable version row, so a later
// edit never renames history; a purged Character yields an empty label and the client shows a generic one.
func (s *Service) TutorLabel(ctx context.Context, owner, characterID string, version int) string {
	if name := builtinTutorName(characterID); name != "" {
		return name
	}
	if _, err := uuid.Parse(characterID); err != nil {
		return ""
	}
	var name string
	if err := s.pool.QueryRow(ctx, `SELECT v.spec->'info'->>'name' FROM characters c JOIN character_versions v ON v.character_id=c.id WHERE c.id=$1 AND c.learner_id=$2 AND v.version=$3`, characterID, owner, version).Scan(&name); err != nil {
		return ""
	}
	return plainPersonaText(name)
}
