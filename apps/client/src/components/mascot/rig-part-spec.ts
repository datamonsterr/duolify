// RigPartSpec v1: the shared appearance contract of the built-in mascot and `custom-characters`.
// Names follow `custom-characters` Revision v2 (robot kind: base, plating, accent, eyes). Mapping:
//   palette.body / bodyEdge / trim  -> robot "plating" colour (+ derived edge and trim)
//   palette.accent                  -> robot "accent" colour
//   palette.visor / eye             -> robot "eyes" (visor screen, eye glow)
//   state                           -> {pose, expression} via characterStateMap (idle|talk|think|celebrate|wave|shrug,
//                                      idle|happy|thinking|celebrate|confused); `listening` and `sleepy` fall back to idle.
// The allowlist lives in rig-part-spec.v1.json so the Go API validates against the same data.
import spec from "./rig-part-spec.v1.json";

export type MascotState = (typeof spec.states)[number];
export type MascotAccessory = (typeof spec.accessories)[number];
export type MascotSwatch = keyof typeof spec.swatches;
export const MASCOT_STATES = spec.states as readonly MascotState[];
export const MASCOT_ACCESSORIES =
  spec.accessories as readonly MascotAccessory[];
export const MASCOT_SWATCHES = Object.keys(spec.swatches) as MascotSwatch[];
export const MASCOT_SWATCH_COLORS = spec.swatches;
export const MAX_ACCESSORIES = spec.maxAccessories;
export const CHARACTER_STATE_MAP = spec.characterStateMap;

export type RigPalette = {
  body: string;
  bodyEdge: string;
  trim: string;
  visor: string;
  eye: string;
  accent: string;
};
export type RigPartSpec = {
  rig: string;
  palette?: RigPalette;
  accessories: MascotAccessory[];
};

const hex = (v: unknown): v is string =>
  typeof v === "string" && /^#[0-9A-Fa-f]{6}$/.test(v);
const same = (a: string, b: string) => a.toUpperCase() === b.toUpperCase();

/** Palette for a swatch id; the single source for the picker and for validation. */
export function paletteFor(swatch: MascotSwatch): RigPalette {
  const s = spec.swatches[swatch];
  return { ...s, visor: spec.visor[0], accent: spec.accent[0] };
}
/** The swatch whose palette equals `palette` (case-insensitive), else undefined. */
export function swatchOf(palette: unknown): MascotSwatch | undefined {
  if (!palette || typeof palette !== "object") return undefined;
  const p = palette as Record<string, unknown>;
  return MASCOT_SWATCHES.find((id) => {
    const want = paletteFor(id) as Record<string, string>;
    return Object.keys(want).every(
      (k) => hex(p[k]) && same(p[k] as string, want[k]),
    );
  });
}
/** Strict validation of a RigPartSpec; returns the cleaned value or undefined if invalid. */
export function validateRigPartSpec(value: unknown): RigPartSpec | undefined {
  if (!value || typeof value !== "object") return undefined;
  const v = value as Record<string, unknown>;
  if (!(spec.rigs as string[]).includes(v.rig as string)) return undefined;
  const acc = Array.isArray(v.accessories) ? v.accessories : [];
  if (acc.length > MAX_ACCESSORIES) return undefined;
  if (new Set(acc).size !== acc.length) return undefined;
  if (!acc.every((a) => (MASCOT_ACCESSORIES as readonly unknown[]).includes(a)))
    return undefined;
  if (v.palette === undefined)
    return { rig: v.rig as string, accessories: acc as MascotAccessory[] };
  const id = swatchOf(v.palette);
  if (!id) return undefined;
  return {
    rig: v.rig as string,
    palette: paletteFor(id),
    accessories: acc as MascotAccessory[],
  };
}
