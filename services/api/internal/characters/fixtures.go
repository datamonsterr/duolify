// Package characters implements the closed v1 CharacterSpec schema, trusted parametric SVG rig, and default cast fixtures.
package characters

import (
	"os"
	"strings"
)

// CustomCharactersEnabled controls whether custom characters are active.
func CustomCharactersEnabled() bool {
	val := os.Getenv("FEATURE_CUSTOM_CHARACTERS")
	if val == "" {
		return true // Default enabled for development and test
	}
	return strings.EqualFold(val, "true") || val == "1"
}

// DefaultGuideFixture returns the original provisional Guide mascot ("Provisional Glint").
func DefaultGuideFixture() CharacterSpec {
	return CharacterSpec{
		SchemaVersion: 1,
		RigVersion:    "original-v1",
		Info: CharacterInfo{
			Name:      "Provisional Glint",
			Bio:       "A curious little star who loves exploring new constellations.",
			Backstory: "Found orbiting a friendly comet, Glint joined Duolify to guide learners through uncharted topics.",
		},
		Personality: CharacterPersonality{
			Traits:        []string{"enthusiastic", "curious", "supportive"},
			SpeakingStyle: "warm, clear, and encouraging with gentle prompts",
			PersonaPrompt: "You are Provisional Glint, a small star guide. Encourage learners warmly.",
		},
		Appearance: CharacterAppearance{
			BodyShape: "star",
			Palette: Palette{
				Body:    "#FFD23F",
				Accent:  "#EE4266",
				Outline: "#1A1A24",
			},
			Eyes:        "round",
			Mouth:       "smile",
			Accessories: []string{"badge"},
		},
		Expressions: []string{"idle", "happy", "sad", "thinking", "celebrate"},
		VoiceKey:    "voice-provisional-guide",
	}
}

// DefaultSupporterFixture returns the original provisional Supporter mascot ("Provisional Moss").
func DefaultSupporterFixture() CharacterSpec {
	return CharacterSpec{
		SchemaVersion: 1,
		RigVersion:    "original-v1",
		Info: CharacterInfo{
			Name:      "Provisional Moss",
			Bio:       "A patient bean mascot who loves asking questions.",
			Backstory: "Grew up in a quiet botanical garden and decided to see what lies beyond Earth's sky.",
		},
		Personality: CharacterPersonality{
			Traits:        []string{"thoughtful", "gentle", "observant"},
			SpeakingStyle: "calm, inquisitive, asking thoughtful questions",
			PersonaPrompt: "You are Provisional Moss, a gentle bean mascot. Ask thoughtful questions.",
		},
		Appearance: CharacterAppearance{
			BodyShape: "bean",
			Palette: Palette{
				Body:    "#38B000",
				Accent:  "#70E000",
				Outline: "#004B23",
			},
			Eyes:        "oval",
			Mouth:       "open",
			Accessories: []string{"scarf"},
		},
		Expressions: []string{"idle", "happy", "sad", "thinking", "celebrate"},
		VoiceKey:    "voice-provisional-supporter",
	}
}

// DefaultCompanionFixture returns the original provisional Companion mascot ("Provisional Echo").
func DefaultCompanionFixture() CharacterSpec {
	return CharacterSpec{
		SchemaVersion: 1,
		RigVersion:    "original-v1",
		Info: CharacterInfo{
			Name:      "Provisional Echo",
			Bio:       "A round blue companion who takes notes on everything.",
			Backstory: "Echo arrived with a tiny notebook and an appetite for astronomy facts.",
		},
		Personality: CharacterPersonality{
			Traits:        []string{"diligent", "quiet", "precise"},
			SpeakingStyle: "concise, focused, referencing facts and notes",
			PersonaPrompt: "You are Provisional Echo, a meticulous round companion. Focus on facts.",
		},
		Appearance: CharacterAppearance{
			BodyShape: "round",
			Palette: Palette{
				Body:    "#4361EE",
				Accent:  "#4CC9F0",
				Outline: "#14213D",
			},
			Eyes:        "sleepy",
			Mouth:       "line",
			Accessories: []string{"glasses"},
		},
		Expressions: []string{"idle", "happy", "sad", "thinking", "celebrate"},
		VoiceKey:    "voice-provisional-companion",
	}
}

// GetDefaultCast returns the three original provisional built-in fixtures.
func GetDefaultCast() []CharacterSpec {
	return []CharacterSpec{
		DefaultGuideFixture(),
		DefaultSupporterFixture(),
		DefaultCompanionFixture(),
	}
}

// BundledFallbackSpec returns a safe static mascot spec used when custom specs are missing or invalid.
func BundledFallbackSpec() CharacterSpec {
	return CharacterSpec{
		SchemaVersion: 1,
		RigVersion:    "original-v1",
		Info: CharacterInfo{
			Name:      "Safe Mascot",
			Bio:       "A safe bundled fallback mascot.",
			Backstory: "Always present to ensure lessons play smoothly.",
		},
		Personality: CharacterPersonality{
			Traits:        []string{"friendly"},
			SpeakingStyle: "friendly and clear",
			PersonaPrompt: "You are a friendly fallback mascot.",
		},
		Appearance: CharacterAppearance{
			BodyShape: "round",
			Palette: Palette{
				Body:    "#90E0EF",
				Accent:  "#0077B6",
				Outline: "#03045E",
			},
			Eyes:        "round",
			Mouth:       "smile",
			Accessories: []string{},
		},
		Expressions: []string{"idle", "happy", "sad", "thinking", "celebrate"},
		VoiceKey:    "voice-provisional-guide",
	}
}
