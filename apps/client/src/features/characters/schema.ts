import type { Palette } from "./types";

const HEX_COLOR_REGEX = /^#[0-9A-Fa-f]{6}$/;
const VALID_BODY_SHAPES = new Set(["round", "bean", "star"]);
const VALID_EYES = new Set(["round", "oval", "sleepy"]);
const VALID_MOUTHS = new Set(["smile", "line", "open"]);
const VALID_ACCESSORIES = new Set(["glasses", "scarf", "hat", "badge"]);
const VALID_EXPRESSIONS = new Set([
  "idle",
  "happy",
  "sad",
  "thinking",
  "celebrate",
]);
const VALID_VOICE_KEYS = new Set([
  "voice-provisional-guide",
  "voice-provisional-supporter",
  "voice-provisional-companion",
]);

const ALLOWED_SPEC_KEYS = new Set([
  "schema_version",
  "rig_version",
  "info",
  "personality",
  "appearance",
  "expressions",
  "voice_key",
]);

const ALLOWED_INFO_KEYS = new Set(["name", "bio", "backstory"]);
const ALLOWED_PERSONALITY_KEYS = new Set([
  "traits",
  "speaking_style",
  "persona_prompt",
]);
const ALLOWED_APPEARANCE_KEYS = new Set([
  "body_shape",
  "palette",
  "eyes",
  "mouth",
  "accessories",
]);
const ALLOWED_PALETTE_KEYS = new Set(["body", "accent", "outline"]);

export interface ValidationResult {
  valid: boolean;
  errors?: string[];
}

export function validateCharacterSpec(input: unknown): ValidationResult {
  const errors: string[] = [];

  if (!input || typeof input !== "object" || Array.isArray(input)) {
    return { valid: false, errors: ["spec_not_object"] };
  }

  const obj = input as Record<string, unknown>;

  // Closed schema check
  for (const key of Object.keys(obj)) {
    if (!ALLOWED_SPEC_KEYS.has(key)) {
      errors.push("unknown_field");
      errors.push(`unknown_field:${key}`);
    }
  }

  if (obj.schema_version !== 1) {
    errors.push("unsupported_schema_version");
  }
  if (obj.rig_version !== "original-v1") {
    errors.push("unsupported_rig_version");
  }

  // Info
  if (!obj.info || typeof obj.info !== "object" || Array.isArray(obj.info)) {
    errors.push("invalid_info");
  } else {
    const info = obj.info as Record<string, unknown>;
    for (const k of Object.keys(info)) {
      if (!ALLOWED_INFO_KEYS.has(k)) errors.push(`unknown_info_field:${k}`);
    }
    const name = String(info.name ?? "");
    if (name.length < 1 || name.length > 40) errors.push("invalid_name_length");
    const bio = String(info.bio ?? "");
    if (bio.length > 280) errors.push("invalid_bio_length");
    const backstory = String(info.backstory ?? "");
    if (backstory.length > 800) errors.push("invalid_backstory_length");

    for (const text of [name, bio, backstory]) {
      if (containsUnsafeContent(text)) errors.push("markup_rejected");
    }
  }

  // Personality
  if (
    !obj.personality ||
    typeof obj.personality !== "object" ||
    Array.isArray(obj.personality)
  ) {
    errors.push("invalid_personality");
  } else {
    const p = obj.personality as Record<string, unknown>;
    for (const k of Object.keys(p)) {
      if (!ALLOWED_PERSONALITY_KEYS.has(k))
        errors.push(`unknown_personality_field:${k}`);
    }
    if (!Array.isArray(p.traits)) {
      errors.push("invalid_traits");
    } else {
      if (p.traits.length < 1 || p.traits.length > 5) {
        errors.push("invalid_traits_count");
      }
      const seen = new Set<string>();
      for (const t of p.traits) {
        const str = String(t);
        if (str.length < 1 || str.length > 40)
          errors.push("invalid_trait_length");
        if (seen.has(str)) errors.push("duplicate_trait");
        seen.add(str);
        if (containsUnsafeContent(str)) errors.push("markup_rejected");
      }
    }
    const style = String(p.speaking_style ?? "");
    if (style.length < 1 || style.length > 200)
      errors.push("invalid_speaking_style_length");
    const prompt = String(p.persona_prompt ?? "");
    if (prompt.length < 1 || prompt.length > 500)
      errors.push("invalid_persona_prompt_length");

    for (const text of [style, prompt]) {
      if (containsUnsafeContent(text)) errors.push("markup_rejected");
    }
  }

  // Appearance
  if (
    !obj.appearance ||
    typeof obj.appearance !== "object" ||
    Array.isArray(obj.appearance)
  ) {
    errors.push("invalid_appearance");
  } else {
    const app = obj.appearance as Record<string, unknown>;
    for (const k of Object.keys(app)) {
      if (!ALLOWED_APPEARANCE_KEYS.has(k))
        errors.push(`unknown_appearance_field:${k}`);
    }

    if (!VALID_BODY_SHAPES.has(String(app.body_shape))) {
      errors.push("invalid_body_shape");
    }

    if (
      !app.palette ||
      typeof app.palette !== "object" ||
      Array.isArray(app.palette)
    ) {
      errors.push("invalid_palette");
    } else {
      const pal = app.palette as Record<string, unknown>;
      for (const k of Object.keys(pal)) {
        if (!ALLOWED_PALETTE_KEYS.has(k))
          errors.push(`unknown_palette_field:${k}`);
      }
      const body = String(pal.body ?? "");
      const accent = String(pal.accent ?? "");
      const outline = String(pal.outline ?? "");

      if (
        !HEX_COLOR_REGEX.test(body) ||
        !HEX_COLOR_REGEX.test(accent) ||
        !HEX_COLOR_REGEX.test(outline)
      ) {
        errors.push("invalid_palette_hex");
      } else {
        if (!hasValidContrast({ body, accent, outline })) {
          errors.push("invalid_contrast");
        }
      }
    }

    if (!VALID_EYES.has(String(app.eyes))) {
      errors.push("invalid_eyes");
    }
    if (!VALID_MOUTHS.has(String(app.mouth))) {
      errors.push("invalid_mouth");
    }

    if (!Array.isArray(app.accessories)) {
      errors.push("invalid_accessories");
    } else {
      if (app.accessories.length > 2) {
        errors.push("too_many_accessories");
      }
      const seen = new Set<string>();
      for (const a of app.accessories) {
        const str = String(a);
        if (!VALID_ACCESSORIES.has(str))
          errors.push(`invalid_accessory:${str}`);
        if (seen.has(str)) errors.push("duplicate_accessory");
        seen.add(str);
      }
    }
  }

  // Expressions
  if (!Array.isArray(obj.expressions)) {
    errors.push("invalid_expressions");
  } else {
    if (obj.expressions.length !== 5) {
      errors.push("invalid_expressions_count");
    }
    const seen = new Set<string>();
    for (const exp of obj.expressions) {
      const str = String(exp);
      if (!VALID_EXPRESSIONS.has(str)) errors.push(`invalid_expression:${str}`);
      seen.add(str);
    }
    if (seen.size !== 5) errors.push("missing_required_expressions");
  }

  // Voice key
  const voiceKey = String(obj.voice_key ?? "");
  if (!VALID_VOICE_KEYS.has(voiceKey)) {
    errors.push("invalid_voice_key");
  }

  return {
    valid: errors.length === 0,
    errors: errors.length > 0 ? errors : undefined,
  };
}

function containsUnsafeContent(text: string): boolean {
  const lower = text.toLowerCase();
  if (lower.includes("<") || lower.includes(">")) return true;
  if (lower.includes("javascript:") || lower.includes("data:")) return true;
  if (lower.includes("http://") || lower.includes("https://")) return true;
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x08\x0B\x0C\x0E-\x1F]/.test(text)) return true;
  return false;
}

function hasValidContrast(palette: Palette): boolean {
  if (
    palette.body.toLowerCase() === palette.outline.toLowerCase() ||
    palette.body.toLowerCase() === palette.accent.toLowerCase() ||
    palette.outline.toLowerCase() === palette.accent.toLowerCase()
  ) {
    return false;
  }

  const lumBody = relativeLuminance(palette.body);
  const lumOutline = relativeLuminance(palette.outline);

  const contrast = contrastRatio(lumBody, lumOutline);
  return contrast >= 2.5;
}

function relativeLuminance(hex: string): number {
  const clean = hex.replace("#", "");
  const r = parseInt(clean.slice(0, 2), 16) / 255;
  const g = parseInt(clean.slice(2, 4), 16) / 255;
  const b = parseInt(clean.slice(4, 6), 16) / 255;
  const toLinear = (c: number) =>
    c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  return 0.2126 * toLinear(r) + 0.7152 * toLinear(g) + 0.0722 * toLinear(b);
}

function contrastRatio(l1: number, l2: number): number {
  const lighter = Math.max(l1, l2);
  const darker = Math.min(l1, l2);
  return (lighter + 0.05) / (darker + 0.05);
}
