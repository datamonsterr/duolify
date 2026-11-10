export type Expression = "idle" | "happy" | "sad" | "thinking" | "celebrate";

export type BodyShape = "round" | "bean" | "star";
export type EyeStyle = "round" | "oval" | "sleepy";
export type MouthStyle = "smile" | "line" | "open";
export type Accessory = "glasses" | "scarf" | "hat" | "badge";

export interface Palette {
  body: string;
  accent: string;
  outline: string;
}

export interface CharacterAppearance {
  body_shape: BodyShape;
  palette: Palette;
  eyes: EyeStyle;
  mouth: MouthStyle;
  accessories: Accessory[];
}

export interface CharacterInfo {
  name: string;
  bio: string;
  backstory: string;
}

export interface CharacterPersonality {
  traits: string[];
  speaking_style: string;
  persona_prompt: string;
}

export interface CharacterSpec {
  schema_version: 1;
  rig_version: "original-v1";
  info: CharacterInfo;
  personality: CharacterPersonality;
  appearance: CharacterAppearance;
  expressions: Expression[];
  voice_key: string;
}

export interface PinnedSpeaker {
  speaker_key: string;
  role: "guide" | "supporter";
  character_id: string;
  character_version: number;
  spec: CharacterSpec;
  is_provisional?: boolean;
}

export interface LessonCastSnapshot {
  cast_source: "default" | "custom" | "mixed" | "published";
  schema_version: number;
  rig_version: string;
  voice_config_version: string;
  speakers: Record<string, PinnedSpeaker>;
  fallback_used?: boolean;
}

export interface SlideSegment {
  speaker_key: string;
  speaker_label: string;
  text: string;
  expression: Expression;
  start_ms: number;
  duration_ms: number;
}

export interface SlideManifest {
  slide_id: string;
  duration_ms: number;
  segments: SlideSegment[];
  html?: string;
}

export interface LessonManifest {
  lesson_id: string;
  title: string;
  cast: LessonCastSnapshot;
  slides: SlideManifest[];
}
