package characters

// PinnedSpeaker represents an immutable speaker pinned in a lesson manifest.
type PinnedSpeaker struct {
	SpeakerKey       string        `json:"speaker_key"`
	Role             string        `json:"role"`
	CharacterID      string        `json:"character_id"`
	CharacterVersion int           `json:"character_version"`
	Spec             CharacterSpec `json:"spec"`
	IsProvisional    bool          `json:"is_provisional,omitempty"`
}

// LessonCastSnapshot holds the pinned cast for a lesson.
type LessonCastSnapshot struct {
	CastVersion        int                      `json:"cast_version"`
	CastSource         string                   `json:"cast_source"`
	SchemaVersion      int                      `json:"schema_version"`
	RigVersion         string                   `json:"rig_version"`
	VoiceConfigVersion string                   `json:"voice_config_version"`
	Speakers           map[string]PinnedSpeaker `json:"speakers"`
	FallbackUsed       bool                     `json:"fallback_used,omitempty"`
}

// SlideSegment represents one spoken segment in a slide.
type SlideSegment struct {
	SpeakerKey   string `json:"speaker_key"`
	SpeakerLabel string `json:"speaker_label"`
	Text         string `json:"text"`
	Expression   string `json:"expression"`
	StartMs      int    `json:"start_ms"`
	DurationMs   int    `json:"duration_ms"`
}

// SlideManifest defines the timing, segments, and optional HTML of a slide.
type SlideManifest struct {
	SlideID    string         `json:"slide_id"`
	DurationMs int            `json:"duration_ms"`
	Segments   []SlideSegment `json:"segments"`
	HTML       string         `json:"html,omitempty"`
}

// LessonManifest represents the authorized lesson payload delivered to the client.
type LessonManifest struct {
	LessonID string             `json:"lesson_id"`
	Title    string             `json:"title"`
	Cast     LessonCastSnapshot `json:"cast"`
	Slides   []SlideManifest    `json:"slides"`
}

// ProjectDefaultCastManifest constructs a lesson manifest with the default cast and two stable speaker keys.
func ProjectDefaultCastManifest(_, lessonID string) (*LessonManifest, error) {
	guideFixture := DefaultGuideFixture()
	supporterFixture := DefaultSupporterFixture()

	guideSpeaker := PinnedSpeaker{
		SpeakerKey:       "guide",
		Role:             "guide",
		CharacterID:      "char-provisional-glint",
		CharacterVersion: 1,
		Spec:             guideFixture,
		IsProvisional:    true,
	}

	supporterSpeaker := PinnedSpeaker{
		SpeakerKey:       "supporter",
		Role:             "supporter",
		CharacterID:      "char-provisional-moss",
		CharacterVersion: 1,
		Spec:             supporterFixture,
		IsProvisional:    true,
	}

	speakers := map[string]PinnedSpeaker{
		"guide":     guideSpeaker,
		"supporter": supporterSpeaker,
	}

	cast := LessonCastSnapshot{
		CastSource:         "default",
		SchemaVersion:      1,
		RigVersion:         "original-v1",
		VoiceConfigVersion: "v1",
		Speakers:           speakers,
		FallbackUsed:       false,
	}

	slides := []SlideManifest{
		{
			SlideID:    "slide-1",
			DurationMs: 3000,
			Segments: []SlideSegment{
				{
					SpeakerKey:   "guide",
					SpeakerLabel: guideFixture.Info.Name,
					Text:         "Look up! The night sky is full of patterns waiting to be explored.",
					Expression:   "happy",
					StartMs:      0,
					DurationMs:   1500,
				},
				{
					SpeakerKey:   "supporter",
					SpeakerLabel: supporterFixture.Info.Name,
					Text:         "Do you think the stars are moving, or is our Earth turning underneath?",
					Expression:   "thinking",
					StartMs:      1500,
					DurationMs:   1500,
				},
			},
		},
		{
			SlideID:    "slide-2",
			DurationMs: 3000,
			Segments: []SlideSegment{
				{
					SpeakerKey:   "guide",
					SpeakerLabel: guideFixture.Info.Name,
					Text:         "Earth turns once every day, which makes the stars seem to sweep across the sky.",
					Expression:   "celebrate",
					StartMs:      0,
					DurationMs:   3000,
				},
			},
		},
	}

	return &LessonManifest{
		LessonID: lessonID,
		Title:    "Why the sky changes",
		Cast:     cast,
		Slides:   slides,
	}, nil
}
