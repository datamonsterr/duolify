import {
  paletteFor,
  swatchOf,
  sanitize,
  setMascotPreference,
  DEFAULT_MASCOT_PREFERENCE,
  type MascotPreference,
} from "../../components/mascot";

/** Server `mascot` object (RigPartSpec v1 fields) to the local preference; invalid input gives defaults. */
export function fromServerMascot(raw: unknown): MascotPreference {
  if (!raw || typeof raw !== "object") return DEFAULT_MASCOT_PREFERENCE;
  const r = raw as Record<string, unknown>;
  return sanitize({
    mode: r.mode,
    swatch: swatchOf(r.palette),
    accessories: r.accessories,
    characterId: r.character_id ?? undefined,
    companionVisible: r.companion_visible,
  });
}
/** Partial patch for `POST /v1/me/settings`; omitted fields are preserved by the server. */
export function toServerMascotPatch(p: Partial<MascotPreference>) {
  const out: Record<string, unknown> = {};
  if (p.mode !== undefined) out.mode = p.mode;
  if ("swatch" in p) out.palette = p.swatch ? paletteFor(p.swatch) : null;
  if (p.accessories !== undefined) out.accessories = p.accessories;
  if ("characterId" in p) out.character_id = p.characterId ?? null;
  if (p.companionVisible !== undefined)
    out.companion_visible = p.companionVisible;
  return out;
}
/** Which fields a patch touches, for the content-free `mascot_customized` event. */
export function changedFields(p: Partial<MascotPreference>) {
  const f: string[] = [];
  if ("swatch" in p) f.push("palette");
  if (p.accessories !== undefined) f.push("accessories");
  if (p.mode !== undefined || "characterId" in p) f.push("character");
  return f;
}
export function applyServerMascot(raw: unknown) {
  if (raw !== undefined) setMascotPreference(fromServerMascot(raw));
}
