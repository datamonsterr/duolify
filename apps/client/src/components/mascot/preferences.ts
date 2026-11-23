import { useSyncExternalStore } from "react";
import type { MascotAppearance } from "./Mascot";
import {
  MASCOT_SWATCHES,
  validateRigPartSpec,
  type MascotAccessory,
  type MascotSwatch,
} from "./rig-part-spec";

/** The Learner's guide preference. Local cache here; the settings API (`mascot` object) syncs it. */
export type MascotPreference = {
  mode: "default" | "character";
  swatch?: MascotSwatch;
  accessories: MascotAccessory[];
  characterId?: string;
  companionVisible: boolean;
};
export const DEFAULT_MASCOT_PREFERENCE: MascotPreference = {
  mode: "default",
  accessories: [],
  companionVisible: true,
};
const KEY = "duolify.mascot";
let current: MascotPreference = load();
const listeners = new Set<() => void>();

function load(): MascotPreference {
  try {
    const raw = JSON.parse(localStorage.getItem(KEY) ?? "null");
    return sanitize(raw);
  } catch {
    return DEFAULT_MASCOT_PREFERENCE;
  }
}
/** Invalid or unknown input falls back to the built-in default; omitted fields keep defaults. */
export function sanitize(raw: unknown): MascotPreference {
  if (!raw || typeof raw !== "object") return DEFAULT_MASCOT_PREFERENCE;
  const r = raw as Record<string, unknown>;
  const swatch = MASCOT_SWATCHES.find((x) => x === r.swatch);
  const spec = validateRigPartSpec({
    rig: "robot-v1",
    accessories: r.accessories ?? [],
  });
  const id = typeof r.characterId === "string" ? r.characterId : undefined;
  return {
    mode: r.mode === "character" && id ? "character" : "default",
    swatch,
    accessories: spec?.accessories ?? [],
    characterId: id,
    companionVisible: r.companionVisible !== false,
  };
}
export const getMascotPreference = () => current;
export function setMascotPreference(next: MascotPreference) {
  current = next;
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    /* storage may be unavailable; keep the in-memory value */
  }
  listeners.forEach((l) => l());
}
const subscribe = (l: () => void) => {
  listeners.add(l);
  return () => listeners.delete(l);
};
export function useMascotPreference() {
  return useSyncExternalStore(
    subscribe,
    getMascotPreference,
    getMascotPreference,
  );
}
export function useMascotAppearance(): MascotAppearance {
  const p = useMascotPreference();
  return {
    swatch: p.swatch,
    accessories: p.accessories,
    characterId: p.mode === "character" ? p.characterId : undefined,
  };
}
