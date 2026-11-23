import { speciesAttr, speciesAttrs } from "./catalog";
import { validateSpecV2 } from "./schema-v2";
import type { BodyKind, CharacterSpecV2, Styled } from "./types-v2";
import { EXPRESSIONS_V2 } from "./types-v2";

const none = (): Styled => ({ style: "none", color: "#1A1A24" });
const base = (body: CharacterSpecV2["body"]): CharacterSpecV2 => ({
  schema_version: 2,
  rig_version: "original-v2",
  catalog_version: 1,
  info: { name: "New character", bio: "", backstory: "" },
  identity: { pronouns: "", label: "" },
  body,
  clothes: {
    top: { style: "tee", color: "#4DA6F0" },
    bottom: { style: "pants", color: "#2E4057" },
    shoes: { style: "sneakers", color: "#F5F1E8" },
    outerwear: none(),
    pattern: "solid",
    accessories: [],
  },
  personality: {
    traits: ["curious"],
    speaking_style: "Gentle and clear",
    persona_prompt: "Explain simply and encourage the Learner.",
  },
  expressions: [...EXPRESSIONS_V2],
  voice_key: "voice-provisional-guide",
});
const common = {
  age_band: "adult",
  build: "average",
  height: "medium",
  glasses: none(),
};

/** Reset every species-controlled attribute to the species default (first allowed value). */
export function withSpeciesDefaults(spec: CharacterSpecV2): CharacterSpecV2 {
  const body = { ...spec.body } as unknown as Record<string, unknown>;
  const species = body.species as string | undefined;
  if (!species) return spec;
  for (const attr of speciesAttrs(spec.body.kind, species)) {
    body[attr] = speciesAttr(spec.body.kind, species, attr)[0];
  }
  return { ...spec, body: body as unknown as CharacterSpecV2["body"] };
}

/** A valid starting document for each kind; all colours chosen by the Learner later. */
export function defaultDraft(kind: BodyKind): CharacterSpecV2 {
  const spec = rawDefaultDraft(kind);
  return kind === "animal" || kind === "fantasy"
    ? withSpeciesDefaults(spec)
    : spec;
}

/** Switch species inside a kind, resetting ears, tail, markings, wings and horns to its defaults. */
export function changeSpecies(
  spec: CharacterSpecV2,
  species: string,
): CharacterSpecV2 {
  if (!("species" in spec.body) || spec.body.species === species) return spec;
  return withSpeciesDefaults({
    ...spec,
    body: { ...spec.body, species } as CharacterSpecV2["body"],
  });
}

function rawDefaultDraft(kind: BodyKind): CharacterSpecV2 {
  switch (kind) {
    case "human":
      return base({
        kind,
        ...common,
        skin_tone: "t06",
        face_shape: "oval",
        hair: {
          texture: "wavy",
          length: "medium",
          style: "loose",
          color: "#3A2418",
        },
        head_covering: none(),
        eyes: { shape: "round", color: "#4B3621" },
        eyebrows: { style: "arched", color: "#3A2418" },
        nose: "round",
        mouth: "smile",
        facial_hair: none(),
        skin_features: [],
      });
    case "animal":
      return base({
        kind,
        ...common,
        species: "fox",
        primary_color: "#E07A3F",
        secondary_color: "#FFF3E0",
        markings: "none",
        ears: "pointy",
        tail: "fluffy",
        eyes: { shape: "round", color: "#1A1A24" },
        mouth: "smile",
      });
    case "robot":
      return base({
        kind,
        ...common,
        base: "boxy",
        primary_color: "#9DB4C0",
        secondary_color: "#E4572E",
        markings: "panels",
        antenna: "single",
        locomotion: "legs",
        eyes: { shape: "screen", color: "#7CFFB2" },
        mouth: "line",
      });
    default:
      return base({
        kind: "fantasy",
        ...common,
        species: "dragon",
        primary_color: "#6BBF59",
        secondary_color: "#F2C94C",
        wings: "none",
        horns: "small",
        tail: "long",
        eyes: { shape: "wide", color: "#1A1A24" },
        mouth: "open",
      });
  }
}

/** Switch kind keeping name, identity, personality, voice and clothes. */
export function changeKind(
  spec: CharacterSpecV2,
  kind: BodyKind,
): CharacterSpecV2 {
  if (spec.body.kind === kind) return spec;
  const next = defaultDraft(kind);
  return {
    ...next,
    info: spec.info,
    identity: spec.identity,
    clothes: spec.clothes,
    personality: spec.personality,
    voice_key: spec.voice_key,
    body: {
      ...next.body,
      age_band: spec.body.age_band,
      build: spec.body.build,
      height: spec.body.height,
    } as CharacterSpecV2["body"],
  };
}

/** Immutable set of a dotted path ("body.hair.style", "clothes.accessories"). */
export function setAt<T>(obj: T, path: string[], value: unknown): T {
  if (path.length === 0) return value as T;
  const [head, ...rest] = path;
  const copy = Array.isArray(obj) ? [...obj] : { ...(obj as object) };
  (copy as Record<string, unknown>)[head!] = setAt(
    (obj as Record<string, unknown>)[head!],
    rest,
    value,
  );
  return copy as T;
}

export type DraftResult =
  | { ok: true; spec: CharacterSpecV2 }
  | { ok: false; code: string; path: string };

/**
 * Draft-patch contract (shared with the guided chat in conversational-flows-and-mascot):
 * a patch is an RFC 7386 JSON merge patch over the draft document (objects merge, arrays and
 * scalars replace, null is rejected). The result is validated by the shared schema on every
 * step, so a chat turn can never leave the draft invalid. Changing `body.kind` requires the
 * whole `body` object in the patch.
 */
export function applyDraftPatch(
  spec: CharacterSpecV2,
  patch: unknown,
): DraftResult {
  const merge = (target: unknown, p: unknown): unknown => {
    if (p === null) throw new Error("null_patch_value");
    if (!p || typeof p !== "object" || Array.isArray(p)) return p;
    const out: Record<string, unknown> =
      target && typeof target === "object" && !Array.isArray(target)
        ? { ...(target as object) }
        : {};
    for (const [k, v] of Object.entries(p)) out[k] = merge(out[k], v);
    return out;
  };
  let next: unknown;
  try {
    next = merge(spec, patch);
  } catch {
    return { ok: false, code: "invalid_type", path: "" };
  }
  const result = validateSpecV2(next);
  return result.valid
    ? { ok: true, spec: result.spec }
    : { ok: false, code: result.code, path: result.path };
}
