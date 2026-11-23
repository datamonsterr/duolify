import schemaV2 from "../../../../../services/api/internal/characters/schema/v2.json";
import type { CharacterSpecV2 } from "./types-v2";

export const CATALOG_VERSION = 1;
const MAX_SPEC_BYTES = 8192;
const MAX_TEXT = 2400;

export interface SpecIssue {
  code: string;
  path: string;
}
export type ValidationV2 =
  | { valid: true; spec: CharacterSpecV2 }
  | { valid: false; code: string; path: string };

type Schema = {
  type?: string;
  const?: unknown;
  enum?: string[];
  required?: string[];
  properties?: Record<string, Schema>;
  additionalProperties?: boolean;
  minLength?: number;
  maxLength?: number;
  pattern?: string;
  minItems?: number;
  maxItems?: number;
  uniqueItems?: boolean;
  uniqueBy?: string;
  items?: Schema;
  discriminator?: string;
  oneOf?: Schema[];
};

const HEX = /^#[0-9A-Fa-f]{6}$/;
const sortedKeys = (o: object) => Object.keys(o).sort();
const join = (path: string, key: string) => (path ? `${path}.${key}` : key);
const runes = (s: string) => Array.from(s).length;
const isObject = (v: unknown): v is Record<string, unknown> =>
  !!v && typeof v === "object" && !Array.isArray(v);
const issue = (code: string, path: string): SpecIssue => ({ code, path });

// Mirrors shapeV2 in services/api/internal/characters/spec_v2.go; traversal
// order is part of the contract and pinned by v2.fixtures.json.
function shape(value: unknown, schema: Schema, path: string): SpecIssue | null {
  if ("const" in schema && String(schema.const) !== String(value))
    return issue("unknown_catalog_id", path);
  if (schema.enum) {
    if (typeof value !== "string") return issue("invalid_type", path);
    if (!schema.enum.includes(value)) return issue("unknown_catalog_id", path);
  }
  switch (schema.type) {
    case "object": {
      if (!isObject(value)) return issue("invalid_type", path);
      const props = schema.properties ?? {};
      if (schema.discriminator) {
        const disc = schema.discriminator;
        if (!(disc in value)) return issue("missing_field", join(path, disc));
        const branch = (schema.oneOf ?? []).find(
          (b) => String(b.properties?.[disc]?.const) === String(value[disc]),
        );
        if (!branch) return issue("unknown_catalog_id", join(path, disc));
        return shape(value, branch, path);
      }
      for (const name of [...(schema.required ?? [])].sort())
        if (!(name in value)) return issue("missing_field", join(path, name));
      if (schema.additionalProperties === false)
        for (const key of sortedKeys(value))
          if (!(key in props)) return issue("unknown_field", join(path, key));
      for (const key of sortedKeys(props))
        if (key in value) {
          const found = shape(value[key], props[key]!, join(path, key));
          if (found) return found;
        }
      return null;
    }
    case "string": {
      if (typeof value !== "string") return issue("invalid_type", path);
      const n = runes(value);
      if (schema.minLength !== undefined && n < schema.minLength)
        return issue("invalid_length", path);
      if (schema.maxLength !== undefined && n > schema.maxLength)
        return issue("invalid_length", path);
      if (schema.pattern && !HEX.test(value))
        return issue("invalid_format", path);
      return null;
    }
    case "integer":
      return typeof value === "number" && Number.isInteger(value)
        ? null
        : issue("invalid_type", path);
    case "array": {
      if (!Array.isArray(value)) return issue("invalid_type", path);
      if (schema.minItems !== undefined && value.length < schema.minItems)
        return issue("invalid_length", path);
      if (schema.maxItems !== undefined && value.length > schema.maxItems)
        return issue("invalid_length", path);
      for (const [i, item] of value.entries()) {
        const found = shape(item, schema.items!, `${path}[${i}]`);
        if (found) return found;
      }
      if (schema.uniqueItems) {
        const seen = new Set<string>();
        for (const [i, item] of value.entries()) {
          const key = String(
            schema.uniqueBy
              ? (item as Record<string, unknown>)[schema.uniqueBy]
              : item,
          );
          if (seen.has(key)) return issue("duplicate_item", `${path}[${i}]`);
          seen.add(key);
        }
      }
      return null;
    }
    default:
      return issue("invalid_type", path);
  }
}

function check(root: Record<string, unknown>): SpecIssue | null {
  if (new TextEncoder().encode(JSON.stringify(root)).length > MAX_SPEC_BYTES)
    return issue("spec_too_large", "");
  if (root.schema_version !== 2)
    return issue("unsupported_schema_version", "schema_version");
  if (root.rig_version !== "original-v2")
    return issue("unsupported_rig_version", "rig_version");
  if (root.catalog_version !== CATALOG_VERSION)
    return issue("unsupported_catalog_version", "catalog_version");
  return shape(root, schemaV2 as unknown as Schema, "");
}

export function containsUnsafeText(text: string): boolean {
  const lower = text.toLowerCase();
  return (
    lower.includes("<") ||
    lower.includes(">") ||
    lower.includes("javascript:") ||
    lower.includes("data:") ||
    lower.includes("http://") ||
    lower.includes("https://") ||
    // eslint-disable-next-line no-control-regex
    /[\x00-\x08\x0B\x0C\x0E-\x1F]/.test(text)
  );
}

export function contrastBetween(a: string, b: string): number {
  const lum = (hex: string) => {
    const channel = (i: number) => {
      const c = parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16) / 255;
      return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * channel(0) + 0.7152 * channel(1) + 0.0722 * channel(2);
  };
  const [la, lb] = [lum(a), lum(b)];
  return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
}

function cross(spec: CharacterSpecV2): SpecIssue | null {
  const texts: [string, string][] = [
    ["info.name", spec.info.name],
    ["info.bio", spec.info.bio],
    ["info.backstory", spec.info.backstory],
    ["identity.pronouns", spec.identity.pronouns],
    ["identity.label", spec.identity.label],
    ["personality.speaking_style", spec.personality.speaking_style],
    ["personality.persona_prompt", spec.personality.persona_prompt],
    ...spec.personality.traits.map(
      (t, i) => [`personality.traits[${i}]`, t] as [string, string],
    ),
  ];
  let total = 0;
  for (const [path, text] of texts) {
    if (containsUnsafeText(text)) return issue("unsafe_text", path);
    total += runes(text);
  }
  if (total > MAX_TEXT) return issue("text_too_long", "");
  const b = spec.body;
  // Robot eyes glow on the dark visor; other non-human eyes sit on the body colour.
  if (
    (b.kind === "robot" && contrastBetween("#16242F", b.eyes.color) < 2.5) ||
    (b.kind !== "human" &&
      b.kind !== "robot" &&
      contrastBetween(b.primary_color, b.eyes.color) < 2.5)
  )
    return issue("low_contrast", "body.eyes.color");
  if (b.kind === "human") {
    const c = spec.clothes;
    const minor = b.age_band === "child" || b.age_band === "teen";
    if (
      minor &&
      (c.top.style === "none" ||
        (c.bottom.style === "none" &&
          c.top.style !== "dress" &&
          c.top.style !== "overalls"))
    )
      return issue("clothing_required_for_minor", "clothes");
    if (b.facial_hair.style !== "none" && b.age_band === "child")
      return issue("facial_hair_age", "body.facial_hair");
    if (b.hair.length === "bald" && b.hair.style !== "loose")
      return issue("hair_style_mismatch", "body.hair.style");
    if (
      b.hair.length === "buzz" &&
      b.hair.style !== "loose" &&
      b.hair.style !== "mohawk"
    )
      return issue("hair_style_mismatch", "body.hair.style");
  }
  const kinds = new Set(spec.clothes.accessories.map((a) => a.kind));
  const human = spec.body.kind === "human" ? spec.body : null;
  if (
    (kinds.has("tie") && kinds.has("bow_tie")) ||
    (kinds.has("hat") && human && human.head_covering.style !== "none") ||
    (kinds.has("headphones") &&
      human &&
      (human.head_covering.style === "helmet" ||
        human.head_covering.style === "hood"))
  )
    return issue("incompatible_accessories", "clothes.accessories");
  return null;
}

/** Validate a parsed JSON value against schema v2. */
export function validateSpecV2(input: unknown): ValidationV2 {
  if (!isObject(input))
    return { valid: false, code: "spec_not_object", path: "" };
  const structural = check(input);
  if (structural)
    return { valid: false, code: structural.code, path: structural.path };
  const spec = input as unknown as CharacterSpecV2;
  const crossIssue = cross(spec);
  if (crossIssue)
    return { valid: false, code: crossIssue.code, path: crossIssue.path };
  return { valid: true, spec };
}
