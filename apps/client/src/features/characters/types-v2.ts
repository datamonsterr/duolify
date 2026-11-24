export type BodyKind = "human" | "animal" | "robot" | "fantasy";
export type ExpressionV2 =
  | "idle"
  | "happy"
  | "sad"
  | "thinking"
  | "celebrate"
  | "surprised"
  | "confused";
export type Pose =
  "idle" | "talk" | "point" | "think" | "celebrate" | "wave" | "shrug";
export const POSES: Pose[] = [
  "idle",
  "talk",
  "point",
  "think",
  "celebrate",
  "wave",
  "shrug",
];

export interface Styled {
  style: string;
  color: string;
}
export interface EyesV2 {
  shape: string;
  color: string;
}
export interface Hair {
  texture: string;
  length: string;
  style: string;
  color: string;
}
export interface AccessoryV2 {
  kind: string;
  color: string;
}

interface BodyBase {
  age_band: string;
  build: string;
  height: string;
  glasses: Styled;
  eyes: EyesV2;
  mouth: string;
}
export interface HumanBody extends BodyBase {
  kind: "human";
  skin_tone: string;
  face_shape: string;
  hair: Hair;
  head_covering: Styled;
  eyebrows: Styled;
  nose: string;
  facial_hair: Styled;
  skin_features: string[];
}
export interface AnimalBody extends BodyBase {
  kind: "animal";
  species: string;
  primary_color: string;
  secondary_color: string;
  markings: string;
  ears: string;
  tail: string;
}
export interface RobotBody extends BodyBase {
  kind: "robot";
  base: string;
  primary_color: string;
  secondary_color: string;
  markings: string;
  antenna: string;
  locomotion: string;
}
export interface FantasyBody extends BodyBase {
  kind: "fantasy";
  species: string;
  primary_color: string;
  secondary_color: string;
  wings: string;
  horns: string;
  tail: string;
}
export type BodyV2 = HumanBody | AnimalBody | RobotBody | FantasyBody;

export interface ClothesV2 {
  top: Styled;
  bottom: Styled;
  shoes: Styled;
  outerwear: Styled;
  pattern: string;
  accessories: AccessoryV2[];
}

export interface CharacterSpecV2 {
  schema_version: 2;
  rig_version: "original-v2";
  catalog_version: 1;
  info: { name: string; bio: string; backstory: string };
  identity: { pronouns: string; label: string };
  body: BodyV2;
  clothes: ClothesV2;
  personality: {
    traits: string[];
    speaking_style: string;
    persona_prompt: string;
  };
  expressions: ExpressionV2[];
  voice_key: string;
}

export const EXPRESSIONS_V2: ExpressionV2[] = [
  "idle",
  "happy",
  "sad",
  "thinking",
  "celebrate",
  "surprised",
  "confused",
];
