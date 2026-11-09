import type { CharacterSpec, LessonManifest } from "./types";

export const defaultGuideFixture: CharacterSpec = {
  schema_version: 1,
  rig_version: "original-v1",
  info: {
    name: "Provisional Glint",
    bio: "A curious little star who loves exploring new constellations.",
    backstory:
      "Found orbiting a friendly comet, Glint joined Duolify to guide learners through uncharted topics.",
  },
  personality: {
    traits: ["enthusiastic", "curious", "supportive"],
    speaking_style: "warm, clear, and encouraging with gentle prompts",
    persona_prompt:
      "You are Provisional Glint, a small star guide. Encourage learners warmly.",
  },
  appearance: {
    body_shape: "star",
    palette: {
      body: "#FFD23F",
      accent: "#EE4266",
      outline: "#1A1A24",
    },
    eyes: "round",
    mouth: "smile",
    accessories: ["badge"],
  },
  expressions: ["idle", "happy", "sad", "thinking", "celebrate"],
  voice_key: "voice-provisional-guide",
};

export const defaultSupporterFixture: CharacterSpec = {
  schema_version: 1,
  rig_version: "original-v1",
  info: {
    name: "Provisional Moss",
    bio: "A patient bean mascot who loves asking questions.",
    backstory:
      "Grew up in a quiet botanical garden and decided to see what lies beyond Earth's sky.",
  },
  personality: {
    traits: ["thoughtful", "gentle", "observant"],
    speaking_style: "calm, inquisitive, asking thoughtful questions",
    persona_prompt:
      "You are Provisional Moss, a gentle bean mascot. Ask thoughtful questions.",
  },
  appearance: {
    body_shape: "bean",
    palette: {
      body: "#38B000",
      accent: "#70E000",
      outline: "#004B23",
    },
    eyes: "oval",
    mouth: "open",
    accessories: ["scarf"],
  },
  expressions: ["idle", "happy", "sad", "thinking", "celebrate"],
  voice_key: "voice-provisional-supporter",
};

export const defaultCompanionFixture: CharacterSpec = {
  schema_version: 1,
  rig_version: "original-v1",
  info: {
    name: "Provisional Echo",
    bio: "A round blue companion who takes notes on everything.",
    backstory:
      "Echo arrived with a tiny notebook and an appetite for astronomy facts.",
  },
  personality: {
    traits: ["diligent", "quiet", "precise"],
    speaking_style: "concise, focused, referencing facts and notes",
    persona_prompt:
      "You are Provisional Echo, a meticulous round companion. Focus on facts.",
  },
  appearance: {
    body_shape: "round",
    palette: {
      body: "#4361EE",
      accent: "#4CC9F0",
      outline: "#14213D",
    },
    eyes: "sleepy",
    mouth: "line",
    accessories: ["glasses"],
  },
  expressions: ["idle", "happy", "sad", "thinking", "celebrate"],
  voice_key: "voice-provisional-companion",
};

export function getDefaultCastFixtures(): CharacterSpec[] {
  return [
    defaultGuideFixture,
    defaultSupporterFixture,
    defaultCompanionFixture,
  ];
}

export function getBundledFallbackSpec(): CharacterSpec {
  return {
    schema_version: 1,
    rig_version: "original-v1",
    info: {
      name: "Safe Mascot",
      bio: "A safe bundled fallback mascot.",
      backstory: "Always present to ensure lessons play smoothly.",
    },
    personality: {
      traits: ["friendly"],
      speaking_style: "friendly and clear",
      persona_prompt: "You are a friendly fallback mascot.",
    },
    appearance: {
      body_shape: "round",
      palette: {
        body: "#90E0EF",
        accent: "#0077B6",
        outline: "#03045E",
      },
      eyes: "round",
      mouth: "smile",
      accessories: [],
    },
    expressions: ["idle", "happy", "sad", "thinking", "celebrate"],
    voice_key: "voice-provisional-guide",
  };
}

export function createDefaultTracerManifest(): LessonManifest {
  return {
    lesson_id: "tracer-default-cast",
    title: "Why the sky changes",
    cast: {
      cast_source: "default",
      schema_version: 1,
      rig_version: "original-v1",
      voice_config_version: "v1",
      speakers: {
        guide: {
          speaker_key: "guide",
          role: "guide",
          character_id: "char-provisional-glint",
          character_version: 1,
          spec: defaultGuideFixture,
          is_provisional: true,
        },
        supporter: {
          speaker_key: "supporter",
          role: "supporter",
          character_id: "char-provisional-moss",
          character_version: 1,
          spec: defaultSupporterFixture,
          is_provisional: true,
        },
      },
      fallback_used: false,
    },
    slides: [
      {
        slide_id: "slide-1",
        duration_ms: 3000,
        segments: [
          {
            speaker_key: "guide",
            speaker_label: defaultGuideFixture.info.name,
            text: "Look up! The night sky is full of patterns waiting to be explored.",
            expression: "happy",
            start_ms: 0,
            duration_ms: 1500,
          },
          {
            speaker_key: "supporter",
            speaker_label: defaultSupporterFixture.info.name,
            text: "Do you think the stars are moving, or is our Earth turning underneath?",
            expression: "thinking",
            start_ms: 1500,
            duration_ms: 1500,
          },
        ],
      },
      {
        slide_id: "slide-2",
        duration_ms: 3000,
        segments: [
          {
            speaker_key: "guide",
            speaker_label: defaultGuideFixture.info.name,
            text: "Earth turns once every day, which makes the stars seem to sweep across the sky.",
            expression: "celebrate",
            start_ms: 0,
            duration_ms: 3000,
          },
        ],
      },
    ],
  };
}
