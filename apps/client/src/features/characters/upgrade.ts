import { validateCharacterSpec } from "./schema";
import { validateSpecV2, CATALOG_VERSION } from "./schema-v2";
import type { CharacterSpec } from "./types";
import { EXPRESSIONS_V2, type CharacterSpecV2, type Styled } from "./types-v2";

const NONE_COLOR = "#1A1A24";
const none = (): Styled => ({ style: "none", color: NONE_COLOR });
const SPECIES: Record<string, string> = {
  round: "slime",
  bean: "ghost",
  star: "fairy",
};
const EYES: Record<string, string> = {
  round: "round",
  oval: "wide",
  sleepy: "sleepy",
};

/** Pure v1 to v2 upgrade; mirrors UpgradeV1 in upgrade.go (golden pairs). */
export function upgradeV1(old: CharacterSpec): CharacterSpecV2 {
  const p = old.appearance.palette;
  const species = SPECIES[old.appearance.body_shape]!;
  let glasses = none();
  const accessories: CharacterSpecV2["clothes"]["accessories"] = [];
  for (const a of old.appearance.accessories) {
    if (a === "glasses") glasses = { style: "round", color: p.outline };
    else accessories.push({ kind: a, color: p.accent });
  }
  return {
    schema_version: 2,
    rig_version: "original-v2",
    catalog_version: CATALOG_VERSION,
    info: { ...old.info },
    identity: { pronouns: "", label: "" },
    body: {
      kind: "fantasy",
      age_band: "adult",
      build: "average",
      height: "medium",
      glasses,
      species,
      primary_color: p.body,
      secondary_color: p.accent,
      wings: species === "fairy" ? "insect" : "none",
      horns: "none",
      tail: "none",
      eyes: { shape: EYES[old.appearance.eyes]!, color: p.outline },
      mouth: old.appearance.mouth,
    },
    clothes: {
      top: none(),
      bottom: none(),
      shoes: none(),
      outerwear: none(),
      pattern: "solid",
      accessories,
    },
    personality: { ...old.personality, traits: [...old.personality.traits] },
    expressions: [...EXPRESSIONS_V2],
    voice_key: old.voice_key,
  };
}

/** Neutral document rendered for unknown future versions or catalog ids. */
export function defaultSpecV2(): CharacterSpecV2 {
  return {
    schema_version: 2,
    rig_version: "original-v2",
    catalog_version: CATALOG_VERSION,
    info: { name: "Guide", bio: "", backstory: "" },
    identity: { pronouns: "", label: "" },
    body: {
      kind: "fantasy",
      age_band: "adult",
      build: "round",
      height: "medium",
      glasses: none(),
      species: "slime",
      primary_color: "#90E0EF",
      secondary_color: "#CAF0F8",
      wings: "none",
      horns: "none",
      tail: "none",
      eyes: { shape: "round", color: "#03045E" },
      mouth: "smile",
    },
    clothes: {
      top: none(),
      bottom: none(),
      shoes: none(),
      outerwear: none(),
      pattern: "solid",
      accessories: [],
    },
    personality: {
      traits: ["friendly"],
      speaking_style: "Clear and kind",
      persona_prompt: "Explain simply and encourage the Learner.",
    },
    expressions: [...EXPRESSIONS_V2],
    voice_key: "voice-provisional-guide",
  };
}

export interface Reading {
  spec: CharacterSpecV2;
  sourceVersion: number;
  upgraded: boolean;
  /** Future version or unknown catalog id: renders the default, refuses writes. */
  readOnly: boolean;
}
export type ReadResult =
  { ok: true; reading: Reading } | { ok: false; code: string };

/** Resolve a stored document of any supported version (ReadSpec in Go). */
export function readSpec(input: unknown): ReadResult {
  const rec =
    input && typeof input === "object" && !Array.isArray(input)
      ? (input as Record<string, unknown>)
      : {};
  const version = rec.schema_version;
  const catalog = rec.catalog_version;
  const degraded = (sourceVersion: number): ReadResult => ({
    ok: true,
    reading: {
      spec: defaultSpecV2(),
      sourceVersion,
      upgraded: false,
      readOnly: true,
    },
  });
  if (version === 1) {
    const check = validateCharacterSpec(input);
    if (!check.valid) return { ok: false, code: "invalid_spec" };
    return {
      ok: true,
      reading: {
        spec: upgradeV1(input as CharacterSpec),
        sourceVersion: 1,
        upgraded: true,
        readOnly: false,
      },
    };
  }
  if (typeof version === "number" && version === 2) {
    if (typeof catalog === "number" && catalog > CATALOG_VERSION)
      return degraded(2);
    const result = validateSpecV2(input);
    if (result.valid)
      return {
        ok: true,
        reading: {
          spec: result.spec,
          sourceVersion: 2,
          upgraded: false,
          readOnly: false,
        },
      };
    if (result.code === "unknown_catalog_id") return degraded(2);
    return { ok: false, code: result.code };
  }
  if (typeof version === "number" && version > 2) return degraded(version);
  return { ok: false, code: "unsupported_schema_version" };
}
